package opfor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestInvokeConsoleCommandUsesRawMessageAndQuoteAwareArguments(t *testing.T) {
	t.Parallel()

	runtime := loadConsoleInvocationFixture(t)
	raw := "inspect one \"two words\" \"\" c:\\temp 'literal quotes'"
	value, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind:     BindingCommand,
		Name:     "inspect",
		RawInput: raw,
	})
	if err != nil {
		t.Fatalf("InvokeConsole: %v", err)
	}

	items := consoleResultValues(t, value)
	if got := items[0].String(); got != raw {
		t.Fatalf("$0 = %q, want exact raw input %q", got, raw)
	}
	assertConsoleArgumentArray(t, "@_", items[1], []string{"one", "two words", "", `c:\temp`, "'literal", "quotes'"})
	if got, want := consoleValueStrings(items[2:]), []string{"one", "two words", "", `c:\temp`, "'literal", "quotes'"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("positional arguments = %q, want %q", got, want)
	}
}

func TestInvokeConsoleAliasesPrependSessionAndPreserveRawMessage(t *testing.T) {
	t.Parallel()

	for _, kind := range []BindingKind{BindingAlias, BindingSSHAlias} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			runtime := loadConsoleInvocationFixture(t)
			raw := "inspect alpha \"two words\" \"\""
			value, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
				Kind:      kind,
				Name:      "inspect",
				RawInput:  raw,
				SessionID: String("session-7"),
			})
			if err != nil {
				t.Fatalf("InvokeConsole: %v", err)
			}

			items := consoleResultValues(t, value)
			if got := items[0].String(); got != raw {
				t.Fatalf("$0 = %q, want exact raw input %q", got, raw)
			}
			want := []string{"session-7", "alpha", "two words", ""}
			assertConsoleArgumentArray(t, "@_", items[1], want)
			if got := consoleValueStrings(items[2:]); !reflect.DeepEqual(got, want) {
				t.Fatalf("positional arguments = %q, want %q", got, want)
			}
		})
	}
}

func TestInvokeConsoleEmptyInputUsesBindingName(t *testing.T) {
	t.Parallel()

	runtime := loadConsoleInvocationFixture(t)
	value, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingCommand,
		Name: "inspect",
	})
	if err != nil {
		t.Fatalf("InvokeConsole: %v", err)
	}
	items := consoleResultValues(t, value)
	if items[0].String() != "inspect" {
		t.Fatalf("$0 = %q, want %q", items[0].String(), "inspect")
	}
	assertConsoleArgumentArray(t, "@_", items[1], []string{})
}

func TestInvokeConsoleRejectsInvalidInputWithoutRunningCallback(t *testing.T) {
	t.Parallel()

	runtime := loadConsoleInvocationFixture(t)
	tests := []struct {
		name       string
		invocation ConsoleInvocation
		want       string
	}{
		{
			name:       "non-console binding kind",
			invocation: ConsoleInvocation{Kind: BindingEvent, Name: "inspect", RawInput: "inspect"},
			want:       "not a console command or alias",
		},
		{
			name:       "empty binding name",
			invocation: ConsoleInvocation{Kind: BindingCommand},
			want:       "binding name is empty",
		},
		{
			name:       "raw command mismatch",
			invocation: ConsoleInvocation{Kind: BindingCommand, Name: "inspect", RawInput: "other value"},
			want:       `command "other" does not match binding "inspect"`,
		},
		{
			name:       "unterminated quote",
			invocation: ConsoleInvocation{Kind: BindingCommand, Name: "inspect", RawInput: `inspect "value`},
			want:       "unterminated double quote",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := runtime.InvokeConsole(context.Background(), test.invocation); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("InvokeConsole error = %v, want substring %q", err, test.want)
			}
		})
	}

	_, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind:     BindingCommand,
		Name:     "missing",
		RawInput: "missing",
	})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Operation != "command" || unsupported.Name != "missing" {
		t.Fatalf("missing binding error = %#v, want command UnsupportedError", err)
	}
}

func TestInvokeBindingRetainsLegacyPositionalContract(t *testing.T) {
	t.Parallel()

	runtime := loadConsoleInvocationFixture(t)
	value, err := runtime.InvokeBinding(
		context.Background(),
		BindingAlias,
		"inspect",
		String("session-legacy"),
		String("argument"),
	)
	if err != nil {
		t.Fatalf("InvokeBinding: %v", err)
	}
	items := consoleResultValues(t, value)
	if !items[0].IsNull() {
		t.Fatalf("legacy $0 = %s, want $null", items[0].Describe())
	}
	assertConsoleArgumentArray(t, "legacy @_", items[1], []string{"session-legacy", "argument"})
}

func loadConsoleInvocationFixture(t *testing.T) *Runtime {
	t.Helper()
	program, err := CompileString("console-invocation.cna", `
command inspect { return @($0, @_, $1, $2, $3, $4, $5, $6); }
alias inspect { return @($0, @_, $1, $2, $3, $4); }
ssh_alias inspect { return @($0, @_, $1, $2, $3, $4); }
`)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Load(context.Background(), program); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return runtime
}

func consoleResultValues(t *testing.T, value Value) []Value {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("callback result = %s, want array", value.Describe())
	}
	return array.Values()
}

func assertConsoleArgumentArray(t *testing.T, name string, value Value, want []string) {
	t.Helper()
	array, ok := value.Array()
	if !ok {
		t.Fatalf("%s = %s, want array", name, value.Describe())
	}
	if got := consoleValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

func consoleValueStrings(values []Value) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}
