package opfor

import (
	"bytes"
	"reflect"
	"testing"
)

const utf16RegexProbeSource = `
$s = chr(0xD800);
if ($s ismatch '^.$') { println("ismatch:1"); } else { println("ismatch:0"); }

$literal = $s;
if ($s ismatch $literal) {
	println("literal:1");
} else { println("literal:0"); }

$class = '[' . $s . ']';
if ($s ismatch $class) { println("class:1"); } else { println("class:0"); }
$quoted = '\Q' . $s . '\E';
if ($s ismatch $quoted) { println("quoted:1"); } else { println("quoted:0"); }

if ($s hasmatch '(.)') {
    @capture = matched();
    println("hasmatch:" . size(@capture) . ":" . strlen(@capture[0]) . ":" . asc(@capture[0]));
} else { println("hasmatch:0"); }
if ($s hasmatch '(.)') { println("hasmatch-again:1"); } else { println("hasmatch-again:0"); }

@all = matches($s, '(.)');
println("matches:" . size(@all) . ":" . strlen(@all[0]) . ":" . asc(@all[0]));

$text = "a" . $s . "b";
println("find-any:" . find($text, '.', 1));
println("find-b:" . find($text, 'b', 0));
println("find-b-neg:" . find($text, 'b', -1));

@allparts = split('.', $s . "x", -1);
println("split-all:" . size(@allparts));
@kept = split(',', $s . ",x", -1);
println("split-keep:" . size(@kept) . ":" . strlen(@kept[0]) . ":" . asc(@kept[0]) . ":" . strlen(@kept[1]));

$replaced = replace($s, '.', "x");
println("replace:" . $replaced . ":" . strlen($replaced));
$captured = replace("a" . $s . "b", '(.)', '<$1>');
println("replace-capture:" . strlen($captured) . ":" . asc(charAt($captured, 4)));

$octets = pack("B3", 237, 160, 128);
@octetparts = split('.', $octets, -1);
println("binary-elements:" . size(@octetparts));
`

const utf16RegexProbeOutput = "ismatch:1\n" +
	"literal:1\n" +
	"class:1\n" +
	"quoted:1\n" +
	"hasmatch:1:1:55296\n" +
	"hasmatch-again:0\n" +
	"matches:1:1:55296\n" +
	"find-any:1\n" +
	"find-b:2\n" +
	"find-b-neg:2\n" +
	"split-all:3\n" +
	"split-keep:2:1:55296:1\n" +
	"replace:x:1\n" +
	"replace-capture:9:55296\n" +
	"binary-elements:4\n"

func TestSleepRegexTreatsLoneSurrogateAsOneElement(t *testing.T) {
	got := runPureGoJavaRegexProbe(t, utf16RegexProbeSource)
	if !bytes.Equal(got, []byte(utf16RegexProbeOutput)) {
		t.Fatalf("UTF-16 regex probe mismatch\nwant:\n%sgot:\n%s", utf16RegexProbeOutput, got)
	}
}

func TestSleepRegexLoneSurrogateOffsets(t *testing.T) {
	t.Parallel()
	expression, err := compileSleepRegex(`(.)`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []uint16{0xd800, 0xdfff} {
		input := sleepCanonicalString(sleepUTF16CharacterValue(unit))
		indices, matchErr := expression.FindStringSubmatchIndex(input)
		if matchErr != nil {
			t.Fatalf("match U+%04X: %v", unit, matchErr)
		}
		if want := []int{0, 3, 0, 3}; !reflect.DeepEqual(indices, want) {
			t.Fatalf("match U+%04X indices = %v, want %v", unit, indices, want)
		}
		capture := sleepRegexCaptures(input, indices)
		if len(capture) != 1 || !equalUTF16Units(sleepStringUnits(capture[0]), []uint16{unit}) {
			t.Fatalf("match U+%04X capture = %#v", unit, capture)
		}
	}
}

// TestSleepRegexLoneSurrogateOfficialJARDifferential is opt-in because the
// BSD Sleep JAR is supplied separately. Its pinned hash makes the Java
// RegexBridge result a reproducible oracle without adding it to the project.
func TestSleepRegexLoneSurrogateOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	javaOutput, err := officialSleepJavaCommand(java, "-jar", jar, "-e", utf16RegexProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep UTF-16 regex probe: %v\n%s", err, javaOutput)
	}
	goOutput := runPureGoJavaRegexProbe(t, utf16RegexProbeSource)
	if !bytes.Equal(goOutput, javaOutput) {
		t.Fatalf("official Sleep UTF-16 regex output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
	}
}
