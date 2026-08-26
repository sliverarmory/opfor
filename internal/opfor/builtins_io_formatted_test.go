package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
)

// TestSleepBasicIOFormattedSourceContract covers Cobalt-Strike/sleep@60ac3ff9
// BasicIO.java lines 91-92, 657-682, 692-929, 933-1158, and 1229-1261,
// together with IOObject.java lines 171-182 and 335-357. In particular, bread
// reads directly from the shared buffered stream, M/R participates in the
// handle's mark state, a partial field closes a live handle, and each o field
// consumes exactly one independent Java serialization stream.
func TestSleepBasicIOFormattedSourceContract(t *testing.T) {
	t.Parallel()

	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()

	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "bwrite", handle,
		String("I- Z5 U*"), Int(16909060), String("xy"), String("Q"))
	mustCallIOBuiltin(t, runtime, functions, "writeb", handle, String("TAIL"))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)

	values := binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", handle, String("M I- R I- Z5 U*")))
	if len(values) != 4 || values[0].Int64() != 16909060 || values[1].Int64() != 16909060 ||
		values[2].String() != "xy" || values[3].String() != "Q" {
		t.Fatalf("bread marked fields = %v", describeBinaryValues(values))
	}
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String(); got != "TAIL" {
		t.Fatalf("unread tail = %q, want TAIL", got)
	}
	mustCallIOBuiltin(t, runtime, functions, "reset", handle)
	values = binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", handle, String("I-")))
	if len(values) != 1 || values[0].Int64() != 16909060 {
		t.Fatalf("bread after reset = %v", describeBinaryValues(values))
	}
	if got, want := []byte(mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String()),
		[]byte{'x', 'y', 0, 0, 0, 0, 'Q', 0, 0, 'T', 'A', 'I', 'L'}; !bytes.Equal(got, want) {
		t.Fatalf("replayed unread bytes = %x, want %x", got, want)
	}

	partial := readableMemoryHandle(t, runtime, functions, string([]byte{0x12, 0x34, 'X'}))
	values = binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", partial, String("S I")))
	if len(values) != 1 || values[0].Int64() != 0x1234 {
		t.Fatalf("partial bread = %v, want [4660]", describeBinaryValues(values))
	}
	assertIOEOF(t, runtime, functions, partial, true)
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", partial, Int(-1)); !got.IsNull() {
		t.Fatalf("partial field left a readable handle: %s", got.Describe())
	}

	objects := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "bwrite", objects, String("o B"), String("value"), Int(7))
	mustCallIOBuiltin(t, runtime, functions, "closef", objects)
	values = binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", objects, String("o")))
	if len(values) != 1 || values[0].String() != "value" {
		t.Fatalf("bread object = %v", describeBinaryValues(values))
	}
	values = binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", objects, String("B")))
	if len(values) != 1 || values[0].Int64() != 7 {
		t.Fatalf("byte after serialized root = %v, want [7]", describeBinaryValues(values))
	}
	assertIOEOF(t, runtime, functions, objects, false)

	openEmpty := readableMemoryHandle(t, runtime, functions, "")
	if got := mustCallIOBuiltin(t, runtime, functions, "bread", openEmpty, String("")); got.Kind() != KindArray {
		t.Fatalf("bread(open, empty format) = %s, want array", got.Describe())
	}
	mustCallIOBuiltin(t, runtime, functions, "closef", openEmpty)
	if got := mustCallIOBuiltin(t, runtime, functions, "bread", openEmpty, String("B")); !got.IsNull() {
		t.Fatalf("bread(closed) = %s, want null", got.Describe())
	}
}

type formattedOneByteReader struct {
	data []byte
}

func (reader *formattedOneByteReader) Read(destination []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	if len(destination) == 0 {
		return 0, nil
	}
	destination[0] = reader.data[0]
	reader.data = reader.data[1:]
	return 1, nil
}

// A Reader is allowed to return fewer bytes than requested without reporting
// EOF. DataInputStream's primitive and UTF-16 field reads keep filling the
// field; a short underlying network or pipe read is not a partial-field EOF.
func TestSleepBasicIOFormattedCompletesShortReads(t *testing.T) {
	t.Parallel()

	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x00, 'A', 0x00, 0x00, 'X'}
	handle := ObjectValue(newIOHandle(
		"one-byte-reader",
		&formattedOneByteReader{data: append([]byte(nil), input...)},
		nil,
		false,
		false,
		false,
	))

	values := binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", handle, String("I U*")))
	if len(values) != 2 || values[0].Int64() != 0x01020304 || values[1].String() != "A" {
		t.Fatalf("one-byte formatted reads = %v, want [16909060 A]", describeBinaryValues(values))
	}
	if tail := mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(1)); tail.String() != "X" {
		t.Fatalf("one-byte formatted tail = %s, want X", tail.Describe())
	}
	assertIOEOF(t, runtime, functions, handle, false)
}

func TestSleepBasicIOFormattedConsoleAndHandleArity(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	runtime, err := New(WithStdin(bytes.NewReader([]byte{10, 20})), WithStdout(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()

	values := binaryArrayValues(t, mustCallIOBuiltin(t, runtime, functions, "bread", String("B2")))
	assertBinaryIntegers(t, values, []int64{10, 20})
	mustCallIOBuiltin(t, runtime, functions, "bwrite", String("B2"), ArrayValue(NewArray(Int(65), Int(66))))
	if got := output.String(); got != "BA" {
		t.Fatalf("console bwrite array bytes = %q, want BA", got)
	}

	if _, err := callIOBuiltin(context.Background(), runtime, functions, "bread", String("B"), Int(1)); err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("two-argument bread did not require a handle: %v", err)
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "bwrite", String("B2"), Int(65), Int(66)); err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("three-argument bwrite did not require a handle: %v", err)
	}
}

func TestSleepBasicIOFormattedExactOutput(t *testing.T) {
	t.Parallel()
	got := runPureGoBasicIOFormattedProbe(t)
	if want := []byte(sleepBasicIOFormattedProbeOutput); !bytes.Equal(got, want) {
		t.Fatalf("formatted I/O probe output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

// TestSleepBasicIOFormattedOfficialJARDifferential compares console and
// memory-handle calls against the separately supplied, SHA-256-pinned official
// Sleep 2.1 JAR. The licensed JAR remains optional for ordinary pure-Go CI.
func TestSleepBasicIOFormattedOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for formatted BasicIO differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	const officialSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	goOutput := runPureGoBasicIOFormattedProbe(t)
	command := osexec.Command(java, "-jar", jar, "-e", sleepBasicIOFormattedProbeSource)
	command.Stdin = bytes.NewReader([]byte{10, 20})
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep formatted BasicIO probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(goOutput, javaOutput) {
		t.Fatalf("official Sleep formatted BasicIO output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
	}
}

func runPureGoBasicIOFormattedProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(
		WithStdin(bytes.NewReader([]byte{10, 20})),
		WithStdout(&output),
		WithStderr(&output),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "basic-io-formatted-probe.sl", sleepBasicIOFormattedProbeSource); err != nil {
		t.Fatalf("pure-Go formatted BasicIO probe: %v\n%s", err, output.String())
	}
	return output.Bytes()
}

const sleepBasicIOFormattedProbeSource = `sub show_array {
  println($1 . ":" . size($2) . ":" . join("|", $2));
}

@console = bread("B2");
show_array("console", @console);
println("console-write:");
bwrite("B2", @(65, 66));
println(":end");

$unicode = "é😀";
println("unicode-pack-be:" . unpack("H*", pack("U* U5", $unicode, $unicode))[0]);
println("unicode-pack-le:" . unpack("H*", pack("U-* U-5", $unicode, $unicode))[0]);
@unicode_unpacked = unpack("U* U-*", pack("U*", $unicode) . pack("U-*", $unicode));
println("unicode-unpack:" . join("|", @unicode_unpacked));
$unicode_bytes = allocate();
bwrite($unicode_bytes, "U* U-* U5 U-5", $unicode, $unicode, $unicode, $unicode);
closef($unicode_bytes);
println("unicode-bwrite:" . unpack("H*", readb($unicode_bytes, -1))[0]);
$unicode_fields = allocate();
writeb($unicode_fields, pack("U*", $unicode) . pack("U-*", $unicode) . pack("U5", $unicode) . pack("U-5", $unicode) . "X");
closef($unicode_fields);
@unicode_bread = bread($unicode_fields, "U* U-* U5 U-5");
println("unicode-bread:" . join("|", @unicode_bread));
println("unicode-tail:" . readb($unicode_fields, -1));
$unpaired_be = unpack("U*", pack("H*", "d8000000"))[0];
$unpaired_le = unpack("U-*", pack("H*", "00d80000"))[0];
println("unpaired:" . unpack("H*", pack("U*", $unpaired_be))[0] . ":" . unpack("H*", pack("U-*", $unpaired_le))[0]);

$h = allocate();
bwrite($h, "I- Z5 U*", 16909060, "xy", "Q");
writeb($h, "TAIL");
closef($h);
@first = bread($h, "M I- R I- Z5 U*");
show_array("first", @first);
println("tail:" . readb($h, -1));
reset($h);
@again = bread($h, "I-");
show_array("again", @again);
println("after-reset:" . unpack("H*", readb($h, -1))[0]);

$short = allocate();
writeb($short, pack("S", 4660) . "X");
closef($short);
@partial = bread($short, "S I");
show_array("partial", @partial);
if (-eof $short) { println("partial-eof:1"); } else { println("partial-eof:0"); }
println("partial-tail:" . readb($short, -1));

$objects = allocate();
$plus = { return $1 + 2; };
bwrite($objects, "o B", $plus, 7);
closef($objects);
@decoded = bread($objects, "o");
println("closure:" . [@decoded[0]: 40]);
println("object-tail:" . bread($objects, "B")[0]);
if (-eof $objects) { println("object-eof:1"); } else { println("object-eof:0"); }
`

const sleepBasicIOFormattedProbeOutput = "console:2:10|20\n" +
	"console-write:\n" +
	"BA:end\n" +
	"unicode-pack-be:00e9d83dde00000000e9d83dde0000000000\n" +
	"unicode-pack-le:e9003dd800de0000e9003dd800de00000000\n" +
	"unicode-unpack:é😀|é😀\n" +
	"unicode-bwrite:00e9d83dde000000e9003dd800de000000e9d83dde0000000000e9003dd800de00000000\n" +
	"unicode-bread:é😀|é😀|é😀|é😀\n" +
	"unicode-tail:X\n" +
	"unpaired:d8000000:00d80000\n" +
	"first:4:16909060|16909060|xy|Q\n" +
	"tail:TAIL\n" +
	"again:1:16909060\n" +
	"after-reset:7879000000005100005441494c\n" +
	"partial:1:4660\n" +
	"partial-eof:1\n" +
	"partial-tail:\n" +
	"closure:42\n" +
	"object-tail:7\n" +
	"object-eof:0\n"
