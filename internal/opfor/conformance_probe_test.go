package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type sleepGoldenExecutionOracle struct {
	name              string
	wantRuntimeError  bool
	sourceWorkingDir  bool
	fixtureWorkingDir bool
	timeout           time.Duration
}

var sleepGoldenExecutionOracles = []sleepGoldenExecutionOracle{
	{name: "accum"}, {name: "addattest"}, {name: "addhash"}, {name: "addtest"}, {name: "align"},
	{name: "align2"}, {name: "arrays"}, {name: "arraysneg"}, {name: "arrmods"},
	{name: "argarr"}, {name: "args"}, {name: "arrayself"}, {name: "asis"}, {name: "assignt"},
	{name: "assert"}, {name: "assert2"},
	{name: "assertcompare"}, {name: "assertp"}, {name: "behavior"}, {name: "binary"}, {name: "binarysz"}, {name: "blockskew"},
	{name: "border"}, {name: "break"}, {name: "breakfirst"}, {name: "brokendec"}, {name: "brokeoper"},
	{name: "brokentuple"}, {name: "buffer"}, {name: "bugfix"}, {name: "byteconvert", fixtureWorkingDir: true}, {name: "byteconvert2", fixtureWorkingDir: true},
	{name: "callcc"}, {name: "callcc_foreach"}, {name: "callcc_ifonce"}, {name: "callcc_other_callers"}, {name: "callcc_prodcon"},
	{name: "callcc_return"}, {name: "callcc_tcatch"}, {name: "callcc_tcatch2"}, {name: "callcc_trycatch"},
	{name: "callccbg"}, {name: "callccfreeze"}, {name: "callccpcon"}, {name: "callccr"},
	{name: "clazz"}, {name: "clmistake"}, {name: "closure"}, {name: "closure2"}, {name: "closureindex"},
	{name: "cache"}, {name: "castbug"}, {name: "charcheck"}, {name: "checksum", fixtureWorkingDir: true, timeout: 10 * time.Second}, {name: "closurekvp"}, {name: "cmdline"}, {name: "compile_cl"},
	{name: "concat"}, {name: "concat2"}, {name: "connpipe"}, {name: "continue"}, {name: "convertds"},
	{name: "convertds2", fixtureWorkingDir: true}, {name: "convertds4", fixtureWorkingDir: true}, {name: "copy"},
	{name: "confused"}, {name: "corrupt"}, {name: "corrupt1"}, {name: "dataio", timeout: 10 * time.Second}, {name: "debug64"}, {name: "debugproxy"}, {name: "digest", fixtureWorkingDir: true, timeout: 10 * time.Second}, {name: "doubleship"}, {name: "dsliteral"}, {name: "dtest1"}, {name: "elfbug"}, {name: "excheck"}, {name: "exotic_i"},
	{name: "cor_foreach"}, {name: "cor_ifonce"}, {name: "cor_other_callers"}, {name: "cor_prodcon"}, {name: "cor_return"},
	{name: "expassign"}, {name: "fact"}, {name: "fe_generator"}, {name: "fe_generatordb"}, {name: "fe_ohash"}, {name: "feloc"}, {name: "feremovetest"}, {name: "fescope"},
	{name: "femod"}, {name: "fetest"}, {name: "feprob"}, {name: "find"}, {name: "fob"}, {name: "for"}, {name: "forany"},
	{name: "foreach"}, {name: "foreachrem"}, {name: "foreachrecurse"}, {name: "fork", timeout: 10 * time.Second}, {name: "fork2"}, {name: "forkdl", fixtureWorkingDir: true, timeout: 10 * time.Second}, {name: "forkof"}, {name: "forkshare"},
	{name: "forksubs"}, {name: "forplay"}, {name: "fpfuncs"}, {name: "ftest"}, {name: "funchandle"}, {name: "functiondesc"},
	{name: "functionerr"}, {name: "genfun"}, {name: "graph"},
	{name: "hash"}, {name: "hash2"}, {name: "hash3"}, {name: "hash4", wantRuntimeError: true},
	{name: "hashambig"}, {name: "hashself"}, {name: "hexbites"}, {name: "hfreeze"}, {name: "hoeswarning"},
	{name: "identity"}, {name: "if"},
	{name: "ifand"}, {name: "ifand2"}, {name: "ifarray"},
	{name: "ifbang"}, {name: "ifby4"}, {name: "iff"}, {name: "ifferr"}, {name: "ifnoparens"},
	{name: "impfrom", fixtureWorkingDir: true}, {name: "impfrom2", fixtureWorkingDir: true}, {name: "impfrom4", fixtureWorkingDir: true},
	{name: "inchack"}, {name: "incit"}, {name: "include", fixtureWorkingDir: true}, {name: "include2", sourceWorkingDir: true}, {name: "index"}, {name: "indexand"}, {name: "indexerr"}, {name: "indexit"}, {name: "inline"}, {name: "inlineb"}, {name: "inlined"},
	{name: "inlined2"}, {name: "inlined3"}, {name: "inlineinv"}, {name: "inlinelocalcallcc"}, {name: "innertroubles"},
	{name: "identity2"}, {name: "isa"}, {name: "iswm"}, {name: "iswm2"},
	{name: "iswm3"},
	{name: "invoke"}, {name: "invoke2"}, {name: "ioerr"}, {name: "itererror"}, {name: "iternotrace"}, {name: "jiter1"}, {name: "jiter2"}, {name: "jiter3"},
	{name: "joiniter"}, {name: "keywrds"}, {name: "lambdacs"}, {name: "lindexOf"},
	{name: "listops"}, {name: "listops2"}, {name: "listops_empty"}, {name: "listops_empty2"},
	{name: "lineno"}, {name: "listops_get"}, {name: "listops_simple"}, {name: "localstack"}, {name: "longip"},
	{name: "makebadscript"}, {name: "matcher", timeout: 10 * time.Second}, {name: "megaio", timeout: 10 * time.Second}, {name: "memoize"}, {name: "mlistfun"}, {name: "multi"}, {name: "multih", fixtureWorkingDir: true}, {name: "multilit"}, {name: "native_arrays"}, {name: "newInstance"}, {name: "newforeach"}, {name: "newnumbers"}, {name: "nmesgs"}, {name: "nosharel"}, {name: "nparams"}, {name: "numbers"},
	{name: "objects"}, {name: "odd"}, {name: "ohash"}, {name: "ohashsem"}, {name: "oopack"}, {name: "oper_space"}, {name: "operorder"}, {name: "or"}, {name: "parms"}, {name: "passarrays"}, {name: "pep255"},
	{name: "pipeit"}, {name: "pliterals"}, {name: "poptest"}, {name: "precedence"}, {name: "preced"}, {name: "probug"},
	{name: "print"}, {name: "process", sourceWorkingDir: true}, {name: "profiler"}, {name: "proxy"}, {name: "push"}, {name: "pushl"}, {name: "pushl2"}, {name: "pushl3"},
	{name: "putAll"}, {name: "putAll2"}, {name: "putAll3"},
	{name: "pureinterop"}, {name: "readb2", sourceWorkingDir: true}, {name: "readb3"}, {name: "readobj"}, {name: "regex"}, {name: "regexc"}, {name: "remove"}, {name: "removeerr"}, {name: "removetest"}, {name: "returnloop"}, {name: "returnstack"},
	{name: "round"}, {name: "scope"},
	{name: "scalref"}, {name: "scope2"}, {name: "setf"}, {name: "setField2"}, {name: "setfield", fixtureWorkingDir: true}, {name: "setfield3", fixtureWorkingDir: true}, {name: "setops"}, {name: "shortcircuit"}, {name: "shortcircuit2"},
	{name: "ser_closure"}, {name: "serohash"}, {name: "sertest"}, {name: "sizehash"}, {name: "skew"}, {name: "skew2"}, {name: "sleepoo"}, {name: "sleepoo2"}, {name: "slist2"}, {name: "slist3"}, {name: "sort"}, {name: "sort2"}, {name: "splicetest"},
	{name: "split2"}, {name: "splsublist"}, {name: "squote"}, {name: "srand"}, {name: "strmods"},
	{name: "stringf"}, {name: "strfun"}, {name: "sublist"}, {name: "sumtest"}, {name: "sync"}, {name: "taint11"}, {name: "tcatchex"}, {name: "trans"}, {name: "tracebr"}, {name: "tracepo"}, {name: "traverse"},
	{name: "tcatch"}, {name: "tcatch2"}, {name: "tcatch3"}, {name: "tcatch4"}, {name: "tcatch5"}, {name: "tcatch6"}, {name: "testfield", fixtureWorkingDir: true}, {name: "traverse2"}, {name: "trybreak"}, {name: "trybreaks"}, {name: "trybreaks2"}, {name: "trylocal"}, {name: "typeof"}, {name: "types"},
	{name: "unicodeseq"}, {name: "unlambdacs"}, {name: "use", fixtureWorkingDir: true}, {name: "use2", fixtureWorkingDir: true}, {name: "useerr"},
	{name: "values"}, {name: "values2"},
	{name: "warn"}, {name: "watch"}, {name: "while2", sourceWorkingDir: true}, {name: "wo"}, {name: "writeobj"},
	{name: "xor"}, {name: "yield"},
}

func TestSleepGoldenConformance(t *testing.T) {
	tests := sleepGoldenExecutionOracles
	if got, want := len(tests), 302; got != want {
		t.Fatalf("exact-output fixture count = %d, want %d", got, want)
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if skipCompatibilityFixtureUnderRace(test.name) {
				t.Skip("canonical CPU/I/O stress fixture is covered outside race instrumentation")
			}
			programRoot := filepath.Join("testdata", "upstream", "sleep-2.1", "programs")
			programBytes, err := os.ReadFile(filepath.Join(programRoot, test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "golden", test.name+".sl"))
			if err != nil {
				t.Fatal(err)
			}
			program, err := Compile(NewSource(test.name+".sl", programBytes))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			var output bytes.Buffer
			runtime, err := New(WithStdout(&output), WithStderr(&output))
			if err != nil {
				t.Fatal(err)
			}
			if test.sourceWorkingDir {
				absoluteRoot, err := filepath.Abs(programRoot)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.Invoke(context.Background(), "chdir", String(absoluteRoot)); err != nil {
					t.Fatalf("set runtime cwd: %v", err)
				}
			}
			if test.fixtureWorkingDir {
				fixtureRoot, err := filepath.Abs(filepath.Join("testdata", "upstream", "sleep-2.1", "fixtures"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.Invoke(context.Background(), "chdir", String(fixtureRoot)); err != nil {
					t.Fatalf("set fixture cwd: %v", err)
				}
			}
			timeout := test.timeout
			if timeout == 0 {
				timeout = 2 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			_, err = runtime.Execute(ctx, program)
			cancel()
			if err != nil && !test.wantRuntimeError {
				t.Fatalf("execute: %v\noutput:\n%s", err, output.String())
			}
			if err == nil && test.wantRuntimeError {
				t.Fatal("execute succeeded, want the canonical flow-interrupting error")
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, output.Bytes())
			}
		})
	}
}

func TestSleepBarewordIsACompileError(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "bareword.sl"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(NewSource("bareword.sl", data))
	var compileError *CompileError
	if !errors.As(err, &compileError) || len(compileError.Diagnostics) != 1 {
		t.Fatalf("Compile error = %v, want one diagnostic", err)
	}
	diagnostic := compileError.Diagnostics[0]
	if diagnostic.Code != "CMP002" || diagnostic.Message != "Unknown expression" || diagnostic.Span.Start.Line != 5 {
		t.Fatalf("bareword diagnostic = %+v", diagnostic)
	}
}
