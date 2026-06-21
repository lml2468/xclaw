package octo

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// buildConnack assembles a minimal success CONNACK (flags=0 → no server-version
// byte, server version stays 0 → no nodeId), matching readConnack's parser.
func buildConnack(t *testing.T) []byte {
	t.Helper()
	kp, err := generateDHKeyPair() // a valid curve25519 public key for the DH
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	var b encoder
	b.writeInt64(0) // timeDiff (unused)
	b.writeByte(1)  // reason = success
	b.writeString(base64.StdEncoding.EncodeToString(kp.pub[:]))
	b.writeString("0123456789abcdef") // 16-byte salt → 16-byte IV
	return frame(pktConnack, b.buf)
}

// TestSocketRunStopsOnContextCancel verifies the shutdown fix: gorilla's
// ReadMessage does not observe ctx, so run must close the conn on cancellation
// and return promptly (rather than blocking until the WS naturally errors).
func TestSocketRunStopsOnContextCancel(t *testing.T) {
	connack := buildConnack(t)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if _, _, err := c.ReadMessage(); err != nil { // CONNECT
			return
		}
		if err := c.WriteMessage(websocket.BinaryMessage, connack); err != nil {
			return
		}
		// Idle: never send another frame, so the client's run() blocks in
		// ReadMessage — exactly the state the cancel path must break out of.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	sock := newSocketConn(wsURL, "uid", "tok", func(BotMessage) {}, func(error) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sock.connect(ctx); err != nil {
		t.Fatalf("connect/handshake: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sock.run(ctx) }()

	// Give run() a moment to settle into the blocking read, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run should return nil after ctx cancel (closed), got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancel — ReadMessage was not unblocked")
	}
}

// rememberMsgID is the LRU dedup state for the recently-seen messageIDs ring.
// First insert returns false (new); a second insert of the same id returns true
// (caller skips forwarding so we don't fire a duplicate turn).
func TestRememberMsgIDDedupsAndBounds(t *testing.T) {
	s := &socketConn{recentMsgIDs: make(map[string]struct{}, recentMsgIDsCap)}
	if s.rememberMsgID("100") {
		t.Fatal("first sighting must be new")
	}
	if !s.rememberMsgID("100") {
		t.Fatal("second sighting must be a duplicate")
	}
	// Empty id is a no-op (cannot be deduped, never enters the ring).
	if s.rememberMsgID("") {
		t.Fatal("empty id must not be considered seen")
	}
	if _, ok := s.recentMsgIDs[""]; ok {
		t.Fatal("empty id must not enter the ring")
	}
	// Ring eviction: once we exceed the cap, the oldest entry is forgotten and
	// re-presenting it must register as new again.
	for i := 0; i < recentMsgIDsCap; i++ {
		s.rememberMsgID(strconv.FormatInt(int64(1000+i), 10))
	}
	if len(s.recentMsgIDs) != recentMsgIDsCap {
		t.Fatalf("ring should be at cap %d, got %d", recentMsgIDsCap, len(s.recentMsgIDs))
	}
	if s.rememberMsgID("100") {
		t.Fatal("after cap eviction, the oldest id (\"100\") should be new again")
	}
}
