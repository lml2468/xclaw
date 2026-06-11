package octo

import (
	"net/url"
	"strings"
)

// buildMediaURL resolves a payload's media reference into a fully-qualified,
// host-validated absolute URL, or "" to reject it. Port of buildMediaUrl in
// cc-channel-octo/src/inbound.ts (hardened against absolute-URL smuggling and
// path traversal):
//
//   - reject scheme-relative URLs (//attacker.com/...)
//   - reject backslash injection
//   - absolute http(s): allow only when the host matches apiURL host
//   - relative: strip /file/ or /file/preview/ prefix, reject ../. traversal and
//     encoded-slash (%2f), then re-parse the assembled URL and assert the
//     normalized path is still under /file/ (catches %2e%2e dot-segment escapes)
//
// NOTE: the TS variant also accepts a configured CDN host (Octo serves media
// from a CDN distinct from the API host). xclaw has no CDN-host config field
// yet, so only same-host absolute URLs are accepted here; relative storage paths
// (the common case) resolve against apiURL. The download path still SSRF-checks
// and scopes the bot token per hop, so an allowed host is necessary but not
// sufficient to leak the token.
func buildMediaURL(relURL, apiURL string) string {
	if relURL == "" {
		return ""
	}
	if strings.Contains(relURL, "\\") {
		return ""
	}
	if strings.HasPrefix(relURL, "//") {
		return ""
	}

	if strings.HasPrefix(relURL, "http://") || strings.HasPrefix(relURL, "https://") {
		if apiURL == "" {
			return ""
		}
		target, err := url.Parse(relURL)
		if err != nil {
			return ""
		}
		base, err := url.Parse(apiURL)
		if err != nil {
			return ""
		}
		if !strings.EqualFold(target.Host, base.Host) {
			return ""
		}
		// Same-host: must not downgrade protocol relative to apiURL.
		if target.Scheme != base.Scheme {
			return ""
		}
		if target.Scheme != "http" && target.Scheme != "https" {
			return ""
		}
		return relURL
	}

	// Relative storage path — strip /file/ or /file/preview/ prefix.
	storagePath := relURL
	switch {
	case strings.HasPrefix(storagePath, "file/preview/"):
		storagePath = storagePath[len("file/preview/"):]
	case strings.HasPrefix(storagePath, "file/"):
		storagePath = storagePath[len("file/"):]
	}
	// Cheap literal traversal pre-check (defense-in-depth).
	for _, seg := range strings.Split(storagePath, "/") {
		if seg == ".." || seg == "." {
			return ""
		}
	}
	if strings.HasPrefix(storagePath, "/") {
		return ""
	}
	// Encoded-traversal defense-in-depth. Go's url.Parse does NOT decode %2e/%2f
	// in the path (unlike the WHATWG parser the TS relies on for normalization),
	// so a candidate like /file/%2e%2e/secret survives a literal prefix check.
	// Production storage paths never contain encoded dots or slashes; reject them
	// outright (covers %2e%2e dot-segment and %2f encoded-slash escapes).
	lower := strings.ToLower(storagePath)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%2e") {
		return ""
	}

	candidate := strings.TrimRight(apiURL, "/") + "/file/" + storagePath
	// Canonical sandbox check: after URL normalization, the path must still start
	// with /file/ (catches %2e%2e and other encoded-dot escapes).
	norm, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(norm.EscapedPath(), "/file/") && !strings.HasPrefix(norm.Path, "/file/") {
		return ""
	}
	return candidate
}

// isSameHost reports whether rawURL and apiURL have matching hosts
// (case-insensitive). Fail-closed on malformed input (inbound.ts isSameHost) —
// used by the gateway's MediaAuth hook to scope the bot token per hop.
func isSameHost(rawURL, apiURL string) bool {
	a, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	b, err := url.Parse(apiURL)
	if err != nil {
		return false
	}
	return a.Host != "" && strings.EqualFold(a.Host, b.Host)
}
