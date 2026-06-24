// Package clog is the daemon's slog wrapper. For(component) returns a
// fresh sub-logger from slog.Default() so callers always see whatever
// handler Setup installed — sidesteps the package-init capture problem
// where `var log = slog.Default().With(...)` would freeze a handler
// before main() configured it.
package clog

import (
	"io"
	"log/slog"
	"os"
)

// Setup installs the daemon's default slog handler. Call once from
// main() before any subsystem logs. debug=true bumps level to DEBUG;
// json=true switches to JSON for machine parsing. out=nil → os.Stderr.
func Setup(debug, json bool, out io.Writer) {
	if out == nil {
		out = os.Stderr
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if json {
		h = slog.NewJSONHandler(out, opts)
	} else {
		h = slog.NewTextHandler(out, opts)
	}
	slog.SetDefault(slog.New(h))
}

// For returns a logger tagged `component=<name>`, freshly bound to the
// live slog default on every call.
func For(component string) *slog.Logger {
	return slog.Default().With("component", component)
}
