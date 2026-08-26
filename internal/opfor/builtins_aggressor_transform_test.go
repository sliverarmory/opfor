package opfor

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAggressorTransformSourceBackedSelectors(t *testing.T) {
	t.Parallel()

	function := (&Runtime{}).aggressorPortableUtilityFunctions()["transform"]
	if function == nil {
		t.Fatal("transform is not registered as a portable Aggressor function")
	}
	tests := []struct {
		name     string
		input    Value
		selector string
		want     string
	}{
		{
			name:     "hex binary octets",
			input:    BinaryString([]byte{0x00, 0x0f, 0x10, 0x80, 0xff}),
			selector: "hex",
			want:     "000f1080ff",
		},
		{
			name:     "PowerShell documented expression",
			input:    String("2 + 2"),
			selector: "powershell-base64",
			want:     "MgAgACsAIAAyAA==",
		},
		{
			name:     "Veil binary octets",
			input:    BinaryString([]byte{0x00, 0x0f, 0x10, 0x80, 0xff}),
			selector: "veil",
			want:     `\x00\x0f\x10\x80\xff`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value, err := function(context.Background(), aggressorPortableInvocation(
				"transform", test.input, String(test.selector),
			))
			if err != nil {
				t.Fatalf("transform(%q): %v", test.selector, err)
			}
			if value.String() != test.want || value.IsBinaryString() {
				t.Fatalf("transform(%q) = %q/binary=%v, want %q/text", test.selector, value.String(), value.IsBinaryString(), test.want)
			}
		})
	}
}

func TestAggressorTransformRejectsUnderspecifiedAndUnknownSelectors(t *testing.T) {
	t.Parallel()

	function := (&Runtime{}).aggressorPortableUtilityFunctions()["transform"]
	for _, selector := range []string{"array", "vba", "vbs", "HEX", "unknown"} {
		selector := selector
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			_, err := function(context.Background(), aggressorPortableInvocation(
				"transform", BinaryString([]byte("payload")), String(selector),
			))
			var argumentErr *PortableUtilityArgumentError
			if !errors.As(err, &argumentErr) || argumentErr.Function != "transform" || argumentErr.Position != 2 {
				t.Fatalf("transform(%q) error = %T %v, want argument 2 *PortableUtilityArgumentError", selector, err, err)
			}
			if selector == "array" || selector == "vba" || selector == "vbs" {
				if !strings.Contains(argumentErr.Reason, "no complete output grammar") {
					t.Fatalf("transform(%q) reason = %q, want explicit public-grammar boundary", selector, argumentErr.Reason)
				}
			}
		})
	}
}

func TestAggressorTransformDocumentsBinaryAndNonBMPEdgePolicy(t *testing.T) {
	t.Parallel()

	function := (&Runtime{}).aggressorPortableUtilityFunctions()["transform"]
	tests := []struct {
		name     string
		input    Value
		selector string
		want     string
	}{
		{
			name:     "binary octets remain individual low-byte units",
			input:    BinaryString([]byte{0xc3, 0xa9}),
			selector: "hex",
			want:     "c3a9",
		},
		{
			name:     "text code units narrow for byte transforms",
			input:    String("é😀"),
			selector: "hex",
			want:     "e93d00",
		},
		{
			name:     "binary octets become UTF-16LE code units for PowerShell",
			input:    BinaryString([]byte{0x00, 0x80, 0xff}),
			selector: "powershell-base64",
			want:     "AACAAP8A",
		},
		{
			name:     "non-BMP text preserves its surrogate pair for PowerShell",
			input:    String("😀"),
			selector: "powershell-base64",
			want:     "PdgA3g==",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value, err := function(context.Background(), aggressorPortableInvocation(
				"transform", test.input, String(test.selector),
			))
			if err != nil {
				t.Fatalf("transform(%q): %v", test.selector, err)
			}
			if value.String() != test.want {
				t.Fatalf("transform(%q) = %q, want provisional edge-policy result %q", test.selector, value.String(), test.want)
			}
		})
	}
}

func TestAggressorTransformFunctionsRequireDocumentedArity(t *testing.T) {
	t.Parallel()

	runtimeInstance := &Runtime{}
	functions := runtimeInstance.aggressorPortableUtilityFunctions()
	for _, name := range []string{"transform", "powershell_command"} {
		for _, arguments := range [][]Value{
			{String("only-one")},
			{String("one"), String("two"), String("extra")},
		} {
			_, err := functions[name](context.Background(), aggressorPortableInvocation(name, arguments...))
			if err == nil || !strings.Contains(err.Error(), "expected exactly 2 argument(s)") {
				t.Errorf("%s arity %d error = %v, want exact-two rejection", name, len(arguments), err)
			}
		}
	}
}

func TestAggressorPowerShellCommandDocumentedDefaults(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	tests := []struct {
		name   string
		remote Value
		want   string
	}{
		{
			name:   "local",
			remote: Null(),
			want:   "powershell -nop -exec bypass -EncodedCommand MgAgACsAIAAyAA==",
		},
		{
			name:   "remote",
			remote: Int(1),
			want:   "powershell -nop -w hidden -encodedcommand MgAgACsAIAAyAA==",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := runtimeInstance.Invoke(context.Background(), "powershell_command", String("2 + 2"), test.remote)
			if err != nil {
				t.Fatalf("powershell_command: %v", err)
			}
			if value.String() != test.want || value.IsBinaryString() {
				t.Fatalf("powershell_command = %q/binary=%v, want %q/text", value.String(), value.IsBinaryString(), test.want)
			}
		})
	}
}

func TestAggressorPowerShellCommandUsesNewestHookAndHonorsLifecycle(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithStdout(io.Discard), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	program, err := CompileString("powershell-command-hook.cna", `
set POWERSHELL_COMMAND { return "old"; }
set POWERSHELL_COMMAND { return "hook:" . $1 . ":" . $2; }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	value, err := runtimeInstance.Invoke(context.Background(), "powershell_command", String("whoami"), Int(1))
	if err != nil {
		t.Fatalf("hooked powershell_command: %v", err)
	}
	if got, want := value.String(), "hook:whoami:1"; got != want {
		t.Fatalf("hooked powershell_command = %q, want %q", got, want)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	value, err = runtimeInstance.Invoke(context.Background(), "powershell_command", String("2 + 2"), Null())
	if err != nil {
		t.Fatalf("default powershell_command after unload: %v", err)
	}
	if got, want := value.String(), "powershell -nop -exec bypass -EncodedCommand MgAgACsAIAAyAA=="; got != want {
		t.Fatalf("powershell_command after hook unload = %q, want %q", got, want)
	}
}

func TestAggressorTransformImporterOverridesTakePrecedence(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithFunction("transform", func(context.Context, Invocation) (Value, error) {
			return String("importer-transform"), nil
		}),
		WithFunction("powershell_command", func(context.Context, Invocation) (Value, error) {
			return String("importer-powershell"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	// Deliberately omit the portable functions' two arguments. Success proves
	// importer functions are selected before portable arity and hook behavior.
	for name, want := range map[string]string{
		"transform":          "importer-transform",
		"powershell_command": "importer-powershell",
	} {
		value, err := runtimeInstance.Invoke(context.Background(), name)
		if err != nil {
			t.Errorf("Invoke(%s override): %v", name, err)
			continue
		}
		if value.String() != want {
			t.Errorf("Invoke(%s override) = %q, want %q", name, value.String(), want)
		}
	}
}

func TestAggressorTransformsHonorCanceledContexts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	tests := []struct {
		name      string
		arguments []Value
	}{
		{name: "transform", arguments: []Value{BinaryString([]byte("payload")), String("hex")}},
		{name: "powershell_command", arguments: []Value{String("2 + 2"), Null()}},
	}
	for _, test := range tests {
		if _, err := functions[test.name](ctx, aggressorPortableInvocation(test.name, test.arguments...)); !errors.Is(err, context.Canceled) {
			t.Errorf("%s canceled error = %v, want context.Canceled", test.name, err)
		}
	}
}

func TestAggressorTransformsCancelDuringNativeLoops(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorPortableUtilityFunctions()
	large := BinaryString(make([]byte, aggressorUtilityChunkSize*2+1))
	tests := []struct {
		name      string
		arguments []Value
	}{
		{name: "transform", arguments: []Value{large, String("powershell-base64")}},
		{name: "powershell_command", arguments: []Value{large, Null()}},
	}
	for _, test := range tests {
		// Admission and the initial conversion checks consume four probes; the
		// next chunk-boundary probe cancels from inside the native loop.
		ctx := newCancelAfterChecksContext(4)
		if _, err := functions[test.name](ctx, aggressorPortableInvocation(test.name, test.arguments...)); !errors.Is(err, context.Canceled) {
			t.Errorf("%s mid-loop cancellation error = %v, want context.Canceled", test.name, err)
		}
	}
}
