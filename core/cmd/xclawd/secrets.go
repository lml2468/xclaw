package main

import (
	"fmt"
	"sync"
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

// Secret kinds carried by the secret.inject control command.
const (
	secretKindOcto    = "octoToken"
	secretKindGateway = "gatewayToken"
)

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
func (s *secretStore) Set(kind, value string) error {
	if value == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch kind {
	case secretKindOcto:
		s.octo = value
	case secretKindGateway:
		s.gateway = value
	default:
		return fmt.Errorf("unknown secret kind %q", kind)
	}
	return nil
}
