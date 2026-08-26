package opfor

import (
	"bytes"
	"testing"
)

func TestInlineTraceCanonicalCompatibility(t *testing.T) {
	got, want := runCanonicalOutput(t, "inline")
	if !bytes.Equal(got, want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}
