//go:build windows

package opfor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortableJavaFileWindowsPermissionAndSpaceContracts(t *testing.T) {
	root := t.TempDir()
	missing := constructPortableJavaFileValue(t, String(filepath.Join(root, "missing")))
	if !invokePortableJavaFileValue(t, missing, "setReadable", Int(1)).Truth() {
		t.Fatal("Windows setReadable(true) on missing path = false, want native return-only true")
	}
	if invokePortableJavaFileValue(t, missing, "setReadable", Int(0)).Truth() {
		t.Fatal("Windows setReadable(false) on missing path = true")
	}
	if !invokePortableJavaFileValue(t, missing, "setExecutable", Int(1)).Truth() {
		t.Fatal("Windows setExecutable(true) on missing path = false, want native return-only true")
	}
	if invokePortableJavaFileValue(t, missing, "setWritable", Int(1)).Truth() {
		t.Fatal("Windows setWritable on missing path = true")
	}

	directory := constructPortableJavaFileValue(t, String(root))
	if invokePortableJavaFileValue(t, directory, "setWritable", Int(0)).Truth() ||
		invokePortableJavaFileValue(t, directory, "setReadOnly").Truth() {
		t.Fatal("Windows read-only attribute mutation accepted a directory")
	}

	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := constructPortableJavaFileValue(t, String(path))
	if !invokePortableJavaFileValue(t, file, "canExecute").Truth() {
		t.Fatal("Windows canExecute existing file = false")
	}
	if invokePortableJavaFileValue(t, missing, "canExecute").Truth() {
		t.Fatal("Windows canExecute missing file = true")
	}
	if !invokePortableJavaFileValue(t, file, "setReadOnly").Truth() {
		t.Fatal("Windows setReadOnly existing file = false")
	}
	if invokePortableJavaFileValue(t, file, "canWrite").Truth() {
		t.Fatal("Windows canWrite read-only file = true")
	}
	if !invokePortableJavaFileValue(t, file, "setWritable", Int(1)).Truth() {
		t.Fatal("Windows setWritable(true) existing file = false")
	}
	if !invokePortableJavaFileValue(t, file, "canWrite").Truth() {
		t.Fatal("Windows canWrite writable file = false")
	}
	for _, method := range []string{"getTotalSpace", "getFreeSpace", "getUsableSpace"} {
		if value := invokePortableJavaFileValue(t, file, method); value.Kind() != KindLong || value.Int64() <= 0 {
			t.Errorf("Windows %s = %s, want positive long", method, value.Describe())
		}
	}
}
