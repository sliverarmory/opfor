package opfor

import "testing"

func TestSourceReleaseVersion(t *testing.T) {
	t.Parallel()
	if got, want := Version, "v0.1.0-alpha.2"; got != want {
		t.Fatalf("Version = %q, want %q", got, want)
	}
}
