package opfor

import (
	"bytes"
	"context"
	"testing"
)

// This probe covers java.util.regex.Pattern's default and UNIX_LINES line
// terminators, embedded mode scoping, predefined ASCII/Unicode classes,
// boundaries, POSIX properties, scripts/blocks/categories, and Java octal
// escapes through Sleep's actual RegexBridge entry points.
const sleepJavaRegexModesProbeSource = `
sub u { return unpack("U", pack("H*", $1 . "0000"))[0]; }
sub fm { if ($2 hasmatch $3) { println($1 . ":1"); } else { println($1 . ":0"); } }
sub wm { if ($2 ismatch $3) { println($1 . ":1"); } else { println($1 . ":0"); } }
sub fi {
    $value = find($2, $3, $4);
    if ($value is $null) { println($1 . ":null"); }
    else { println($1 . ":" . $value); }
}

$nel = u("0085");
$ls = u("2028");
$ps = u("2029");
$nbsp = u("00a0");
$emsp = u("2003");
$arabic = u("0661");
$eacute = u("00e9");
$alpha = u("03b1");
$combining = u("0301");
$join = u("200d");
$letter_number = u("2160");
$vt = u("000b");

fm("dollar-nl", "a\n", 'a$');
fm("dollar-cr", "a\r", 'a$');
fm("dollar-crlf", "a\r\n", 'a$');
fm("dollar-nel", "a" . $nel, 'a$');
fm("dollar-ls", "a" . $ls, 'a$');
fm("dollar-ps", "a" . $ps, 'a$');
wm("dot-cr", "\r", '.');
wm("dot-nel", $nel, '.');
wm("dot-ls", $ls, '.');
wm("dot-ps", $ps, '.');
fm("multiline-cr", "x\ra\ry", '(?m)^a$');
fm("multiline-crlf", "x\r\na\r\ny", '(?m)^a$');
fm("multiline-nel", "x" . $nel . "a" . $nel . "y", '(?m)^a$');
fm("multiline-ls", "x" . $ls . "a" . $ls . "y", '(?m)^a$');
fm("multiline-ps", "x" . $ps . "a" . $ps . "y", '(?m)^a$');
fm("unix-cr", "x\ra\ry", '(?dm)^a$');
fm("unix-nl", "x\na\ny", '(?dm)^a$');
wm("dotall-scope", $ls, '(?s:.)');
wm("dotall-restore", $ls . $ls, '(?s:.).');
wm("unix-scope", "\r", '(?d:.)');
wm("unix-restore", "\r\r", '(?d:.).');
fm("multiline-scope", "x\ra\ry", '(?m:^a$)');
fm("multiline-restore", "x\rb\ry", '(?m:^a$)|^b$');
wm("comments-class-space", " ", '(?x)[ a ]');
wm("comments-class-a", "a", '(?x)[ a ]');
wm("comments-class-hash", "#", '(?x)[a# comment' . "\n" . 'b]');
wm("comments-class-b", "b", '(?x)[a# comment' . "\n" . 'b]');
fm("comments-cr", "ab", '(?x)a# comment' . "\r" . 'b');
fm("comments-lf", "ab", '(?x)a# comment' . "\n" . 'b');
fm("comments-nel", "ab", '(?x)a# comment' . $nel . 'b');
fm("comments-ls", "ab", '(?x)a# comment' . $ls . 'b');
fm("comments-ps", "ab", '(?x)a# comment' . $ps . 'b');

wm("space-vt", $vt, '\s');
wm("space-vt-class", $vt, '[\s]');
wm("horizontal-nbsp", $nbsp, '\h');
wm("horizontal-emspace", $emsp, '\h');
wm("vertical-ls", $ls, '\v');
wm("linebreak-crlf", "\r\n", '\R');
wm("linebreak-ls", $ls, '\R');
wm("digit-default", $arabic, '\d');
wm("word-default", $eacute, '\w');
wm("letter-number-word-default", $letter_number, '\w');
wm("space-default", $emsp, '\s');
wm("digit-unicode", $arabic, '(?U)\d');
wm("word-unicode", $eacute, '(?U)\w');
wm("mark-word-unicode", $combining, '(?U)\w');
wm("join-word-unicode", $join, '(?U)\w');
wm("letter-number-word-unicode", $letter_number, '(?U)\w');
wm("letter-number-word-case-unicode", $letter_number, '(?iU)\w');
wm("space-unicode", $emsp, '(?U)\s');
wm("unicode-intersection", $arabic, '(?U)[\d&&[^0-9]]');
fm("boundary-default", $eacute, '\b' . $eacute . '\b');
fm("boundary-unicode", $eacute, '(?U)\b' . $eacute . '\b');
fm("letter-number-boundary-unicode", $letter_number, '(?U)\b' . $letter_number . '\b');

wm("script-is", $eacute, '\p{IsLatin}');
wm("script-key", $alpha, '\p{sc=Greek}');
wm("script-long-key", $alpha, '\p{script=Greek}');
wm("block", $eacute, '\p{InLatin-1Supplement}');
wm("category", $arabic, '\p{gc=Nd}');
wm("binary", $eacute, '\p{IsAlphabetic}');
wm("letter-number-binary-alphabetic", $letter_number, '\p{IsAlphabetic}');
wm("java-lower", $eacute, '\p{javaLowerCase}');
wm("posix-default", $eacute, '\p{Lower}');
wm("letter-number-posix-alpha-default", $letter_number, '\p{Alpha}');
wm("posix-unicode", $eacute, '(?U)\p{Lower}');
wm("letter-number-posix-alpha", $letter_number, '(?U)\p{Alpha}');
wm("letter-number-posix-alpha-case", $letter_number, '(?iU)\p{Alpha}');
wm("letter-number-posix-alnum", $letter_number, '(?U)\p{Alnum}');
wm("octal", "A", '\0101');

fi("dollar-crlf-start0", "\r\n", '$', 0);
fi("dollar-crlf-start1", "\r\n", '$', 1);
fi("endz-crlf-start0", "\r\n", '\Z', 0);
fi("multiline-start-final", "\n", '(?m)^', 1);
fi("multiline-dollar-crlf", "\r\n", '(?m)$', 1);
fi("unix-dollar-crlf", "\r\n", '(?dm)$', 1);
`

const sleepJavaRegexModesProbeOutput = "dollar-nl:1\n" +
	"dollar-cr:1\n" +
	"dollar-crlf:1\n" +
	"dollar-nel:1\n" +
	"dollar-ls:1\n" +
	"dollar-ps:1\n" +
	"dot-cr:0\n" +
	"dot-nel:0\n" +
	"dot-ls:0\n" +
	"dot-ps:0\n" +
	"multiline-cr:1\n" +
	"multiline-crlf:1\n" +
	"multiline-nel:1\n" +
	"multiline-ls:1\n" +
	"multiline-ps:1\n" +
	"unix-cr:0\n" +
	"unix-nl:1\n" +
	"dotall-scope:1\n" +
	"dotall-restore:0\n" +
	"unix-scope:1\n" +
	"unix-restore:0\n" +
	"multiline-scope:1\n" +
	"multiline-restore:0\n" +
	"comments-class-space:0\n" +
	"comments-class-a:1\n" +
	"comments-class-hash:0\n" +
	"comments-class-b:1\n" +
	"comments-cr:1\n" +
	"comments-lf:1\n" +
	"comments-nel:0\n" +
	"comments-ls:0\n" +
	"comments-ps:0\n" +
	"space-vt:1\n" +
	"space-vt-class:1\n" +
	"horizontal-nbsp:1\n" +
	"horizontal-emspace:1\n" +
	"vertical-ls:1\n" +
	"linebreak-crlf:1\n" +
	"linebreak-ls:1\n" +
	"digit-default:0\n" +
	"word-default:0\n" +
	"letter-number-word-default:0\n" +
	"space-default:0\n" +
	"digit-unicode:1\n" +
	"word-unicode:1\n" +
	"mark-word-unicode:1\n" +
	"join-word-unicode:1\n" +
	"letter-number-word-unicode:1\n" +
	"letter-number-word-case-unicode:1\n" +
	"space-unicode:1\n" +
	"unicode-intersection:1\n" +
	"boundary-default:0\n" +
	"boundary-unicode:1\n" +
	"letter-number-boundary-unicode:1\n" +
	"script-is:1\n" +
	"script-key:1\n" +
	"script-long-key:1\n" +
	"block:1\n" +
	"category:1\n" +
	"binary:1\n" +
	"letter-number-binary-alphabetic:1\n" +
	"java-lower:1\n" +
	"posix-default:0\n" +
	"letter-number-posix-alpha-default:0\n" +
	"posix-unicode:1\n" +
	"letter-number-posix-alpha:1\n" +
	"letter-number-posix-alpha-case:1\n" +
	"letter-number-posix-alnum:1\n" +
	"octal:1\n" +
	"dollar-crlf-start0:0\n" +
	"dollar-crlf-start1:2\n" +
	"endz-crlf-start0:0\n" +
	"multiline-start-final:null\n" +
	"multiline-dollar-crlf:2\n" +
	"unix-dollar-crlf:1\n"

func TestSleepJavaRegexModesExactOutput(t *testing.T) {
	got := runPureGoJavaRegexProbe(t, sleepJavaRegexModesProbeSource)
	if !bytes.Equal(got, []byte(sleepJavaRegexModesProbeOutput)) {
		t.Fatalf("Java regex probe mismatch\nwant:\n%sgot:\n%s", sleepJavaRegexModesProbeOutput, got)
	}
}

func TestSleepJavaRegexRejectsGraphemeInCharacterClass(t *testing.T) {
	t.Parallel()
	if _, err := compileSleepRegex(`[\X]`, false); err == nil {
		t.Error(`compileSleepRegex("[\X]") unexpectedly succeeded`)
	}
}

// TestSleepJavaRegexModesOfficialJARDifferential is opt-in because the BSD
// Sleep JAR is supplied separately. Its hash pins the RegexBridge oracle while
// ordinary CI remains pure Go and network-independent.
func TestSleepJavaRegexModesOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	goOutput := runPureGoJavaRegexProbe(t, sleepJavaRegexModesProbeSource)
	command := officialSleepJavaCommand(java, "-jar", jar, "-e", sleepJavaRegexModesProbeSource)
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep regex probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(goOutput, javaOutput) {
		t.Fatalf("official Sleep regex output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
	}
}

func runPureGoJavaRegexProbe(t *testing.T, source string) []byte {
	t.Helper()
	program, err := CompileString("java-regex-modes.sl", source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return append([]byte(nil), output.Bytes()...)
}
