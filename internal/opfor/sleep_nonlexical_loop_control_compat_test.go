package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type sleepLoopControlProbe struct {
	name    string
	source  string
	include string
	want    string
}

var sleepLoopControlProbes = []sleepLoopControlProbe{
	{
		name:   "top-level-break.sl",
		source: "println(\"top-before\"); break; println(\"top-after\");\n",
		want:   "top-before\n",
	},
	{
		name:   "top-level-continue.sl",
		source: "println(\"top-before\"); continue; println(\"top-after\");\n",
		want:   "top-before\n",
	},
	{
		name: "call-boundaries.sl",
		source: `sub mark { println("mark=" . $1); return $1; }
sub break_sub { println("sub-break-before"); break mark("break-operand"); println("sub-break-after"); }
sub continue_sub { println("sub-continue-before"); continue mark("continue-operand"); println("sub-continue-after"); }
$break_closure = { println("closure-break-before"); break; println("closure-break-after"); };
$continue_closure = { println("closure-continue-before"); continue; println("closure-continue-after"); };
sub builtin_inline_break {
    println("builtin-inline-break-owner-before");
    inline({ println("builtin-inline-break-before"); break; println("builtin-inline-break-after"); });
    println("builtin-inline-break-owner-after");
}
sub builtin_inline_continue {
    println("builtin-inline-continue-owner-before");
    inline({ println("builtin-inline-continue-before"); continue; println("builtin-inline-continue-after"); });
    println("builtin-inline-continue-owner-after");
}
println("break-sub=[" . break_sub() . "]");
println("continue-sub=[" . continue_sub() . "]");
println("break-closure=[" . [$break_closure] . "]");
println("continue-closure=[" . [$continue_closure] . "]");
println("builtin-break=[" . builtin_inline_break() . "]");
println("builtin-continue=[" . builtin_inline_continue() . "]");
println("boundary-tail");
`,
		want: `sub-break-before
mark=break-operand
break-sub=[]
sub-continue-before
mark=continue-operand
continue-sub=[]
closure-break-before
break-closure=[]
closure-continue-before
continue-closure=[]
builtin-inline-break-owner-before
builtin-inline-break-before
builtin-inline-break-owner-after
builtin-break=[]
builtin-inline-continue-owner-before
builtin-inline-continue-before
builtin-inline-continue-owner-after
builtin-continue=[]
boundary-tail
`,
	},
	{
		name: "named-inline-loops.sl",
		source: `inline break_inline { println("inline-break"); break; println("inline-break-after"); }
inline continue_inline { println("inline-continue"); continue; println("inline-continue-after"); }
$i = 0;
while ($i < 3) { $i++; println("while-break-before=" . $i); break_inline(); println("while-break-after"); }
println("while-break-tail=" . $i);
for ($j = 0; $j < 2; $j++) { println("for-continue-before=" . $j); continue_inline(); println("for-continue-after"); }
println("for-continue-tail=" . $j);
foreach $value (@("a", "b")) { println("foreach-continue-before=" . $value); continue_inline(); println("foreach-continue-after"); }
println("foreach-continue-tail");
$k = 0;
while ($k < 2) {
    $k++;
    try { if (true) { break_inline(); } println("try-break-after"); }
    catch $error { println("try-break-caught"); }
}
println("try-break-tail=" . $k);
`,
		want: `while-break-before=1
inline-break
while-break-tail=1
for-continue-before=0
inline-continue
for-continue-before=1
inline-continue
for-continue-tail=2
foreach-continue-before=a
inline-continue
foreach-continue-before=b
inline-continue
foreach-continue-tail
inline-break
try-break-tail=1
`,
	},
	{
		name: "dynamic-loop-control.sl",
		source: `inline dynamic_break { println("dynamic-inline-break"); break; println("dynamic-inline-break-after"); }
inline dynamic_continue { println("dynamic-inline-continue"); continue; println("dynamic-inline-continue-after"); }
println("eval-break=[" . eval('println("eval-break-before"); break; println("eval-break-after");') . "]");
println("eval-continue=[" . eval('println("eval-continue-before"); continue; println("eval-continue-after");') . "]");
println("expr-break=[" . expr("dynamic_break()") . "]");
println("expr-continue=[" . expr("dynamic_continue()") . "]");
println("include-before");
include("loop-control-child.sl");
println("include-after");
println("dynamic-tail");
`,
		include: "println(\"include-child-before\"); break; println(\"include-child-after\");\n",
		want: `eval-break-before
eval-break=[]
eval-continue-before
eval-continue=[]
dynamic-inline-break
expr-break=[]
dynamic-inline-continue
expr-continue=[]
include-before
include-child-before
include-after
dynamic-tail
`,
	},
	{
		name: "condition-continue.sl",
		source: `inline condition_continue { println("while-condition-continue"); continue; return 1; }
println("condition-before");
while (condition_continue()) { println("condition-body"); }
println("condition-after");
`,
		want: "condition-before\nwhile-condition-continue\n",
	},
	{
		name: "for-post-continue.sl",
		source: `inline post_continue { println("for-post-continue"); continue; }
for ($j = 0; $j < 2; post_continue()) { println("for-post-body=" . $j); $j++; }
println("for-post-after=" . $j);
`,
		want: "for-post-body=0\nfor-post-continue\nfor-post-continue\n",
	},
	{
		name: "lexical-loop-results.sl",
		source: `sub mark { println("mark=" . $1); return $1; }
$bare_break = { while (true) { $prior = 41; break; } };
$operand_break = { while (true) { break mark("closure-break"); } };
$bare_continue = { $i = 0; while ($i < 2) { $i++; continue; } };
$operand_continue = { $i = 0; while ($i < 2) { $i++; continue mark("closure-continue"); } };
println("closure-bare-break=[" . [$bare_break] . "]");
println("closure-operand-break=[" . [$operand_break] . "]");
println("closure-bare-continue=[" . [$bare_continue] . "]");
println("closure-operand-continue=[" . [$operand_continue] . "]");
println("eval-bare-break=[" . eval('$i = 0; while ($i < 1) { $i++; break; }') . "]");
println("eval-operand-break=[" . eval('while (true) { break mark("eval-break"); }') . "]");
println("eval-bare-continue=[" . eval('$i = 0; while ($i < 2) { $i++; continue; }') . "]");
println("eval-operand-continue=[" . eval('$i = 0; while ($i < 2) { $i++; continue mark("eval-continue"); }') . "]");
`,
		want: `closure-bare-break=[]
mark=closure-break
closure-operand-break=[]
closure-bare-continue=[]
mark=closure-continue
mark=closure-continue
closure-operand-continue=[]
eval-bare-break=[]
mark=eval-break
eval-operand-break=[]
eval-bare-continue=[]
mark=eval-continue
mark=eval-continue
eval-operand-continue=[]
`,
	},
}

func TestSleepNonLexicalLoopControlCompatibility(t *testing.T) {
	for _, probe := range sleepLoopControlProbes {
		t.Run(probe.name, func(t *testing.T) {
			got, result := runSleepLoopControlProbe(t, probe)
			if got != probe.want {
				t.Fatalf("loop-control output mismatch\nwant:\n%sgot:\n%s", probe.want, got)
			}
			if !result.IsNull() {
				t.Fatalf("loop-control result = %s, want $null", result.Describe())
			}
			if strings.Contains(got, "Warning:") {
				t.Fatalf("loop-control produced an unexpected warning: %q", got)
			}
		})
	}
}

func TestSleepNonLexicalLoopControlOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	for _, probe := range sleepLoopControlProbes {
		t.Run(probe.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, probe.name)
			if err := os.WriteFile(path, []byte(probe.source), 0o600); err != nil {
				t.Fatal(err)
			}
			if probe.include != "" {
				if err := os.WriteFile(filepath.Join(directory, "loop-control-child.sl"), []byte(probe.include), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := osexec.Command(java, "-jar", jar, path)
			command.Dir = directory
			want, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("official Sleep loop-control probe: %v\n%s", err, want)
			}
			if string(want) != probe.want {
				t.Fatalf("recorded official output mismatch\nrecorded:\n%sofficial:\n%s", probe.want, want)
			}
			got, _ := runSleepLoopControlProbe(t, probe)
			if !bytes.Equal([]byte(got), want) {
				t.Fatalf("official Sleep loop-control output mismatch\nwant:\n%sgot:\n%s", want, got)
			}
		})
	}
}

func runSleepLoopControlProbe(t *testing.T, probe sleepLoopControlProbe) (string, Value) {
	t.Helper()
	var output bytes.Buffer
	options := []Option{WithStdout(&output), WithStderr(&output)}
	if probe.include != "" {
		options = append(options, WithSourceResolver(SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
			if request.Name != "loop-control-child.sl" {
				return Source{}, os.ErrNotExist
			}
			return NewSource("loop-control-child.sl", []byte(probe.include)), nil
		})))
	}
	runtimeInstance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), probe.name, probe.source)
	if err != nil {
		t.Fatalf("Eval returned a fatal error: %v", err)
	}
	return output.String(), result
}
