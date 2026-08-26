package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
	"github.com/sliverarmory/opfor/internal/parser"
)

func TestOfficialSleepSyntaxCorpus(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "upstream", "sleep-2.1", "programs", "*.sl"))
	if got, want := len(paths), 342; got != want {
		t.Fatalf("official Sleep program count = %d, want %d", got, want)
	}

	// errors1/2/3/5 exercise malformed delimiters or top-level syntax. errors4
	// contains invalid parsed-literal alignments. hoeserror and sillysyntax use
	// invalid object-access argument separators. noterm/noterm2 intentionally
	// omit the explicit terminator required by the reference parser for return
	// statements. argerr/keyvalueerr omit commas between key/value pairs, while
	// concaterrs contains the deliberately ambiguous numeric term 1.2.3.4.5.6.
	// warn.sl's unusual "*8 30" spelling is a dynamically bridgeable Sleep
	// operator, not a parse error. The remaining 330 programs are the current
	// parser-acceptance set. Later compile phases can still reject them: class
	// resolution and import validation (including imperror.sl) are tracked
	// separately from this top-level syntax corpus.
	expectedErrors := map[string]bool{
		"argerr.sl":      true,
		"concaterrs.sl":  true,
		"errors1.sl":     true,
		"errors2.sl":     true,
		"errors3.sl":     true,
		"errors4.sl":     true,
		"errors5.sl":     true,
		"hoeserror.sl":   true,
		"keyvalueerr.sl": true,
		"noterm.sl":      true,
		"noterm2.sl":     true,
		"sillysyntax.sl": true,
	}
	if got, want := len(expectedErrors), 12; got != want {
		t.Fatalf("official Sleep parser-error count = %d, want %d", got, want)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result := parser.Parse(lexer.NewSource(filepath.Base(path), data))
		name := filepath.Base(path)
		if got, want := result.HasErrors(), expectedErrors[name]; got != want {
			t.Errorf("%s HasErrors() = %v, want %v; diagnostics: %v", name, got, want, result.Diagnostics)
		}
	}
}
