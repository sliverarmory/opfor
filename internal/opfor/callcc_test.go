package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCallCCCanonicalHandoff(t *testing.T) {
	tests := []string{
		"callcc_return",
		"callccbg",
		"callcc_foreach",
		"callccpcon",
		"callccr",
		"callcc_ifonce",
		"callcc_other_callers",
		"callcc_prodcon",
		"callcc_tcatch",
		"callcc_tcatch2",
		"callcc_trycatch",
		"inlinelocalcallcc",
	}

	for _, name := range tests {
		name := name
		t.Run(name, func(t *testing.T) {
			programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(name+".sl", programBytes))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}

			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = runtime.Execute(ctx, program)
			cancel()
			if err != nil {
				t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestCallCCPassesSourceClosureAndMessage(t *testing.T) {
	program, err := CompileString("callcc-contract.sl", `
sub target {
    println($0);
    println($1);
    return [$1: "resumed"];
}
sub source {
    callcc &target;
    return $1;
}
println(source());
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	want := "CALLCC\n&closure[callcc-contract.sl:8-9]#2\nresumed\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestCallCCSuspensionsResumeLIFO(t *testing.T) {
	program, err := CompileString("callcc-lifo.sl", `
sub source {
    if ($1 == 0) {
        source(1);
        callcc { return "outer parked"; };
        return "outer resumed";
    }
    callcc { return "inner parked"; };
    return "inner resumed";
}
println(source(0));
println(source());
println(source());
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	want := "outer parked\nouter resumed\ninner resumed\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
