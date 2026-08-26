package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidReleaseVersion(t *testing.T) {
	for _, version := range []string{
		"v0.1.0",
		"v0.1.0-alpha.1",
		"v12.34.56-rc.2+build.7",
		"v1.2.3-alpha--candidate+build--metadata",
		"v1.2.3+001",
	} {
		if !validReleaseVersion(version) {
			t.Errorf("validReleaseVersion(%q) = false", version)
		}
	}
	for _, version := range []string{
		"",
		"0.1.0",
		"v0.1",
		"v0.1.0/escape",
		"v0.1.0 alpha",
		"v01.2.3",
		"v1.02.3",
		"v1.2.03",
		"v1.2.3-01",
		"v1.2.3-alpha.01",
	} {
		if validReleaseVersion(version) {
			t.Errorf("validReleaseVersion(%q) = true", version)
		}
	}
}

func TestDeterministicArchives(t *testing.T) {
	entries := []archiveEntry{
		{name: "opfor_0.1.0_linux_amd64/LICENSE", mode: 0o644, data: []byte("license\n")},
		{name: "opfor_0.1.0_linux_amd64/opfor", mode: 0o755, data: []byte("binary\n")},
	}

	t.Run("tar gzip", func(t *testing.T) {
		first := new(bytes.Buffer)
		second := new(bytes.Buffer)
		if err := writeTarGzip(first, entries); err != nil {
			t.Fatal(err)
		}
		if err := writeTarGzip(second, entries); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("tar.gz output is not deterministic")
		}
		gzipReader, err := gzip.NewReader(bytes.NewReader(first.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		tarReader := tar.NewReader(gzipReader)
		assertTarEntry(t, tarReader, entries[0])
		assertTarEntry(t, tarReader, entries[1])
		if header, err := tarReader.Next(); err != io.EOF || header != nil {
			t.Fatalf("trailing tar entry = %#v, %v", header, err)
		}
		if err := gzipReader.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("zip", func(t *testing.T) {
		first := new(bytes.Buffer)
		second := new(bytes.Buffer)
		if err := writeZip(first, entries); err != nil {
			t.Fatal(err)
		}
		if err := writeZip(second, entries); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("zip output is not deterministic")
		}
		reader, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
		if err != nil {
			t.Fatal(err)
		}
		if len(reader.File) != len(entries) {
			t.Fatalf("zip entries = %d, want %d", len(reader.File), len(entries))
		}
		for index, file := range reader.File {
			entry := entries[index]
			if file.Name != entry.name || file.Mode().Perm() != entry.mode.Perm() || !file.Modified.Equal(archiveTime) {
				t.Errorf("zip entry %d = %q mode %o time %s", index, file.Name, file.Mode().Perm(), file.Modified)
			}
			stream, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(stream)
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(content, entry.data) {
				t.Errorf("zip entry %q content = %q", file.Name, content)
			}
		}
	})
}

func TestWriteChecksums(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "a.tar.gz")
	second := filepath.Join(directory, "b.zip")
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "checksums.txt")
	if err := writeChecksums(destination, []string{first, second}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x  a.tar.gz\n%x  b.zip\n", sha256.Sum256([]byte("first")), sha256.Sum256([]byte("second")))
	if string(content) != want {
		t.Fatalf("checksums = %q, want %q", content, want)
	}
	if info, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o644 {
		t.Fatalf("checksums mode = %o, want 644", info.Mode().Perm())
	}
}

func TestWriteAtomicFailureLeavesNoDestination(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "artifact")
	wantErr := fmt.Errorf("deliberate")
	err := writeAtomic(destination, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, "partial"); err != nil {
			return err
		}
		return wantErr
	})
	if err != wantErr {
		t.Fatalf("writeAtomic error = %v, want %v", err, wantErr)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination stat error = %v, want not-exist", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for index, entry := range entries {
			names[index] = entry.Name()
		}
		t.Fatalf("temporary files remain: %s", strings.Join(names, ", "))
	}
}

func TestReleaseBuildEnvironmentOverridesUserGoSettings(t *testing.T) {
	t.Setenv("GOFLAGS", "-race")
	t.Setenv("GOENV", "/tmp/untrusted-go-env")
	t.Setenv("GOWORK", "/tmp/untrusted-go-work")
	t.Setenv("GOEXPERIMENT", "fieldtrack")
	t.Setenv("GOFIPS140", "latest")
	environment := releaseBuildEnvironment(target{goos: "linux", goarch: "arm64"})
	values := make(map[string]string)
	counts := make(map[string]int)
	for _, variable := range environment {
		name, value, _ := strings.Cut(variable, "=")
		upperName := strings.ToUpper(name)
		values[upperName] = value
		counts[upperName]++
	}
	want := map[string]string{
		"CGO_ENABLED":  "0",
		"GO111MODULE":  "on",
		"GOAMD64":      "v1",
		"GOARCH":       "arm64",
		"GOARM64":      "v8.0",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFIPS140":    "off",
		"GOFLAGS":      "",
		"GOOS":         "linux",
		"GOTOOLCHAIN":  "local",
		"GOWORK":       "off",
	}
	for name, value := range want {
		if values[name] != value || counts[name] != 1 {
			t.Errorf("%s = %q with count %d, want %q exactly once", name, values[name], counts[name], value)
		}
	}
}

func TestReadReleaseDocumentationIncludesNotices(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := readReleaseDocumentation(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.name
		if entry.mode != 0o644 || len(entry.data) == 0 {
			t.Errorf("documentation entry %q has mode %o and %d bytes", entry.name, entry.mode, len(entry.data))
		}
	}
	for _, name := range []string{
		"LICENSE",
		"NOTICE",
		"README.md",
		"third_party_licenses/Cobalt-Strike-aggressor_script_examples-Apache-2.0.txt",
		"third_party_licenses/Cobalt-Strike-sleep-BSD-3-Clause.txt",
		"third_party_licenses/Unicode-3.0.txt",
		"third_party_licenses/pflag-BSD-3-Clause.txt",
		"third_party_licenses/psilva261-timsort-MIT.txt",
		"third_party_licenses/regexp2-MIT.txt",
	} {
		if !containsString(names, name) {
			t.Errorf("release documentation omits %q; got %q", name, names)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertTarEntry(t *testing.T, reader *tar.Reader, entry archiveEntry) {
	t.Helper()
	header, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != entry.name || os.FileMode(header.Mode).Perm() != entry.mode.Perm() || !header.ModTime.Equal(archiveTime) {
		t.Errorf("tar entry = %q mode %o time %s", header.Name, header.Mode, header.ModTime)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, entry.data) {
		t.Errorf("tar entry %q content = %q", header.Name, content)
	}
}
