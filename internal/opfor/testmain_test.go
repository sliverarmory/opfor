package opfor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain keeps the existing test corpus rooted at the module directory after
// the implementation package moved to internal/opfor. A number of compatibility
// tests intentionally address testdata and third_party_licenses from that root.
func TestMain(m *testing.M) {
	// Process-object tests re-exec this test binary with an explicit child
	// working directory. Do not replace that directory before the helper runs.
	for _, argument := range os.Args {
		if argument == processObjectHelperMarker || argument == ioHelperMarker {
			os.Exit(m.Run())
		}
	}

	originalDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "opfor tests: get working directory: %v\n", err)
		os.Exit(1)
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "opfor tests: locate test source")
		os.Exit(1)
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err != nil {
		fmt.Fprintf(os.Stderr, "opfor tests: locate module root: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(moduleRoot); err != nil {
		fmt.Fprintf(os.Stderr, "opfor tests: change to module root: %v\n", err)
		os.Exit(1)
	}

	status := m.Run()
	if err := os.Chdir(originalDir); err != nil && status == 0 {
		fmt.Fprintf(os.Stderr, "opfor tests: restore working directory: %v\n", err)
		status = 1
	}
	os.Exit(status)
}
