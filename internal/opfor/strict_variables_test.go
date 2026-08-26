package opfor

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestStrictVariableAccessAndForeachDestinations(t *testing.T) {
	program, err := CompileString("strict-foreach.sl", `debug(7);
global('@items');
@items = @("one");
foreach $key => $value (@items) {
    $value = "changed";
    println($key . "=" . $value);
}
foreach $empty (@()) {
    println("never");
}
println($empty);
$written = 1;
println($written);
println(@items);
sub nothing { return $null; }
while $bound (nothing()) {
    println("never");
}
println($bound);
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

	want := "Warning: variable '$key' not declared at strict-foreach.sl:4\n" +
		"0=changed\n" +
		"Warning: variable '$empty' not declared at strict-foreach.sl:8\n" +
		"Warning: variable '$empty' not declared at strict-foreach.sl:11\n" +
		"\n" +
		"Warning: variable '$written' not declared at strict-foreach.sl:12\n" +
		"1\n" +
		"@('changed')\n" +
		"Warning: variable '$bound' not declared at strict-foreach.sl:16\n" +
		"\n"
	if got := output.String(); got != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestStrictVariableCanonicalGoldens(t *testing.T) {
	for _, name := range []string{"brokendec", "dtest1", "feloc", "skew"} {
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
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestStrictVariableMultilineParsedLiteralGolden(t *testing.T) {
	programBytes, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "lineno.sl"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", "lineno.sl"))
	if err != nil {
		t.Fatal(err)
	}
	program, err := Compile(NewSource("lineno.sl", programBytes))
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
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
	}
}

func TestStrictVariableParsedLiteralLinesDistinguishEscapedAndSourceNewlines(t *testing.T) {
	program, err := CompileString("literal-lines.sl",
		"debug(7);\nprintln(\"escaped\\n$first \n$second \");\n")
	if err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	runtime, err := New(WithStdout(io.Discard), WithStderr(&warnings))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	want := "Warning: variable '$first' not declared at literal-lines.sl:2\n" +
		"Warning: variable '$second' not declared at literal-lines.sl:3\n"
	if got := warnings.String(); got != want {
		t.Fatalf("warnings mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestStrictVariableParsedLiteralBareCRKeepsEvalDisplayLine(t *testing.T) {
	program, err := CompileString("eval", "debug(7);\rprintln(\"before\r$missing \");")
	if err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	runtime, err := New(WithStdout(io.Discard), WithStderr(&warnings))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if got, want := warnings.String(), "Warning: variable '$missing' not declared at eval:0\n"; got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
}
