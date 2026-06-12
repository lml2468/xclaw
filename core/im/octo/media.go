package octo

import (
	"net/url"
	"strings"
)

// isSameHost reports whether rawURL and apiURL have matching hosts
// (case-insensitive). Fail-closed on malformed input (inbound.ts isSameHost) —
// used by the gateway's MediaAuth hook to scope the bot token per hop.
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
	return a.Host != "" && strings.EqualFold(a.Host, b.Host)
}
