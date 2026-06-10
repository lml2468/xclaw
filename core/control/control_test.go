package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	body, _ := json.Marshal(SessionSendBody{UID: "u1", Text: "hi"})
	line, err := Encode(Envelope{Kind: KindCommand, ID: "1", Type: "session.send", Body: body})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if line[len(line)-1] != '\n' {
		t.Fatal("encoded line must end with newline")
	}
	e, err := Decode(line[:len(line)-1])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.V != ProtocolVersion || e.Kind != KindCommand || e.Type != "session.send" || e.ID != "1" {
		t.Fatalf("envelope fields wrong: %+v", e)
	}
	var got SessionSendBody
	if err := json.Unmarshal(e.Body, &got); err != nil || got.Text != "hi" {
		t.Fatalf("body wrong: %+v %v", got, err)
	}
}

func TestDefaultsVersion(t *testing.T) {
	line, _ := Encode(Envelope{Kind: KindEvent, Type: "x"})
	if !strings.Contains(string(line), `"v":1`) {
		t.Fatalf("version should default to 1: %s", line)
	}
}

func TestFrameTooLarge(t *testing.T) {
	big := make([]byte, MaxFrameBytes+1)
	if _, err := Decode(big); err != ErrFrameTooLarge {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}
