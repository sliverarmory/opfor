package regexp2

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestJavaGraphemeUnicode17Conformance is opt-in because the licensed UCD is
// not duplicated as testdata. The digest pins the final Unicode 17.0.0
// GraphemeBreakTest.txt before every row is exercised.
func TestJavaGraphemeUnicode17Conformance(t *testing.T) {
	path := os.Getenv("OPFOR_GRAPHEME_BREAK_TEST")
	if path == "" {
		t.Skip("set OPFOR_GRAPHEME_BREAK_TEST to Unicode 17.0.0 GraphemeBreakTest.txt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const wantSHA256 = "e2d134d2c52919bace503ebb6a551c1855fe1a1faec18478c78fff254a1793ec"
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantSHA256 {
		t.Fatalf("GraphemeBreakTest.txt SHA-256 = %s, want %s", got, wantSHA256)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		text, want := parseGraphemeBreakTestRow(t, lineNumber, line)
		got := []int{0}
		for offset := 0; offset < len(text); {
			next, nextErr := nextJavaGraphemeBoundary(text, offset, len(text), nil)
			if nextErr != nil {
				t.Fatalf("line %d: %v", lineNumber, nextErr)
			}
			got = append(got, next)
			offset = next
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("line %d: boundaries = %v, want %v for %U", lineNumber, got, want, text)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func parseGraphemeBreakTestRow(t *testing.T, lineNumber int, line string) ([]rune, []int) {
	t.Helper()
	fields := strings.Fields(line)
	text := make([]rune, 0, len(fields)/2)
	boundaries := make([]int, 0, len(fields)/2+1)
	for _, field := range fields {
		switch field {
		case "÷":
			boundaries = append(boundaries, len(text))
		case "×":
			// No boundary at the current position.
		default:
			value, err := strconv.ParseInt(field, 16, 32)
			if err != nil || value < 0 || value > 0x10ffff {
				t.Fatalf("line %d: malformed code point %q", lineNumber, field)
			}
			text = append(text, rune(value))
		}
	}
	return text, boundaries
}
