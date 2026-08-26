package parser

import (
	"testing"

	"github.com/sliverarmory/opfor/internal/lexer"
)

const maximumParserFuzzBytes = 64 << 10

func FuzzParse(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`sub twice { return $1 * 2; } return twice(21);`),
		[]byte(`foreach $key => $value (%items) { println($key, $value); }`),
		[]byte(`popup beacon_bottom { item "Run" { return $1; } }`),
		[]byte(`try { throw "boom"; } catch $error { warn($error); }`),
		[]byte("\x00\xff\n{[("),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maximumParserFuzzBytes {
			t.Skip()
		}
		options := CompatibilityOptions()
		options.MaximumDiagnostics = 64
		result := ParseWithOptions(lexer.NewSource("<parser-fuzz>", data), options)
		for index, diagnostic := range result.Diagnostics {
			span := diagnostic.Span
			if span.Start.Offset < 0 || span.End.Offset < span.Start.Offset || span.End.Offset > len(data) {
				t.Fatalf("diagnostic %d span offsets = %d..%d, source length %d", index, span.Start.Offset, span.End.Offset, len(data))
			}
		}
	})
}
