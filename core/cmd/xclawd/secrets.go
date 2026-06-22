package main

import (
	"fmt"
	"sync"

	"github.com/lml2468/xclaw/core/control/wire"
)

// secretStore holds a bot's secret tokens in memory only — never persisted to
// disk and never logged. Tokens may be seeded from the config file (the headless
// fallback) and/or injected at runtime over the control bus (secret.inject from
// the GUI's Keychain). Consumers read them lazily through the getters, so an
// injection takes effect on the next use without rebuilding the bot stack.
type secretStore struct {
	mu      sync.RWMutex
	octo    string
	gateway string
}

// OctoToken returns the current Octo bot token (empty if not yet set).
func (s *secretStore) OctoToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.octo
}

// GatewayToken returns the current model-gateway token (empty if not yet set).
func (s *secretStore) GatewayToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gateway
}

// Set records a token by kind. An empty value is ignored (seeding from an absent
// config field is a no-op, never clobbering an injected token). Unknown kinds
// return an error so a malformed secret.inject is surfaced, not silently dropped.
func (s *secretStore) Set(kind wire.SecretKind, value string) error {
	if value == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case wire.SecretKindOcto:
		s.octo = value
	case wire.SecretKindGateway:
		s.gateway = value
	default:
		return fmt.Errorf("unknown secret kind %q", kind)
	}
	return nil
}

// Clear removes the token for kind (the explicit revoke path, distinct from Set's
// seed-ignores-empty behavior). Unknown kinds return an error.
func (s *secretStore) Clear(kind wire.SecretKind) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case wire.SecretKindOcto:
		s.octo = ""
	case wire.SecretKindGateway:
		s.gateway = ""
	default:
		return fmt.Errorf("unknown secret kind %q", kind)
	}
	return nil
}
