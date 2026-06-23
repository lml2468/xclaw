// Media/file materialization — the Go port of cc-channel-octo's inbound media
// handling (src/media-inbound.ts downloadInboundImage + src/inbound.ts
// tryResolveFile + src/file-inline-wrap.ts buildInlinedFileBody).
//
// The IM connector attaches media/file URLs to router.InboundMessage.Attachments
// WITHOUT downloading (it has no session cwd). The gateway materializes them in
// runTurn AFTER the session cwd is resolved and BEFORE driver.Query:
//
// - Images (PNG/JPEG/GIF/WebP) are downloaded into <cwd>/.octobuddy-media/ so the
// agent's Read tool can open them natively, and a Read hint is appended to
// THIS turn's prompt body (not stored history).
// - Small text files (<20 KiB) are inlined as base64 inside a <file_content>
// wrapper — base64's alphabet can't forge the closing tag, defeating the
// "--- file end ---" prompt-injection break-out (file-inline-wrap.ts S2).
// - Larger / binary files are downloaded to <cwd>/.octobuddy-media/ and described
// with a path hint.
//
// SSRF: every download (and every redirect hop) is re-validated against
// config.AssertPublicURL. The bot Authorization header is scoped per hop via the
// gateway's MediaAuth hook — it only travels to the same host as apiUrl, so a
// redirect to an attacker host drops the credential.
package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lml2468/octobuddy/core/config"
	"github.com/lml2468/octobuddy/core/router"
	"github.com/lml2468/octobuddy/core/safepath"
	"github.com/lml2468/octobuddy/core/safety"
)

// InboundMediaDir is the subdir (under the session cwd) where downloaded inbound
// images and oversized files land. Mirrors INBOUND_MEDIA_DIR in media-inbound.ts
// (renamed for this project). It shares the session cwd partition, so the 7-day
// sandbox janitor reclaims it — no separate cleanup needed.
const InboundMediaDir = ".octobuddy-media"

// Download / inline caps (media-inbound.ts + inbound.ts + file-inline-wrap.ts).
const (
	maxImageBytes        = 5 * 1024 * 1024 // MAX_IMAGE_BYTES
	maxFileDownloadBytes = 5 * 1024 * 1024 // MAX_FILE_DOWNLOAD_BYTES
	inlineFileMaxBytes   = 20 * 1024       // INLINE_FILE_MAX_BYTES
	// inlineProbeBytes reads one byte past the inline cap so an over-cap file is
	// detected without reading it unboundedly (see resolveFile). Keep it exactly
	// inlineFileMaxBytes+1 — the overflow test (n > inlineFileMaxBytes) depends on it.
	inlineProbeBytes    = inlineFileMaxBytes + 1
	maxImagesPerMessage = 6 // MAX_IMAGES_PER_MESSAGE
	downloadTimeout     = 15 * time.Second
	maxRedirects        = 10
	mediaConcurrency    = 4 // max simultaneous attachment downloads per message
)

// allowedImageTypes maps the content types Claude can natively Read to a file
// extension. A response whose type isn't here is rejected (ALLOWED_IMAGE_TYPES).
var allowedImageTypes = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/jpg":  "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// textFileExtensions are inlined (text-like content), mirroring
// TEXT_FILE_EXTENSIONS in inbound.ts.
var textFileExtensions = map[string]bool{
	"txt": true, "md": true, "csv": true, "json": true, "xml": true,
	"yaml": true, "yml": true, "log": true, "py": true, "js": true,
	"ts": true, "tsx": true, "jsx": true, "mjs": true, "cjs": true,
	"go": true, "java": true, "rs": true, "c": true, "h": true,
	"cpp": true, "hpp": true, "cs": true, "rb": true, "php": true,
	"html": true, "htm": true, "css": true, "scss": true, "sass": true,
	"less": true, "sh": true, "bash": true, "zsh": true, "fish": true,
	"ps1": true, "toml": true, "ini": true, "conf": true, "cfg": true,
	"env": true, "sql": true, "graphql": true, "gql": true, "proto": true,
}

// MediaAuth returns the Authorization header value to use when fetching url, or
// "" to send no credential. The IM connector supplies it so the bot token is
// scoped to the apiUrl host (a redirect to another host drops it). Keeping it a
// hook preserves the gateway's IM-agnosticism — it never embeds a token.
type MediaAuth func(url string) string

// mediaHTTPClient is the media downloader's transport.
//
// - redirect: manual — we walk the chain ourselves so each hop is
// SSRF-revalidated and the Authorization header is recomputed per hop
// (fetchWithRedirectGuard parity).
// - DialControl: the actual socket address chosen by the resolver is
// re-checked against the private/local ranges at *dial time*. This closes the
// DNS-rebinding TOCTOU: AssertPublicURL resolves once for the policy check,
// but the transport resolves again to dial — a hostile authoritative DNS
// could return a public IP to the first lookup and 169.254.169.254 / a
// private IP to the second. Validating in Control (which runs on the exact
// address being connected) makes the check authoritative for the connection.
// - explicit Transport timeouts + per-host conn cap so a slow/hostile endpoint
// can't tie up connections (the ctx deadline still bounds the whole fetch).
var mediaHTTPClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   downloadTimeout,
			KeepAlive: 30 * time.Second,
			Control:   dialControlGuard,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxConnsPerHost:       8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: downloadTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// dialControlGuard rejects a connection whose resolved destination address is in
// a private/loopback/link-local/CGN/unspecified range. Runs after DNS resolution
// on the concrete address the kernel is about to connect to, so it defeats DNS
// rebinding that AssertPublicURL's earlier lookup cannot (the resolver may return
// a different IP at dial time).
func dialControlGuard(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("media dial: bad address %q: %w", address, err)
	}
	if config.IsPrivateOrLocalAddress(host) {
		return fmt.Errorf("media dial: refusing private/local address %s", host)
	}
	return nil
}

// materializeAttachments downloads/inlines the message's attachments into cwd and
// returns the prompt addition for THIS turn only: a trailing Read hint for
// downloaded images and any inline <file_content> blocks for small text files.
// It never returns an error — a failed attachment degrades to nothing (the
// original URL marker the connector put in the text remains the fallback).
//
// When cwd is "" (sandbox disabled) images can't be materialized for the Read
// tool; image attachments are skipped (the text already carries the URL marker).
// Text-file inlining still works since it doesn't need the cwd.
//
// Downloads run concurrently (bounded by mediaConcurrency) so N attachments cost
// roughly one download's wall time instead of the sum — a slow URL can't serialize
// the whole turn for downloadTimeout × N before driver.Query. Output order is
// preserved (results are assembled by attachment index after the fan-out joins).
func (g *Gateway) materializeAttachments(ctx context.Context, cwd string, atts []router.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	doImage := imageMaterializationPlan(cwd, atts)
	results := make([]attachmentMaterializationResult, len(atts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, mediaConcurrency)
	for i, att := range atts {
		if !shouldMaterializeAttachment(att, doImage[i]) {
			continue
		}
		wg.Add(1)
		go func(i int, att router.Attachment) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			results[i] = g.materializeOneAttachment(ctx, cwd, att)
		}(i, att)
	}
	wg.Wait()
	return renderMaterializedAttachments(results)
}

type attachmentMaterializationResult struct {
	imageRel  string
	fileBlock string
}

func imageMaterializationPlan(cwd string, atts []router.Attachment) []bool {
	doImage := make([]bool, len(atts))
	imageBudget := 0
	for i, att := range atts {
		if att.Kind == router.AttachmentImage && cwd != "" && imageBudget < maxImagesPerMessage {
			doImage[i] = true
			imageBudget++
		}
	}
	return doImage
}

func shouldMaterializeAttachment(att router.Attachment, doImage bool) bool {
	switch att.Kind {
	case router.AttachmentImage:
		return doImage
	case router.AttachmentFile:
		return true
	default:
		return false
	}
}

func (g *Gateway) materializeOneAttachment(ctx context.Context, cwd string, att router.Attachment) attachmentMaterializationResult {
	switch att.Kind {
	case router.AttachmentImage:
		rel, err := g.downloadImage(ctx, cwd, att.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[gateway] inbound image skipped: %v\n", err)
			return attachmentMaterializationResult{}
		}
		return attachmentMaterializationResult{imageRel: rel}
	case router.AttachmentFile:
		return attachmentMaterializationResult{fileBlock: g.resolveFile(ctx, cwd, att)}
	default:
		return attachmentMaterializationResult{}
	}
}

func renderMaterializedAttachments(results []attachmentMaterializationResult) string {
	var b strings.Builder
	var imageRelPaths []string
	for i := range results {
		if results[i].fileBlock != "" {
			b.WriteString("\n")
			b.WriteString(results[i].fileBlock)
		} else if results[i].imageRel != "" {
			imageRelPaths = append(imageRelPaths, results[i].imageRel)
		}
	}

	if len(imageRelPaths) > 0 {
		b.WriteString("\n")
		b.WriteString(RenderImageFragment(imageRelPaths))
	}
	return b.String()
}

// RenderImageFragment returns the prompt line listing downloaded image paths
// in the format the agent already knows from IM inbound — a single
// "[已下载图片到本地: …]" hint for one image, or a "[已下载 N 张图片到本地: …]"
// hint for multiple. Empty for an empty slice. Composer attachments and IM
// inbound both route through here so the agent sees one consistent shape.
func RenderImageFragment(relPaths []string) string {
	switch len(relPaths) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("[已下载图片到本地: %s — 请用 Read 工具查看]", relPaths[0])
	default:
		return fmt.Sprintf("[已下载 %d 张图片到本地: %s — 请用 Read 工具逐个查看]",
			len(relPaths), strings.Join(relPaths, ", "))
	}
}

// RenderFileFragment returns the prompt block for a downloaded file —
// "[文件: name]\n本地路径: relPath". Mirrors resolveFile's downloaded-file
// branch; shared so Composer attachments produce an identical line.
func RenderFileFragment(name, relPath string) string {
	return fmt.Sprintf("[文件: %s]\n本地路径: %s", name, relPath)
}

// RenderInlinedFileFragment wraps a small text-like file's bytes in a
// `<file_content name=… encoding="base64" bytes="…">…</file_content>` block —
// the same envelope IM-inbound uses for text files under inlineFileMaxBytes.
// Caller must check ShouldInlineAsText + the size cap.
func RenderInlinedFileFragment(name string, body []byte) string {
	return buildInlinedFileBody(name, body)
}

// ShouldInlineAsText reports whether a filename's extension is in the
// text-like set the inbound path inlines (vs writes-then-references). Shared
// so Composer's branching mirrors inbound's.
func ShouldInlineAsText(name string) bool {
	return textFileExtensions[extractExtension("", name)]
}

// MaxInlineFileBytes is the size cap (in bytes) above which a text-like
// attachment is materialized to disk instead of inlined in the prompt.
// Exposed so Composer-path callers can branch identically to inbound.
const MaxInlineFileBytes = inlineFileMaxBytes

// MaxImageBytes is the per-attachment image size cap shared by IM inbound and
// Composer outbound. Bytes over this cap are rejected at write time.
const MaxImageBytes = maxImageBytes

// MaxFileBytes is the per-attachment file (non-image) size cap shared by IM
// inbound and Composer outbound.
const MaxFileBytes = maxFileDownloadBytes

// MaxImagesPerSend is the per-message image cap (IM inbound enforces it as
// MAX_IMAGES_PER_MESSAGE; Composer respects the same).
const MaxImagesPerSend = maxImagesPerMessage

// WriteSandboxImage SafeWrites the given image bytes into
// <cwd>/.octobuddy-media/<uuid>-image.<ext> and returns the cwd-relative path
// the prompt fragment uses. The Composer path calls this with bytes the
// operator picked locally (no HTTP fetch). Mime must be in allowedImageTypes;
// body must be <= MaxImageBytes and non-empty.
func WriteSandboxImage(cwd, mime string, body []byte) (string, error) {
	rawType := strings.ToLower(strings.TrimSpace(mime))
	ext, ok := allowedImageTypes[rawType]
	if !ok {
		if rawType == "" {
			rawType = "未知"
		}
		return "", fmt.Errorf("unsupported image type: %s", rawType)
	}
	if err := checkAttachmentSize(body, MaxImageBytes); err != nil {
		return "", err
	}
	if err := safepath.SafeMkdirAll(cwd, InboundMediaDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir media dir: %w", err)
	}
	name := fmt.Sprintf("%s-image.%s", uuid.NewString(), ext)
	if err := safepath.SafeWrite(cwd, filepath.Join(InboundMediaDir, name), body, 0o600); err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}
	return filepath.Join(InboundMediaDir, name), nil
}

// WriteSandboxFile SafeWrites the given file bytes into
// <cwd>/.octobuddy-media/<uuid>-<safename> and returns the cwd-relative path
// the prompt fragment uses. The operator-supplied name is sanitized to
// [A-Za-z0-9._-] for the on-disk leaf — the original name is preserved for
// the prompt label by callers.
func WriteSandboxFile(cwd, name string, body []byte) (string, error) {
	if err := checkAttachmentSize(body, MaxFileBytes); err != nil {
		return "", err
	}
	if err := safepath.SafeMkdirAll(cwd, InboundMediaDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir media dir: %w", err)
	}
	safeName := sanitizeFileBaseName(name)
	leaf := fmt.Sprintf("%s-%s", uuid.NewString(), safeName)
	if err := safepath.SafeWrite(cwd, filepath.Join(InboundMediaDir, leaf), body, 0o600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return filepath.Join(InboundMediaDir, leaf), nil
}

func checkAttachmentSize(body []byte, max int64) error {
	if len(body) == 0 {
		return fmt.Errorf("empty attachment")
	}
	if int64(len(body)) > max {
		return fmt.Errorf("exceeds size cap %s", formatBytes(max))
	}
	return nil
}

// downloadImage fetches url into <cwd>/.octobuddy-media/<uuid>-<safeName> and returns
// the path relative to cwd (what the Read hint shows). Mirrors
// downloadInboundImage: SSRF-checked, per-hop token scoping, content-type gate,
// 5 MB cap, downloadTimeout deadline.
func (g *Gateway) downloadImage(ctx context.Context, cwd, rawURL string) (string, error) {
	resp, err := g.fetchGuarded(ctx, rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed HTTP %d", resp.StatusCode)
	}
	ct, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	rawType := strings.ToLower(strings.TrimSpace(ct))
	ext, ok := allowedImageTypes[rawType]
	if !ok {
		if rawType == "" {
			rawType = "未知"
		}
		return "", fmt.Errorf("unsupported image type: %s", rawType)
	}

	// agent owns `cwd` (Bash + bypass) — bare MkdirAll
	// would follow an agent-planted `.octobuddy-media → ~/.ssh/` and the
	// subsequent writeCapped would land attacker-supplied IM bytes under
	//.ssh/. safepath's dirfd walk refuses the symlinked entry. cwd
	// itself is operator-trusted as the sandbox root.
	if err := safepath.SafeMkdirAll(cwd, InboundMediaDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir media dir: %w", err)
	}
	name := fmt.Sprintf("%s-image.%s", uuid.NewString(), ext)
	dir := filepath.Join(cwd, InboundMediaDir)
	localPath := filepath.Join(dir, name)
	if err := writeCapped(cwd, filepath.Join(InboundMediaDir, name), resp.Body, maxImageBytes); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cwd, localPath)
	if err != nil {
		rel = localPath
	}
	return rel, nil
}

// resolveFile mirrors tryResolveFile + buildInlinedFileBody. It returns the
// prompt block for the file (inline <file_content>, a downloaded-path hint, or a
// short description) — never an error; failures degrade to a description so the
// turn still proceeds.
func (g *Gateway) resolveFile(ctx context.Context, cwd string, att router.Attachment) string {
	// att.Name is already sanitized by the connector; re-sanitize defensively for
	// the label/attribute so a downstream change can't reintroduce injection.
	filename := safety.SanitizeDisplayName(att.Name, "未知文件")
	ext := extractExtension(att.URL, filename)
	if !textFileExtensions[ext] {
		return renderFileDescription(filename, att.Size)
	}
	if att.Size > 0 && att.Size > maxFileDownloadBytes {
		return fmt.Sprintf("[文件: %s (%s) - 超过下载上限 %s]",
			filename, formatBytes(att.Size), formatBytes(maxFileDownloadBytes))
	}
	resp, err := g.fetchGuarded(ctx, att.URL)
	if err != nil {
		return fmt.Sprintf("[文件: %s - 拒绝下载: %v]", filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("[文件: %s - 下载失败 HTTP %d]", filename, resp.StatusCode)
	}
	head, n, err := readInlineProbe(resp.Body)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Sprintf("[文件: %s - 网络错误: %v]", filename, err)
	}
	if n <= inlineFileMaxBytes {
		return buildInlinedFileBody(filename, head)
	}
	if cwd == "" {
		return fmt.Sprintf("[文件: %s - 过大未内联]", filename)
	}
	return g.materializeLargeFile(cwd, filename, head, resp.Body)
}

func renderFileDescription(filename string, size int64) string {
	if size > 0 {
		return fmt.Sprintf("[文件: %s (%s)]", filename, formatBytes(size))
	}
	return fmt.Sprintf("[文件: %s]", filename)
}

func readInlineProbe(r io.Reader) ([]byte, int, error) {
	head := make([]byte, inlineProbeBytes)
	n, err := io.ReadFull(r, head)
	return head[:n], n, err
}

func (g *Gateway) materializeLargeFile(cwd, filename string, head []byte, body io.Reader) string {
	if err := safepath.SafeMkdirAll(cwd, InboundMediaDir, 0o755); err != nil {
		return fmt.Sprintf("[文件: %s - 下载错误: %v]", filename, err)
	}
	dir := filepath.Join(cwd, InboundMediaDir)
	safeName := sanitizeFileBaseName(filename)
	leaf := fmt.Sprintf("%s-%s", uuid.NewString(), safeName)
	localPath := filepath.Join(dir, leaf)
	src := io.MultiReader(strings.NewReader(string(head)), body)
	if err := writeCapped(cwd, filepath.Join(InboundMediaDir, leaf), src, maxFileDownloadBytes); err != nil {
		_ = os.Remove(localPath)
		return fmt.Sprintf("[文件: %s - %v]", filename, err)
	}
	rel, relErr := filepath.Rel(cwd, localPath)
	if relErr != nil {
		rel = localPath
	}
	return RenderFileFragment(filename, rel)
}

// fetchGuarded performs an SSRF-guarded GET, manually walking redirects so each
// hop is re-validated and the Authorization header is recomputed per hop
// (fetchWithRedirectGuard + assertPublicUrl parity).
func (g *Gateway) fetchGuarded(ctx context.Context, rawURL string) (*http.Response, error) {
	assertPublic := g.assertPublic
	if assertPublic == nil {
		assertPublic = config.AssertPublicURL
	}
	client := g.mediaClient
	if client == nil {
		client = mediaHTTPClient
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	current := rawURL
	for hop := 0; hop <= maxRedirects; hop++ {
		if err := assertPublic(ctx, current); err != nil {
			cancel()
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		if g.mediaAuth != nil {
			if h := g.mediaAuth(current); h != "" {
				req.Header.Set("Authorization", h)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return nil, err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			_ = resp.Body.Close()
			if loc == "" {
				cancel()
				return nil, fmt.Errorf("redirect without Location")
			}
			base, perr := url.Parse(current)
			if perr != nil {
				cancel()
				return nil, perr
			}
			ref, perr := url.Parse(loc)
			if perr != nil {
				cancel()
				return nil, perr
			}
			current = base.ResolveReference(ref).String()
			continue
		}
		// Terminal response — wrap the body so closing it also cancels the ctx.
		resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	}
	cancel()
	return nil, fmt.Errorf("too many redirects (started at %s)", rawURL)
}

// cancelOnCloseBody cancels the download context when the body is closed, so the
// per-download timeout's resources are always released.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// writeCapped reads src into memory (capped at max bytes) and writes it via
// safepath.SafeWrite anchored at `root` — symlink-safe (refuses a planted
// leaf-symlink AND dirfd-walks every component from `root`), atomic
// temp+rename. Caller passes `root` = the operator-trusted sandbox cwd and
// `rel` = the leaf path inside it; for cwdBase configured outside $HOME
// SafeWriteAbs's fallback skipped the dirfd walk entirely, leaving the
// .octobuddy-media exfil race open. Taking root explicitly closes that path.
func writeCapped(root, rel string, src io.Reader, max int64) error {
	buf, err := io.ReadAll(io.LimitReader(src, max+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if int64(len(buf)) > max {
		return fmt.Errorf("exceeds size cap %s", formatBytes(max))
	}
	if len(buf) == 0 {
		return fmt.Errorf("empty response")
	}
	if err := safepath.SafeWrite(root, rel, buf, 0o600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// buildInlinedFileBody frames file content as a base64 <file_content> block.
// The base64 alphabet ([A-Za-z0-9+/=]) contains none of <, /, > so the content
// cannot forge the closing tag (file-inline-wrap.ts S2 defense).
func buildInlinedFileBody(filename string, content []byte) string {
	safeName := sanitizeFilenameForAttribute(filename)
	b64 := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf("[文件: %s]\n<file_content name=%q encoding=\"base64\" bytes=\"%d\">\n%s\n</file_content>",
		filename, safeName, len(content), b64)
}

// sanitizeFilenameForAttribute strips characters that could break out of the
// name="..." attribute or be misread as the closing tag (file-inline-wrap.ts).
func sanitizeFilenameForAttribute(name string) string {
	r := strings.NewReplacer(
		"<", "_", ">", "_", "\"", "_", "'", "_", "\\", "_",
		"\r", "_", "\n", "_", "\t", "_",
	)
	out := r.Replace(name)
	if len(out) > 128 {
		out = out[:128]
	}
	return out
}

// sanitizeFileBaseName reduces a filename to [A-Za-z0-9._-] for an on-disk name
// (tryResolveFile temp-path naming), falling back to "file".
func sanitizeFileBaseName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "file"
	}
	s := b.String()
	// A name that is only dots (".", "..", …) is harmless here because callers
	// always prefix a uuid, but collapse it anyway so an on-disk name never reads
	// like a path traversal segment.
	if strings.Trim(s, ".") == "" {
		return "file"
	}
	return s
}

// extractExtension returns the lowercase extension from the URL path, falling
// back to the filename. Matches extractExtension in inbound.ts (<=8 chars).
func extractExtension(rawURL, fallbackName string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if ext := extFromPath(u.Path); ext != "" {
			return ext
		}
	}
	return extFromPath(fallbackName)
}

func extFromPath(p string) string {
	// Drop any query/fragment that rode along on a fallback name (the URL-parsed
	// path is already clean, but a raw filename may carry "?v=2" etc.).
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	i := strings.LastIndex(p, ".")
	if i < 0 || i == len(p)-1 {
		return ""
	}
	ext := strings.ToLower(p[i+1:])
	// stop at the first path-ish separator that slipped in
	if strings.ContainsAny(ext, "/\\") || len(ext) > 8 {
		return ""
	}
	return ext
}

// formatBytes renders a byte count for a human-readable label (formatBytes in
// inbound.ts).
func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%dB", n)
	case n < mb:
		return fmt.Sprintf("%.1fKB", float64(n)/kb)
	case n < gb:
		return fmt.Sprintf("%.1fMB", float64(n)/mb)
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/gb)
	}
}
