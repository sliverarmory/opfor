package opfor

import (
	"bytes"
	"testing"
)

const regexUTF16ZeroWidthProbe = `sub units {
    local('$text $result $index');
    $text = $1;
    $result = "" . strlen($text);
    for ($index = 0; $index < strlen($text); $index++) {
        $result .= ":" . asc(charAt($text, $index));
    }
    return $result;
}
sub probe {
    local('$pattern $text $result $part');
    $pattern = $1;
    $text = $2;
    println("P=" . $pattern . " S=" . units($text));
    println("A=" . units([$text replaceAll: $pattern, "-"]));
    println("F=" . units([$text replaceFirst: $pattern, "-"]));
    @parts = [$text split: $pattern, -1];
    $result = "X=" . size(@parts);
    foreach $part (@parts) { $result .= "|" . units($part); }
    println($result);
}
$pair = chr(0xD83D) . chr(0xDE00);
probe("(?=.)", $pair);
probe("(?=a?)", $pair);
probe("(?=.{0})", $pair);
probe("^", $pair);
probe("$", $pair);
probe("(?<=.)", $pair);
probe("(?<!.)", $pair);
$prefixed = "a" . $pair;
probe("(?=.)", $prefixed);
probe("(?=a?)", $prefixed);
probe("(?=.{0})", $prefixed);
probe("^", $prefixed);
probe("$", $prefixed);
probe("(?<=.)", $prefixed);
probe("(?<!.)", $prefixed);
println("dot=" . units([$pair replaceAll: '.', '-']));
println("codepoint=" . units([$pair replaceAll: '\x{1F600}', '-']));
println("sleep-replace=" . units(replace($pair, '(?=.)', '-')));
@sleepParts = split('(?=.)', $pair, -1);
$sleepResult = "sleep-split=" . size(@sleepParts);
foreach $part (@sleepParts) { $sleepResult .= "|" . units($part); }
println($sleepResult);
`

const regexUTF16ZeroWidthOutput = `P=(?=.) S=2:55357:56832
A=4:45:55357:45:56832
F=3:45:55357:56832
X=2|1:55357|1:56832
P=(?=a?) S=2:55357:56832
A=5:45:55357:45:56832:45
F=3:45:55357:56832
X=3|1:55357|1:56832|0
P=(?=.{0}) S=2:55357:56832
A=5:45:55357:45:56832:45
F=3:45:55357:56832
X=3|1:55357|1:56832|0
P=^ S=2:55357:56832
A=3:45:55357:56832
F=3:45:55357:56832
X=1|2:55357:56832
P=$ S=2:55357:56832
A=3:55357:56832:45
F=3:55357:56832:45
X=2|2:55357:56832|0
P=(?<=.) S=2:55357:56832
A=3:55357:56832:45
F=3:55357:56832:45
X=2|2:55357:56832|0
P=(?<!.) S=2:55357:56832
A=4:45:55357:45:56832
F=3:45:55357:56832
X=2|1:55357|1:56832
P=(?=.) S=3:97:55357:56832
A=6:45:97:45:55357:45:56832
F=4:45:97:55357:56832
X=3|1:97|1:55357|1:56832
P=(?=a?) S=3:97:55357:56832
A=7:45:97:45:55357:45:56832:45
F=4:45:97:55357:56832
X=4|1:97|1:55357|1:56832|0
P=(?=.{0}) S=3:97:55357:56832
A=7:45:97:45:55357:45:56832:45
F=4:45:97:55357:56832
X=4|1:97|1:55357|1:56832|0
P=^ S=3:97:55357:56832
A=4:45:97:55357:56832
F=4:45:97:55357:56832
X=1|3:97:55357:56832
P=$ S=3:97:55357:56832
A=4:97:55357:56832:45
F=4:97:55357:56832:45
X=2|3:97:55357:56832|0
P=(?<=.) S=3:97:55357:56832
A=5:97:45:55357:56832:45
F=4:97:45:55357:56832
X=3|1:97|2:55357:56832|0
P=(?<!.) S=3:97:55357:56832
A=5:45:97:55357:45:56832
F=4:45:97:55357:56832
X=2|2:97:55357|1:56832
dot=1:45
codepoint=1:45
sleep-replace=4:45:55357:45:56832
sleep-split=2|1:55357|1:56832
`

func TestPortableJavaStringRegexZeroWidthUTF16Compatibility(t *testing.T) {
	got := runPureGoJavaRegexProbe(t, regexUTF16ZeroWidthProbe)
	if !bytes.Equal(got, []byte(regexUTF16ZeroWidthOutput)) {
		t.Fatalf("UTF-16 zero-width regex output mismatch\nwant:\n%sgot:\n%s", regexUTF16ZeroWidthOutput, got)
	}
}

func TestPortableJavaStringRegexZeroWidthUTF16OfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	want, err := officialSleepJavaCommand(java, "-jar", jar, "-e", regexUTF16ZeroWidthProbe).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep UTF-16 zero-width probe: %v\n%s", err, want)
	}
	got := runPureGoJavaRegexProbe(t, regexUTF16ZeroWidthProbe)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep UTF-16 zero-width output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}
