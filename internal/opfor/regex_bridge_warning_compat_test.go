package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const regexBridgeWarningProbeName = "regex-bridge-warning.sl"

// Sleep commit 60ac3ff9dacc3e7b5a6c58be201c5830afbda398 leaves
// Pattern.compile and Matcher.appendReplacement exceptions to Block. Block
// converts IllegalArgumentException directly and IndexOutOfBoundsException
// with its invalid-index prefix, then recovers at the active block boundary.
const regexBridgeWarningProbe = `sub split_bad {
    println("split-before");
    split("[", "abc");
    println("split-tail");
}
split_bad();
println("split-resume");
sub find_bad {
    try {
        println("find-before");
        find("abc", "[");
        println("find-try-tail");
    }
    catch $error {
        println("find-caught=" . $error);
    }
    println("find-sub-resume");
}
find_bad();
println("find-caller-resume");
sub matches_bad {
    println("matches-before");
    matches("abc", "[");
    println("matches-tail");
}
matches_bad();
println("matches-resume");
sub replace_bad {
    println("replace-before");
    replace("a", "(a)", chr(36));
    println("replace-tail");
}
replace_bad();
println("replace-resume");
setf("&zsplit", function("&split"));
sub split_alias_bad {
    println("split-alias-before");
    zsplit("[", "abc");
    println("split-alias-tail");
}
split_alias_bad();
println("split-alias-resume");
setf("&zfind", function("&find"));
sub find_alias_bad {
    println("find-alias-before");
    zfind("abc", "[");
    println("find-alias-tail");
}
find_alias_bad();
println("find-alias-resume");
setf("&zmatches", function("&matches"));
sub matches_alias_bad {
    println("matches-alias-before");
    zmatches("abc", "[");
    println("matches-alias-tail");
}
matches_alias_bad();
println("matches-alias-resume");
setf("&zreplace", function("&replace"));
sub replace_alias_bad {
    println("replace-alias-before");
    zreplace("a", "(a)", chr(36));
    println("replace-alias-tail");
}
replace_alias_bad();
println("replace-alias-resume");
sub replace_index_bad {
    println("replace-index-before");
    replace("a", "(a)", chr(36) . "9");
    println("replace-index-tail");
}
replace_index_bad();
println("replace-index-resume");
sub ismatch_bad {
    println("ismatch-before");
    if ("abc" ismatch "[") {
        println("ismatch-then");
    }
    println("ismatch-tail");
}
ismatch_bad();
println("ismatch-resume");
sub hasmatch_bad {
    println("hasmatch-before");
    if ("abc" hasmatch "[") {
        println("hasmatch-then");
    }
    println("hasmatch-tail");
}
hasmatch_bad();
println("hasmatch-resume");
`

const regexBridgeWarningOutput = `split-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:3
split-resume
find-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:11
find-sub-resume
find-caller-resume
matches-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:23
matches-resume
replace-before
Warning: Illegal group reference: group index is missing at regex-bridge-warning.sl:30
replace-resume
split-alias-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:38
split-alias-resume
find-alias-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:46
find-alias-resume
matches-alias-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:54
matches-alias-resume
replace-alias-before
Warning: Illegal group reference: group index is missing at regex-bridge-warning.sl:62
replace-alias-resume
replace-index-before
Warning: attempted an invalid index: No group 9 at regex-bridge-warning.sl:69
replace-index-resume
ismatch-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:76
ismatch-resume
hasmatch-before
Warning: Unclosed character class near index 0
[
^ at regex-bridge-warning.sl:85
hasmatch-resume
`

const regexBridgeUTF16EmptyProbeName = "regex-bridge-utf16-empty.sl"

const regexBridgeUTF16EmptyProbe = `sub units {
    local('$text $result $index');
    $text = $1;
    $result = "" . strlen($text);
    for ($index = 0; $index < strlen($text); $index++) {
        $result .= ":" . asc(charAt($text, $index));
    }
    return $result;
}
$pair = "😀";
@parts = split("", $pair, -1);
println("split=" . size(@parts) . ":" . units(@parts[0]) . ":" . units(@parts[1]) . ":" . units(@parts[2]));
println("all=" . units(replace($pair, "", "-")));
println("zero=" . units(replace($pair, "", "-", 0)));
println("one=" . units(replace($pair, "", "-", 1)));
println("two=" . units(replace($pair, "", "-", 2)));
println("three=" . units(replace($pair, "", "-", 3)));
println("large=" . units(replace($pair, "", "-", 99)));
println("negative=" . units(replace($pair, "", "-", -2)));
`

const regexBridgeUTF16EmptyOutput = `split=3:1:55357:1:56832:0
all=5:45:55357:45:56832:45
zero=2:55357:56832
one=3:45:55357:56832
two=4:45:55357:45:56832
three=5:45:55357:45:56832:45
large=5:45:55357:45:56832:45
negative=5:45:55357:45:56832:45
`

func TestRegexBridgeWarningCompatibility(t *testing.T) {
	if got := runRegexBridgeWarningProbe(t); !bytes.Equal(got, []byte(regexBridgeWarningOutput)) {
		t.Fatalf("RegexBridge warning output mismatch\nwant:\n%sgot:\n%s", regexBridgeWarningOutput, got)
	}
}

func TestRegexBridgeWarningOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, regexBridgeWarningProbeName)
	if err := os.WriteFile(path, []byte(regexBridgeWarningProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep RegexBridge warning probe: %v\n%s", err, want)
	}
	if got := runRegexBridgeWarningProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep RegexBridge warning output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestRegexBridgeEmptyPatternUTF16Compatibility(t *testing.T) {
	if got := runRegexBridgeProbe(t, regexBridgeUTF16EmptyProbeName, regexBridgeUTF16EmptyProbe); !bytes.Equal(got, []byte(regexBridgeUTF16EmptyOutput)) {
		t.Fatalf("RegexBridge empty-pattern UTF-16 output mismatch\nwant:\n%sgot:\n%s", regexBridgeUTF16EmptyOutput, got)
	}
}

func TestRegexBridgeEmptyPatternUTF16OfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, regexBridgeUTF16EmptyProbeName)
	if err := os.WriteFile(path, []byte(regexBridgeUTF16EmptyProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep RegexBridge empty-pattern UTF-16 probe: %v\n%s", err, want)
	}
	if got := runRegexBridgeProbe(t, regexBridgeUTF16EmptyProbeName, regexBridgeUTF16EmptyProbe); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep RegexBridge empty-pattern UTF-16 output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestRegexBridgeResourceFailuresRemainFatal(t *testing.T) {
	_, err := compileSleepRegexBridge(strings.Repeat("a", javaRegexTranslatedPatternLimit+1), false)
	if err == nil {
		t.Fatal("oversize RegexBridge pattern unexpectedly compiled")
	}
	var warning *uncaughtScriptWarning
	if errors.As(err, &warning) {
		t.Fatalf("oversize RegexBridge pattern became a script warning: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = builtinSleepReplace(ctx, Invocation{
		Name: "replace",
		Arguments: []Argument{
			{Value: String("aaaa")},
			{Value: String("a")},
			{Value: String("x")},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled RegexBridge replacement error = %v, want context.Canceled", err)
	}
	if errors.As(err, &warning) {
		t.Fatalf("canceled RegexBridge replacement became a script warning: %v", err)
	}
}

func TestRegexBridgeAllMatchPathsObserveCancellation(t *testing.T) {
	const catastrophicText = `(("a" x 20000) . "x")`
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "matches-direct", source: `matches(` + catastrophicText + `, "(a+)+$");`},
		{name: "matches-alias", source: `setf("&zmatches", function("&matches")); zmatches(` + catastrophicText + `, "(a+)+$");`},
		{name: "find-direct", source: `find(` + catastrophicText + `, "(a+)+$");`},
		{name: "find-alias", source: `setf("&zfind", function("&find")); zfind(` + catastrophicText + `, "(a+)+$");`},
		{name: "ismatch-predicate", source: `if (` + catastrophicText + ` ismatch "(a+)+$") { println("unreachable"); }`},
		{name: "hasmatch-predicate", source: `if (` + catastrophicText + ` hasmatch "(a+)+$") { println("unreachable"); }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			ctx, cancel := context.WithCancel(context.Background())
			timer := time.AfterFunc(100*time.Millisecond, cancel)
			_, err = runtimeInstance.Eval(ctx, test.name+".sl", test.source)
			timer.Stop()
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("RegexBridge cancellation error = %v, want context.Canceled", err)
			}
			var warning *uncaughtScriptWarning
			if errors.As(err, &warning) {
				t.Fatalf("RegexBridge cancellation became a script warning: %v", err)
			}
		})
	}
}

func TestRegexBridgeAllMatchesConsumeInstructionBudget(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "matches-direct", source: `matches("a" x 10000, "(a)");`},
		{name: "matches-alias", source: `setf("&zmatches", function("&matches")); zmatches("a" x 10000, "(a)");`},
		{name: "hasmatch-predicate", source: `if (("a" x 10000) hasmatch "(a)") { }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, err := New(WithInstructionLimit(100))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			_, err = runtimeInstance.Eval(context.Background(), test.name+".sl", test.source)
			if !errors.Is(err, ErrInstructionLimit) {
				t.Fatalf("RegexBridge instruction error = %v, want ErrInstructionLimit", err)
			}
			var warning *uncaughtScriptWarning
			if errors.As(err, &warning) {
				t.Fatalf("RegexBridge instruction limit became a script warning: %v", err)
			}
		})
	}
}

func TestRegexBridgeTranslationGuardFailuresRemainFatal(t *testing.T) {
	markers := newJavaRegexPropertyMarkers("")
	atom, err := markers.add(`[A]`)
	if err != nil {
		t.Fatal(err)
	}
	_, lostErr := markers.expand("")
	_, duplicateErr := markers.expand(atom + atom)
	exhausted := &javaRegexPropertyMarkers{
		reserved:   make(map[rune]struct{}),
		rangeIndex: len(javaRegexPrivateUseRanges),
	}
	_, exhaustionErr := exhausted.add(`[A]`)
	limitErr := javaRegexTranslatedPatternLimitError()

	for name, err := range map[string]error{
		"limit":      limitErr,
		"exhaustion": exhaustionErr,
		"duplicate":  duplicateErr,
		"lost":       lostErr,
	} {
		t.Run(name, func(t *testing.T) {
			if err == nil {
				t.Fatal("translator guard unexpectedly succeeded")
			}
			if !sleepRegexCompileFailureIsFatal(err) {
				t.Fatalf("translator guard was not classified fatal: %v", err)
			}
			var guard *javaRegexTranslationGuardError
			if !errors.As(err, &guard) {
				t.Fatalf("translator guard error type = %T, want *javaRegexTranslationGuardError", err)
			}
		})
	}
}

func runRegexBridgeWarningProbe(t *testing.T) []byte {
	t.Helper()
	return runRegexBridgeProbe(t, regexBridgeWarningProbeName, regexBridgeWarningProbe)
}

func runRegexBridgeProbe(t *testing.T, name, source string) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), name, source); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return append([]byte(nil), output.Bytes()...)
}
