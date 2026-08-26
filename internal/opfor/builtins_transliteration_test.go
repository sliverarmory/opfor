package opfor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSleepTransliterationCanonicalBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text, pattern, mapper, options string
		want                           string
	}{
		{text: "this is a test uNF 12345!!!!", pattern: "a-zA-Z", mapper: "n-za-mN-ZA-M", want: "guvf vf n grfg hAS 12345!!!!"},
		{text: "AAA BABA BBB", pattern: "AB", mapper: "BA", want: "BBB ABAB AAA"},
		{text: "Th1s 1s 4 t3st", pattern: `\d`, mapper: "", want: "Ths s  tst"},
		{text: "Th1s 1s 4 t3st", pattern: `\d`, mapper: "", options: "c", want: "1143"},
		{text: "AAA BABA BBB", pattern: "AB", mapper: "BA", options: "s", want: "B ABAB A"},
		{text: "abc", pattern: "abc", mapper: "X", want: "XXX"},
		{text: "abc", pattern: "abc", mapper: "X", options: "d", want: "X"},
		{text: "cba", pattern: "c-a", mapper: "1-3", want: "123"},
		{text: ".a", pattern: `\.`, mapper: "X", want: "Xa"},
		{text: ".a", pattern: ".", mapper: "X", want: "XX"},
		{text: "ABBA", pattern: "AB", mapper: "BA", options: "s", want: "BAB"},
		// Complement is applied per pattern element by the reference bridge,
		// rather than once to the union of all elements.
		{text: "ABC", pattern: "AB", mapper: "xy", options: "c", want: "yxx"},
		// Character.isWhitespace deliberately excludes Java's non-breaking
		// spaces even though they belong to Unicode's space category.
		{text: " \u00a0", pattern: `\s`, mapper: "", want: "\u00a0"},
		// Java transliteration visits UTF-16 chars, so each surrogate half is
		// independently mapped.
		{text: "😀", pattern: "😀", mapper: "XY", want: "XY"},
	}
	for _, test := range tests {
		got, err := invokeSleepTr(test.text, test.pattern, test.mapper, test.options)
		if err != nil {
			t.Fatalf("tr(%q, %q, %q, %q): %v", test.text, test.pattern, test.mapper, test.options, err)
		}
		if got != test.want {
			t.Errorf("tr(%q, %q, %q, %q) = %q, want %q", test.text, test.pattern, test.mapper, test.options, got, test.want)
		}
	}
}

func TestSleepTransliterationPatternErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		want    string
	}{
		{pattern: "-a", want: "Dangling range"},
		{pattern: "a-", want: "Dangling range"},
		{pattern: `\q`, want: "unrecognized escaped"},
		{pattern: `a\`, want: "escape end"},
	}
	for _, test := range tests {
		_, err := invokeSleepTr("input", test.pattern, "", "")
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("tr pattern %q error = %v, want %q", test.pattern, err, test.want)
		}
	}
}

func TestSleepTransliterationUsesPinnedUnicode17Classes(t *testing.T) {
	t.Parallel()

	if javaRegexUnicodeVersion != "17.0.0" {
		t.Fatalf("Unicode property version = %q, want 17.0.0", javaRegexUnicodeVersion)
	}
	// U+1C89 CYRILLIC CAPITAL LETTER TJE was added after the Unicode version
	// used by older Go toolchains. The generated table keeps \w deterministic.
	got, err := invokeSleepTr("\u1c89\u1c8b", `\w`, "X", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "X\u1c8b" {
		t.Fatalf("Unicode 17 \\w transliteration = %q, want %q", got, "X\u1c8b")
	}
	if !sleepTrMatches(0x0665, sleepTrElement{item: 'd', special: true}, 0) {
		t.Fatal("Unicode 17 DIGIT table did not classify ARABIC-INDIC DIGIT FIVE")
	}
}

func TestSleepTransliterationCompilationHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx := newTransliterationCancelAfterChecksContext(4)
	pattern := sleepStringValueFromUnits([]uint16{0, '-', 0xffff}, nil)
	_, err := compileSleepTransliteration(ctx, pattern, String("X"), 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("compile error = %v, want context.Canceled", err)
	}
}

func TestSleepTransliterationApplicationHonorsCancellation(t *testing.T) {
	t.Parallel()

	elements := make([]sleepTrElement, 1<<16)
	for index := range elements {
		elements[index] = sleepTrElement{item: 'a', replacement: 'x'}
	}
	ctx := newTransliterationCancelAfterChecksContext(3)
	_, err := applySleepTransliteration(ctx, String("b"), elements, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v, want context.Canceled", err)
	}
}

func invokeSleepTr(text, pattern, mapper, options string) (string, error) {
	value, err := builtinSleepTr(context.Background(), Invocation{
		Name: "tr",
		Arguments: []Argument{
			{Value: String(text)},
			{Value: String(pattern)},
			{Value: String(mapper)},
			{Value: String(options)},
		},
	})
	return value.String(), err
}

type transliterationCancelAfterChecksContext struct {
	context.Context
	remaining atomic.Int32
	done      chan struct{}
	once      sync.Once
}

func newTransliterationCancelAfterChecksContext(checks int32) *transliterationCancelAfterChecksContext {
	ctx := &transliterationCancelAfterChecksContext{Context: context.Background(), done: make(chan struct{})}
	ctx.remaining.Store(checks)
	return ctx
}

func (ctx *transliterationCancelAfterChecksContext) Done() <-chan struct{} { return ctx.done }

func (ctx *transliterationCancelAfterChecksContext) Err() error {
	if ctx.remaining.Add(-1) < 0 {
		ctx.once.Do(func() { close(ctx.done) })
	}
	select {
	case <-ctx.done:
		return context.Canceled
	default:
		return nil
	}
}
