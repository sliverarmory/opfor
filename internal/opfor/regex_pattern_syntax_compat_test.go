package opfor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const regexPatternSyntaxProbeName = "regex-pattern-syntax.sl"

var regexPatternSyntaxCases = []struct {
	pattern string
	want    string
}{
	{"(", "Unclosed group near index 1\n("},
	{")", "Unmatched closing ')'\n)"},
	{"a)", "Unmatched closing ')' near index 0\na)\n^"},
	{"(a))", "Unmatched closing ')' near index 2\n(a))\n  ^"},
	{"*", "Dangling meta character '*' near index 0\n*\n^"},
	{"a**", "Dangling meta character '*' near index 2\na**\n  ^"},
	{"{", "Illegal repetition near index 1\n{"},
	{"a{,}", "Illegal repetition near index 2\na{,}\n  ^"},
	{"a{2", "Unclosed counted closure near index 3\na{2"},
	{"a{2,1}", "Illegal repetition range near index 5\na{2,1}\n     ^"},
	{"a{2147483648}", "Illegal repetition range near index 11\na{2147483648}\n           ^"},
	{"\\", "Unescaped trailing backslash near index 1\n\\"},
	{"\\x", "Illegal hexadecimal escape sequence near index 2\n\\x"},
	{"\\x{110000}", "Hexadecimal codepoint is too big near index 8\n\\x{110000}\n        ^"},
	{"\\u12", "Illegal Unicode escape sequence near index 4\n\\u12"},
	{"\\c", "Illegal control escape sequence near index 1\n\\c\n ^"},
	{"[z-a]", "Illegal character range near index 3\n[z-a]\n   ^"},
	{"(?q)", "Unknown inline modifier near index 2\n(?q)\n  ^"},
	{"(?a)", "Unknown inline modifier near index 2\n(?a)\n  ^"},
	{"(?(a)b)", "Unknown inline modifier near index 2\n(?(a)b)\n  ^"},
	{"(?<1>a)", "capturing group name does not start with a Latin letter near index 3\n(?<1>a)\n   ^"},
	{"(?<x>a)(?<x>b)", "Named capturing group <x> is already defined near index 11\n(?<x>a)(?<x>b)\n           ^"},
	{"\\k<x>", "named capturing group <x> does not exist near index 4\n\\k<x>\n    ^"},
	{"\\k", "\\k is not followed by '<' for named capturing group near index 2\n\\k"},
	{"\\p", "Unknown character property name {\x00} near index 2\n\\p"},
	{"\\p{Bogus}", "Unknown character property name {Bogus} near index 8\n\\p{Bogus}\n        ^"},
	{"\\N", "Illegal character name escape sequence near index 2\n\\N"},
	{"\\N{BOGUS}", "Unknown character name [BOGUS] near index 8\n\\N{BOGUS}\n        ^"},
	{"[&&]", "Bad class syntax near index 2\n[&&]\n  ^"},
	{"[&&&&a]", "Bad class syntax near index 2\n[&&&&a]\n  ^"},
	{"😀)", "Unmatched closing ')' near index 0\n😀)\n^"},
	{"😀(", "Unclosed group near index 2\n😀(\n  ^"},
	{"[😀", "Unclosed character class near index 1\n[😀\n ^"},
	{"😀\\", "Unescaped trailing backslash near index 2\n😀\\\n  ^"},
	{"😀(?q)", "Unknown inline modifier near index 3\n😀(?q)\n   ^"},
}

func TestRegexPatternSyntaxDiagnostics(t *testing.T) {
	for _, test := range regexPatternSyntaxCases {
		test := test
		t.Run(fmt.Sprintf("%q", test.pattern), func(t *testing.T) {
			_, err := compileSleepRegex(test.pattern, false)
			if err == nil {
				t.Fatal("compileSleepRegex unexpectedly accepted invalid Java pattern")
			}
			if got := sleepJavaPatternSyntaxMessage(test.pattern, err); got != test.want {
				t.Fatalf("PatternSyntaxException message mismatch\nwant:\n%s\ngot:\n%s", test.want, got)
			}
		})
	}
}

func TestRegexPatternSyntaxOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	source := regexPatternSyntaxProbe()
	directory := t.TempDir()
	path := filepath.Join(directory, regexPatternSyntaxProbeName)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep PatternSyntaxException probe: %v\n%s", err, want)
	}
	if got := runRegexBridgeProbe(t, regexPatternSyntaxProbeName, source); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep PatternSyntaxException output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func regexPatternSyntaxProbe() string {
	var source strings.Builder
	for index, test := range regexPatternSyntaxCases {
		fmt.Fprintf(&source, "sub regex_syntax_%d {\n", index)
		fmt.Fprintf(&source, "    println(\"before-%d\");\n", index)
		fmt.Fprintf(&source, "    split(%s, \"abc\");\n", sleepRegexProbeLiteral(test.pattern))
		fmt.Fprintf(&source, "    println(\"tail-%d\");\n", index)
		source.WriteString("}\n")
		fmt.Fprintf(&source, "regex_syntax_%d();\n", index)
		fmt.Fprintf(&source, "println(\"after-%d\");\n", index)
	}
	return source.String()
}

func sleepRegexProbeLiteral(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\r", "\\r",
		"\n", "\\n",
		"\t", "\\t",
	)
	return "\"" + replacer.Replace(value) + "\""
}

func TestRegexPatternSyntaxAcceptedJavaForms(t *testing.T) {
	const source = `println(iff("q" ismatch "\\c1", "control", "bad"));
println(iff("" ismatch "(?)", "empty-flags", "bad"));
println(iff("a" ismatch "[a&&]", "right-empty", "bad"));
println(iff("a" ismatch "[&&a]", "left-empty", "bad"));
println(iff("a" ismatch "[a&&&&]", "repeated-right-empty", "bad"));
println(iff("" ismatch "{2}", "leading-count", "bad"));
`
	const want = "control\nempty-flags\nright-empty\nleft-empty\nrepeated-right-empty\nleading-count\n"

	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "regex-pattern-accepted.sl", source); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != want {
		t.Fatalf("accepted Java regex forms output = %q, want %q", got, want)
	}
}

func TestRegexPatternSyntaxAcceptedJavaFormsOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	const source = `println(iff("q" ismatch "\\c1", "control", "bad"));
println(iff("" ismatch "(?)", "empty-flags", "bad"));
println(iff("a" ismatch "[a&&]", "right-empty", "bad"));
println(iff("a" ismatch "[&&a]", "left-empty", "bad"));
println(iff("a" ismatch "[a&&&&]", "repeated-right-empty", "bad"));
println(iff("" ismatch "{2}", "leading-count", "bad"));
`
	directory := t.TempDir()
	path := filepath.Join(directory, "regex-pattern-accepted.sl")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep accepted-pattern probe: %v\n%s", err, want)
	}
	if got := runRegexBridgeProbe(t, "regex-pattern-accepted.sl", source); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep accepted-pattern output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}
