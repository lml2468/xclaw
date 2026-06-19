package octo

import (
	"net/url"
	"strings"
)

// isSameHost reports whether rawURL and apiURL have matching scheme AND host
// (case-insensitive). Fail-closed on malformed input (inbound.ts isSameHost) —
// used by the gateway's MediaAuth hook to scope the bot token per hop. The scheme
// must match too, so the bearer token is never sent over a plaintext http
// downgrade to the same host that apiURL serves over https (L19).
//
// (URL building lives in content.go's buildMediaURL, which both the content
// renderer and resolveAttachments share.)
func isSameHost(rawURL, apiURL string) bool {
	a, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	b, err := url.Parse(apiURL)
	if err != nil {
		return false
	}
	return a.Host != "" && strings.EqualFold(a.Host, b.Host) &&
		a.Scheme != "" && strings.EqualFold(a.Scheme, b.Scheme)
}
