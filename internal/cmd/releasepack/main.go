// Command releasepack builds deterministic OPFOR release archives.
//
// It is an internal release-engineering command rather than a second shipped
// binary. Run it from the repository root with:
//
//	go run ./internal/cmd/releasepack -version v0.0.1 -out dist
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sliverarmory/opfor"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

var archiveTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

type target struct {
	goos   string
	goarch string
}

var releaseTargets = []target{
	{goos: "darwin", goarch: "amd64"},
	{goos: "darwin", goarch: "arm64"},
	{goos: "linux", goarch: "amd64"},
	{goos: "linux", goarch: "arm64"},
	{goos: "windows", goarch: "amd64"},
	{goos: "windows", goarch: "arm64"},
}

type archiveEntry struct {
	name string
	mode os.FileMode
	data []byte
}

func main() {
	version := flag.String("version", opfor.Version, "release version embedded in the CLI")
	out := flag.String("out", "dist", "release artifact directory")
	root := flag.String("root", ".", "OPFOR repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fatalf("unexpected positional arguments: %s", strings.Join(flag.Args(), " "))
	}

	artifacts, err := packageRelease(*root, *out, *version)
	if err != nil {
		fatalf("%v", err)
	}
	for _, artifact := range artifacts {
		fmt.Println(artifact)
	}
}

func packageRelease(root, out, version string) ([]string, error) {
	version = strings.TrimSpace(version)
	if !validReleaseVersion(version) {
		return nil, fmt.Errorf("invalid release version %q", version)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact directory: %w", err)
	}
	if err := verifyRepositoryRoot(root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}

	documentation, err := readReleaseDocumentation(root)
	if err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp("", "opfor-releasepack-*")
	if err != nil {
		return nil, fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(temporary)

	fileVersion := strings.TrimPrefix(version, "v")
	artifacts := make([]string, 0, len(releaseTargets)+1)
	for _, target := range releaseTargets {
		base := fmt.Sprintf("opfor_%s_%s_%s", fileVersion, target.goos, target.goarch)
		binaryName := "opfor"
		if target.goos == "windows" {
			binaryName += ".exe"
		}
		binaryPath := filepath.Join(temporary, base, binaryName)
		if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
			return nil, fmt.Errorf("create %s/%s build directory: %w", target.goos, target.goarch, err)
		}
		if err := buildCLI(root, binaryPath, version, target); err != nil {
			return nil, err
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			return nil, fmt.Errorf("read %s/%s binary: %w", target.goos, target.goarch, err)
		}
		entries := make([]archiveEntry, 0, len(documentation)+1)
		entries = append(entries, archiveEntry{name: path.Join(base, binaryName), mode: 0o755, data: binary})
		for _, document := range documentation {
			entries = append(entries, archiveEntry{
				name: path.Join(base, document.name),
				mode: document.mode,
				data: document.data,
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

		var artifact string
		if target.goos == "windows" {
			artifact = filepath.Join(out, base+".zip")
			err = writeAtomic(artifact, func(writer io.Writer) error {
				return writeZip(writer, entries)
			})
		} else {
			artifact = filepath.Join(out, base+".tar.gz")
			err = writeAtomic(artifact, func(writer io.Writer) error {
				return writeTarGzip(writer, entries)
			})
		}
		if err != nil {
			return nil, fmt.Errorf("package %s/%s: %w", target.goos, target.goarch, err)
		}
		artifacts = append(artifacts, artifact)
	}

	sort.Strings(artifacts)
	checksumsPath := filepath.Join(out, "checksums.txt")
	if err := writeChecksums(checksumsPath, artifacts); err != nil {
		return nil, err
	}
	artifacts = append(artifacts, checksumsPath)
	return artifacts, nil
}

func validReleaseVersion(version string) bool {
	if !releaseVersionPattern.MatchString(version) {
		return false
	}

	withoutBuild, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), "+")
	core, prerelease, hasPrerelease := strings.Cut(withoutBuild, "-")
	for _, identifier := range strings.Split(core, ".") {
		if hasLeadingZero(identifier) {
			return false
		}
	}
	if hasPrerelease {
		for _, identifier := range strings.Split(prerelease, ".") {
			if isDecimal(identifier) && hasLeadingZero(identifier) {
				return false
			}
		}
	}
	return true
}

func isDecimal(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}

func hasLeadingZero(identifier string) bool {
	return len(identifier) > 1 && identifier[0] == '0'
}

func verifyRepositoryRoot(root string) error {
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	if !strings.HasPrefix(string(module), "module github.com/sliverarmory/opfor\n") {
		return errors.New("repository root does not contain the OPFOR module")
	}
	for _, name := range []string{"README.md", "LICENSE", "NOTICE", filepath.Join("cmd", "opfor", "main.go")} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			return fmt.Errorf("inspect %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", name)
		}
	}
	return nil
}

func readReleaseDocumentation(root string) ([]archiveEntry, error) {
	entries := make([]archiveEntry, 0, 8)
	for _, name := range []string{"LICENSE", "NOTICE", "README.md"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		entries = append(entries, archiveEntry{name: name, mode: 0o644, data: content})
	}
	licenseDirectory := filepath.Join(root, "third_party_licenses")
	licenses, err := os.ReadDir(licenseDirectory)
	if err != nil {
		return nil, fmt.Errorf("read third_party_licenses: %w", err)
	}
	for _, license := range licenses {
		if !license.Type().IsRegular() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(licenseDirectory, license.Name()))
		if err != nil {
			return nil, fmt.Errorf("read third_party_licenses/%s: %w", license.Name(), err)
		}
		entries = append(entries, archiveEntry{
			name: path.Join("third_party_licenses", license.Name()),
			mode: 0o644,
			data: content,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

func buildCLI(root, output, version string, target target) error {
	command := exec.Command(
		"go", "build", "-trimpath", "-buildvcs=false", "-mod=readonly",
		"-ldflags=-s -w -X main.version="+version,
		"-o", output, "./cmd/opfor",
	)
	command.Dir = root
	command.Env = releaseBuildEnvironment(target)
	outputBytes, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s/%s: %w: %s", target.goos, target.goarch, err, strings.TrimSpace(string(outputBytes)))
	}
	return nil
}

func releaseBuildEnvironment(target target) []string {
	controlled := map[string]string{
		"CGO_ENABLED":  "0",
		"GO111MODULE":  "on",
		"GOAMD64":      "v1",
		"GOARCH":       target.goarch,
		"GOARM64":      "v8.0",
		"GOENV":        "off",
		"GOEXPERIMENT": "",
		"GOFIPS140":    "off",
		"GOFLAGS":      "",
		"GOOS":         target.goos,
		"GOTOOLCHAIN":  "local",
		"GOWORK":       "off",
	}
	environment := make([]string, 0, len(os.Environ())+len(controlled))
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		controlledName := false
		for key := range controlled {
			if strings.EqualFold(name, key) {
				controlledName = true
				break
			}
		}
		if !controlledName {
			environment = append(environment, variable)
		}
	}
	keys := make([]string, 0, len(controlled))
	for key := range controlled {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+controlled[key])
	}
	return environment
}

func writeTarGzip(output io.Writer, entries []archiveEntry) error {
	gzipWriter, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = archiveTime
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Mode:     int64(entry.mode.Perm()),
			Size:     int64(len(entry.data)),
			ModTime:  archiveTime,
			Typeflag: tar.TypeReg,
			Format:   tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(entry.data); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeZip(output io.Writer, entries []archiveEntry) error {
	zipWriter := zip.NewWriter(output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		header.SetModTime(archiveTime)
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(entry.data); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}

func writeChecksums(destination string, artifacts []string) error {
	return writeAtomic(destination, func(writer io.Writer) error {
		for _, artifact := range artifacts {
			content, err := os.ReadFile(artifact)
			if err != nil {
				return fmt.Errorf("read %s for checksum: %w", filepath.Base(artifact), err)
			}
			digest := sha256.Sum256(content)
			if _, err := fmt.Fprintf(writer, "%x  %s\n", digest, filepath.Base(artifact)); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeAtomic(destination string, write func(io.Writer) error) (returnErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+"-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); returnErr == nil && closeErr != nil {
				returnErr = closeErr
			}
		}
		if removeErr := os.Remove(temporaryName); returnErr == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			returnErr = removeErr
		}
	}()
	if err := write(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Rename(temporaryName, destination); err != nil {
		return err
	}
	return nil
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "releasepack: "+format+"\n", arguments...)
	os.Exit(1)
}
