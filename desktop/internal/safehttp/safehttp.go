// Package safehttp builds HTTP clients with a dial-time guard against
// SSRF / DNS rebinding: the guard refuses TCP connects to private,
// loopback, link-local and CGN addresses so a poisoned DNS or
// compromised mirror can't redirect a public URL to internal targets
// (cloud metadata at 169.254.169.254, an RFC-1918 host, etc.).
//
// Two policies — strict for download CDNs, loopback-tolerant for the
// wizard's API URL when an operator points at http://localhost.
package safehttp

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/lml2468/octobuddy/core/config"
)

// Options controls the dial guard.
type Options struct {
	// AllowLoopback permits 127.0.0.0/8 + ::1 connects. Set only when
	// the caller has knowingly pointed at a localhost dev endpoint.
	AllowLoopback bool
	// Tag prefixes the dial-error message so an operator can tell which
	// caller refused the connect ("octocli dial: …" vs "octoapi dial: …").
	Tag string
}

// NewClient returns an http.Client whose dialer enforces opts.
func NewClient(opts Options) *http.Client {
	if opts.Tag == "" {
		opts.Tag = "safehttp"
	}
	dial := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   guard(opts),
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dial.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          8,
			MaxConnsPerHost:       4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// Guard returns the dialer Control func for callers that want to build
// their own http.Transport with custom sizing.
func Guard(opts Options) func(network, address string, _ syscall.RawConn) error {
	return guard(opts)
}

func guard(opts Options) func(network, address string, _ syscall.RawConn) error {
	tag := opts.Tag
	return func(_ string, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%s dial: bad address %q: %w", tag, address, err)
		}
		// AllowLoopback honors all RFC1122 loopback (127.0.0.0/8 + ::1),
		// not just literal 127.0.0.1 — multi-instance dev setups on
		// 127.0.0.2 would otherwise be refused.
		if opts.AllowLoopback {
			if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
				return nil
			}
		}
		if config.IsPrivateOrLocalAddress(host) {
			return fmt.Errorf("%s dial: refusing private/local address %s", tag, host)
		}
		return nil
	}
}
