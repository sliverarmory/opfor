package opfor

import (
	"bytes"
	"context"
	"testing"
)

func TestPredicateTraceFormattingRemainsLazyAndCompatible(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "disabled", source: `if (1 < 2) { }`, want: ""},
		{name: "binary", source: `debug(64); if (1 < 2) { }`, want: "Trace: 1 < 2 ? TRUE at binary.sl:1\n"},
		{name: "truth", source: `debug(64); if (7) { }`, want: "Trace: -istrue 7 ? TRUE at truth.sl:1\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, err := CompileString(test.name+".sl", test.source)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			defer runtimeInstance.Close(context.Background())
			if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != test.want {
				t.Fatalf("trace = %q, want %q", got, test.want)
			}
		})
	}
}
