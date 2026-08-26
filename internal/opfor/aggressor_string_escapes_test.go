package opfor

import (
	"bytes"
	"context"
	"testing"
)

func TestAggressorConsoleFormattingEscapesMatchParserConstants(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(
		context.Background(),
		"formatting-escapes.cna",
		`return "\c0zero\cFwhite\Uunder\U\oreset";`,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Cobalt Strike installs c, U, and o through Sleep's ParserConfig as
	// U+0003 (color), U+001F (underline), and U+000F (reset), respectively.
	want := "\x030zero\x03Fwhite\x1funder\x1f\x0freset"
	if got := value.String(); got != want {
		t.Fatalf("formatted literal bytes = % x, want % x", []byte(got), []byte(want))
	}
}

func TestAggressorConsoleFormattingEscapesTerminateInterpolationVariables(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), "formatting-interpolation.cna", `
$value = "payload";
return "\c4$value\o:\U$value\U";
`)
	if err != nil {
		t.Fatal(err)
	}

	want := "\x034payload\x0f:\x1fpayload\x1f"
	if got := value.String(); got != want {
		t.Fatalf("interpolated formatting bytes = % x, want % x", []byte(got), []byte(want))
	}
}

func TestAggressorConsoleFormattingEscapesReachHostArguments(t *testing.T) {
	t.Parallel()

	var calls []Invocation
	runtime, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls = append(calls, invocation)
		return Null(), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Eval(context.Background(), "formatting-host.cna", `
$value = "host";
capture("\cE$value\o", "\Uunder\U");
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "capture" {
		t.Fatalf("host calls = %#v, want one capture call", calls)
	}
	values := calls[0].Values()
	if len(values) != 2 {
		t.Fatalf("capture arguments = %d, want 2", len(values))
	}
	want := []string{"\x03Ehost\x0f", "\x1funder\x1f"}
	for index := range want {
		if got := values[index].String(); got != want[index] {
			t.Errorf("argument %d bytes = % x, want % x", index, []byte(got), []byte(want[index]))
		}
	}
}

func TestAggressorConsoleFormattingEscapesRemainLiteralInSingleQuotes(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(
		context.Background(),
		"formatting-single-quote.cna",
		`return '\c4literal\Uunder\U\o';`,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `\c4literal\Uunder\U\o`
	if got := value.String(); got != want {
		t.Fatalf("single-quoted literal = %q, want %q", got, want)
	}
}

func TestAggressorConsoleFormattingEscapesCanBeEscaped(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(
		context.Background(),
		"formatting-escaped.cna",
		`return "\\c4\\U\\o";`,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `\c4\U\o`
	if got := value.String(); got != want {
		t.Fatalf("escaped formatting literal = %q, want %q", got, want)
	}
}

func TestAggressorConsoleFormattingEscapesSurviveClosureSerialization(t *testing.T) {
	t.Parallel()

	producerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	producerProgram, err := CompileString("formatting-producer.cna", `
$callback = lambda({ return "\c4$value\o"; }, $value => "payload");
`)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := producerRuntime.Load(context.Background(), producerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producerRuntime.Close(context.Background()) })

	encoded, err := encodeSleepScalarStream(producer.Get("$callback"))
	if err != nil {
		t.Fatal(err)
	}

	consumerRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	consumerProgram, err := CompileString("formatting-consumer.cna", "return 1;")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := consumerRuntime.Load(context.Background(), consumerProgram)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumerRuntime.Close(context.Background()) })

	decoded, _, err := decodeSleepScalarStreamForScript(bytes.NewReader(encoded), consumer)
	if err != nil {
		t.Fatal(err)
	}
	callable, ok := decoded.Function()
	if !ok {
		t.Fatalf("decoded callback = %s, want function", decoded.Describe())
	}
	value, err := callable.Invoke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "\x034payload\x0f"
	if got := value.String(); got != want {
		t.Fatalf("serialized callback result bytes = % x, want % x", []byte(got), []byte(want))
	}
}
