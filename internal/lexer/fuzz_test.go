package lexer

import "testing"

const maximumLexerFuzzBytes = 64 << 10

func FuzzLex(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`println("hello");`),
		[]byte(`$value = 0x1.fp3D; $value++;`),
		[]byte(`on ready { println($1); }`),
		[]byte("\"unterminated"),
		{0, 0xff, '\n', '$', 0xc0},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maximumLexerFuzzBytes {
			t.Skip()
		}
		result := Lex(NewSource("<lexer-fuzz>", data))
		if len(result.Tokens) == 0 || result.Tokens[len(result.Tokens)-1].Kind != EOF {
			t.Fatalf("lexer returned %d tokens without terminal EOF", len(result.Tokens))
		}
		for index, token := range result.Tokens {
			assertFuzzSpan(t, "token", index, token.Span, len(data))
			if token.TextSpan.Source != "" {
				assertFuzzSpan(t, "token text", index, token.TextSpan, len(data))
			}
		}
		for index, diagnostic := range result.Diagnostics {
			assertFuzzSpan(t, "diagnostic", index, diagnostic.Span, len(data))
		}
	})
}

func assertFuzzSpan(t *testing.T, kind string, index int, span Span, sourceLength int) {
	t.Helper()
	if span.Start.Offset < 0 || span.End.Offset < span.Start.Offset || span.End.Offset > sourceLength {
		t.Fatalf("%s %d span offsets = %d..%d, source length %d", kind, index, span.Start.Offset, span.End.Offset, sourceLength)
	}
	if span.Start.Line < 1 || span.Start.Column < 1 || span.End.Line < 1 || span.End.Column < 1 {
		t.Fatalf("%s %d span positions = %s", kind, index, span)
	}
}
