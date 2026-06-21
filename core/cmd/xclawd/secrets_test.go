package main

import (
	"sync"
	"testing"

	"github.com/lml2468/xclaw/core/control/wire"
)

func TestSecretStoreSetGet(t *testing.T) {
	var s secretStore
	if s.OctoToken() != "" || s.GatewayToken() != "" {
		t.Fatal("zero value should be empty")
	}
	if err := s.Set(wire.SecretKindOcto, "bf_x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(wire.SecretKindGateway, "sk_y"); err != nil {
		t.Fatal(err)
	}
	if s.OctoToken() != "bf_x" || s.GatewayToken() != "sk_y" {
		t.Fatalf("got octo=%q gateway=%q", s.OctoToken(), s.GatewayToken())
	}
}

func TestSecretStoreEmptyValueDoesNotClobber(t *testing.T) {
	var s secretStore
	_ = s.Set(wire.SecretKindOcto, "bf_real")
	// Seeding from an absent config field (empty) must not wipe an injected token.
	if err := s.Set(wire.SecretKindOcto, ""); err != nil {
		t.Fatal(err)
	}
	if s.OctoToken() != "bf_real" {
		t.Fatalf("empty set clobbered token: %q", s.OctoToken())
	}
}

func TestSecretStoreUnknownKind(t *testing.T) {
	var s secretStore
	if err := s.Set("bogus", "v"); err == nil {
		t.Fatal("unknown kind should error")
	}
}

func TestSecretStoreConcurrent(t *testing.T) {
	var s secretStore
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = s.Set(wire.SecretKindOcto, "bf") }()
		go func() { defer wg.Done(); _ = s.OctoToken() }()
	}
	wg.Wait()
	if s.OctoToken() != "bf" {
		t.Fatalf("got %q", s.OctoToken())
	}
}
