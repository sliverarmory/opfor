package opfor

import "testing"

func TestSourceReleaseVersion(t *testing.T) {
	t.Parallel()
	if got, want := Version, "v0.0.2"; got != want {
		t.Fatalf("Version = %q, want %q", got, want)
	}
}
