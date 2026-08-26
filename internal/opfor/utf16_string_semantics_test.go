package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"testing"
)

const utf16CoreProbeSource = `
println(strlen("é"));
println(strlen("😀"));
println(asc(charAt("😀", 0)));
println(asc(charAt("😀", 1)));
println(indexOf("x😀y", "y"));
println(substr("a😀b", 1, 3));
$high = chr(55357);
$low = chr(56832);
$joined = $high . $low;
println(strlen($joined));
println($joined);
$interpolated = "$high$low";
println(strlen($interpolated));
println($interpolated);
println(strlen(left("a😀b", 3)));
println(asc(charAt(left("a😀b", 3), 2)));
println(strlen(right("a😀b", 3)));
println(asc(charAt(right("a😀b", 3), 0)));
println(strlen(mid("a😀b", 1, 2)));
println(asc(charAt(mid("a😀b", 1, 2), 1)));
println(replaceAt("a😀b", "X", 1, 2));
println(strrep("a😀b😀", "😀", "X"));
println("😀" cmp "😁");
println("😀" cmp "\uE000");
if ("😀" isin "x😀y") { println(1); } else { println(0); }
println(strlen("😀" x 3));
println(asc(chr(4660)));
println(pack("B2", 195, 169));
$binary = pack("B", 233);
println(strlen($binary));
if ($binary eq "é") { println(1); } else { println(0); }
%units[$binary] = "yes";
println(%units["é"]);
println(strlen("\ud83d\ude00"));
println(asc(charAt("\ud83d\ude00", 0)));
println(strlen("\xE9"));
println(asc("\xE9"));
println(asc(uc(chr(55357))));
println(strlen(lc(chr(56832))));
println(replace(pack("B", 233), "é", "X"));
@utfparts = split("é", "x" . pack("B", 233) . "y");
println(join("|", @utfparts));
`

const utf16CoreProbeOutput = "1\n2\n55357\n56832\n3\n😀\n2\n😀\n2\n😀\n" +
	"3\n56832\n3\n55357\n2\n56832\naXb\naXbX\n-1\n-1987\n1\n6\n4660\nÃ©\n1\n1\nyes\n" +
	"2\n55357\n1\n233\n" +
	"55357\n1\nX\nx|y\n"

func TestSleepStringsUseJavaUTF16CodeUnits(t *testing.T) {
	t.Parallel()

	high := sleepUTF16CharacterValue(0xd83d)
	low := sleepUTF16CharacterValue(0xde00)
	if got, want := []byte(high.String()), []byte{0xed, 0xa0, 0xbd}; !bytes.Equal(got, want) {
		t.Fatalf("high-surrogate host spelling = %x, want %x", got, want)
	}
	if got, want := []byte(low.String()), []byte{0xed, 0xb8, 0x80}; !bytes.Equal(got, want) {
		t.Fatalf("low-surrogate host spelling = %x, want %x", got, want)
	}

	if got := runUTF16CoreProbe(t); !bytes.Equal(got, []byte(utf16CoreProbeOutput)) {
		t.Fatalf("UTF-16 probe mismatch\nwant:\n%sgot:\n%s", utf16CoreProbeOutput, got)
	}
}

func TestBinaryStringDistinguishesValidUTF8OctetsFromText(t *testing.T) {
	t.Parallel()

	text := String("é")
	binary := BinaryString([]byte{0xc3, 0xa9})
	if sleepStringLength(text) != 1 || sleepStringLength(binary) != 2 {
		t.Fatalf("text/binary lengths = %d/%d, want 1/2", sleepStringLength(text), sleepStringLength(binary))
	}
	if text.IdentityEqual(binary) {
		t.Fatal("text and byte-carrier UTF-8 unexpectedly compare equal")
	}
	if !binary.IsBinaryString() || text.IsBinaryString() {
		t.Fatalf("text/binary provenance = %v/%v, want false/true", text.IsBinaryString(), binary.IsBinaryString())
	}
	if got, ok := binary.Bytes(); !ok || !bytes.Equal(got, []byte{0xc3, 0xa9}) {
		t.Fatalf("BinaryString.Bytes = %x/%v, want c3a9/true", got, ok)
	}
	joined := sleepStringConcat(String("x"), binary, String("😀"))
	if got := sleepStringLength(joined); got != 5 {
		t.Fatalf("mixed concatenation length = %d, want 5", got)
	}
	if got, want := joined.String(), "x"+string([]byte{0xc3, 0xa9})+"😀"; got != want {
		t.Fatalf("mixed concatenation bytes = %x, want %x", []byte(got), []byte(want))
	}
}

func TestSleepStringsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for UTF-16 differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}
	want, err := osexec.Command(java, "-jar", jar, "-e", utf16CoreProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep UTF-16 probe: %v\n%s", err, want)
	}
	got := runUTF16CoreProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep UTF-16 output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runUTF16CoreProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Eval(context.Background(), "utf16-strings.sl", utf16CoreProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
