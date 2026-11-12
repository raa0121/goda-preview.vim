package plantuml

import (
	"bytes"
	"compress/flate"
	"io"
	"strings"
	"testing"
)

func TestEncodeRoundTrip(t *testing.T) {
	for _, src := range []string{"", "A", "Bob -> Alice : hello", "@startuml\nAlice -> Bob: こんにちは\n@enduml"} {
		enc := Encode(src)
		if len(enc)%4 != 0 {
			t.Errorf("Encode(%q) = %q: not whole groups of 4", src, enc)
		}
		raw, err := encoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("decode %q: %v", enc, err)
		}
		got, err := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
		if err != nil {
			t.Fatalf("inflate %q: %v", enc, err)
		}
		if string(got) != src {
			t.Errorf("round trip of %q = %q", src, got)
		}
	}
}

func TestURL(t *testing.T) {
	got := URL("http://localhost:8080/", "A")
	if !strings.HasPrefix(got, "http://localhost:8080/svg/") || strings.Contains(got, "//svg") {
		t.Errorf("URL = %q", got)
	}
}
