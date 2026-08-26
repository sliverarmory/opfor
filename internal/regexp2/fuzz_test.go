package regexp2

import (
	"context"
	"testing"
	"time"
)

const (
	maximumRegexFuzzPatternBytes = 8 << 10
	maximumRegexFuzzInputBytes   = 32 << 10
)

func FuzzCompileAndMatchContext(f *testing.F) {
	f.Add([]byte(`^(?<word>\p{L}+)(?:\s+\k<word>)?$`), []byte("alpha alpha"), uint16(0))
	f.Add([]byte(`(?s)(a+)+$`), []byte("aaaaaaaaaaaaaaaaaaaaaaaa!"), uint16(0))
	f.Add([]byte(`(?<=\bfoo)bar|\X`), []byte("foobar\U0001f600"), uint16(JavaUnicode))
	f.Add([]byte{0xff, '[', '\\'}, []byte{0, 0xff}, uint16(RightToLeft))

	const supportedOptions = IgnoreCase | Multiline | ExplicitCapture | Singleline |
		IgnorePatternWhitespace | RightToLeft | ECMAScript | RE2 | Unicode | JavaASCII | JavaUnicode

	f.Fuzz(func(t *testing.T, pattern, input []byte, rawOptions uint16) {
		if len(pattern) > maximumRegexFuzzPatternBytes || len(input) > maximumRegexFuzzInputBytes {
			t.Skip()
		}
		expression, err := Compile(string(pattern), RegexOptions(rawOptions)&supportedOptions)
		if err != nil {
			return
		}
		expression.MatchTimeout = 50 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, _ = expression.FindRunesMatchContext(ctx, []rune(string(input)))
	})
}
