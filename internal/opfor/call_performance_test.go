package opfor

import (
	"bytes"
	"context"
	"testing"

	"github.com/sliverarmory/opfor/internal/ast"
)

func TestCallArgumentOrderRetainsLiveReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "positional",
			source: `$value = 1;
sub mutate { $value = $value + 1; return $value; }
sub accept { println($1 . ":" . $2 . ":" . $3); $2 = 10; println(@_[1]); }
accept(mutate(), $value, mutate());
println($value);`,
			want: "3:3:2\n10\n10\n",
		},
		{
			name: "named-pairs",
			source: `$value = 1;
sub mutate { $value = $value + 1; return $value; }
sub accept { println($named . ":" . $1 . ":" . $other); $other = 20; }
accept($named => mutate(), mutate(), $other => $value);
println($value);`,
			want: "3:2:3\n20\n",
		},
		{
			name: "omitted-comma",
			source: `$value = 1;
sub mutate { $value = $value + 1; return $value; }
sub accept { println($1 . ":" . $2 . ":" . $3); $1 = 30; println(@_[0]); }
accept(mutate() $value, mutate());
println($value);`,
			want: "3:3:2\n30\n30\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := runSleepParameterCompatibilityProbe(t, test.name+".sl", test.source); got != test.want {
				t.Fatalf("output = %q, want %q", got, test.want)
			}
		})
	}
}

type callTraceFormattingProbe struct {
	descriptions int
	invoke       func()
}

func (probe *callTraceFormattingProbe) String() string {
	probe.descriptions++
	return "&probe"
}

func (probe *callTraceFormattingProbe) Invoke(context.Context, ...Value) (Value, error) {
	probe.invoke()
	return Int(7), nil
}

func TestClosureTraceFormattingIsLazyAcrossDebugChanges(t *testing.T) {
	for _, test := range []struct {
		name       string
		before     int32
		after      int32
		wantFormat bool
		wantTrace  bool
	}{
		{name: "disabled", before: 0, after: 0},
		{name: "enabled", before: 8, after: 8, wantFormat: true, wantTrace: true},
		{name: "suppressed", before: 24, after: 24},
		{name: "enables-during-call", before: 0, after: 8},
		{name: "disables-during-call", before: 8, after: 0, wantFormat: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			runtime, err := New(WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			script := &Script{runtime: runtime, debug: test.before}
			fiber := &fiber{closure: &scriptClosure{script: script}}
			probe := &callTraceFormattingProbe{invoke: func() {
				script.mu.Lock()
				script.debug = test.after
				script.mu.Unlock()
			}}
			value, err := fiber.invokeCallableAt(context.Background(), &ast.CallExpr{}, FunctionValue(probe), nil)
			if err != nil || value.Int32() != 7 {
				t.Fatalf("call = (%v, %v), want (7, nil)", value, err)
			}
			if got := probe.descriptions != 0; got != test.wantFormat {
				t.Fatalf("formatted = %v, want %v", got, test.wantFormat)
			}
			if got := output.Len() != 0; got != test.wantTrace {
				t.Fatalf("trace = %q, want trace present %v", output.String(), test.wantTrace)
			}
		})
	}
}

func BenchmarkSleepMultiArgumentCalls(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "multi-argument-calls", `
sub advance { return $1 + $2 + $3; }
sub benchmark {
    $value = 0;
    for ($index = 0; $index < 1000; $index++) {
        $value = advance($value, 1, 2);
    }
    return $value;
}
`, 3000)
}

func BenchmarkSleepBracketClosureCalls(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "bracket-closure-calls", `
sub advance { return $1 + 1; }
sub benchmark {
    $value = 0;
    for ($index = 0; $index < 1000; $index++) {
        $value = [&advance: $value];
    }
    return $value;
}
`, 1000)
}
