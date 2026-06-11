package config

import (
	"context"
	"testing"
)

func TestAssertPublicURL(t *testing.T) {
	ctx := context.Background()
	reject := []string{
		"http://127.0.0.1/x",
		"https://127.0.0.1/x",
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"https://10.0.0.5/x",
		"https://192.168.1.1/x",
		"https://[::1]/x",
		"ftp://example.com/x",  // non-http(s) scheme
		"https://100.64.0.1/x", // CGN
		"https://0.0.0.0/x",
	}
	for _, u := range reject {
		if err := AssertPublicURL(ctx, u); err == nil {
			t.Errorf("AssertPublicURL(%q) = nil, want rejection", u)
		}
	}

	// Public IP literal is accepted (no DNS needed).
	if err := AssertPublicURL(ctx, "https://203.0.113.7/x"); err != nil {
		t.Errorf("public IP literal rejected: %v", err)
	}
}

func TestIsPrivateOrLocalAddress(t *testing.T) {
	priv := []string{"127.0.0.1", "10.1.2.3", "192.168.0.1", "169.254.169.254", "::1", "100.64.0.1", "0.0.0.0"}
	for _, a := range priv {
		if !IsPrivateOrLocalAddress(a) {
			t.Errorf("IsPrivateOrLocalAddress(%q) = false, want true", a)
		}
	}
	pub := []string{"203.0.113.7", "8.8.8.8", "2606:4700:4700::1111"}
	for _, a := range pub {
		if IsPrivateOrLocalAddress(a) {
			t.Errorf("IsPrivateOrLocalAddress(%q) = true, want false", a)
		}
	}
	if IsPrivateOrLocalAddress("not-an-ip") {
		t.Error("non-IP should return false")
	}
}
