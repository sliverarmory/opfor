package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const utf16HashKeyProbeSource = `
$hash = ohash();
$high = chr(55357);
$hash[$high] = "H";
$key = keys($hash)[0];
println(strlen($key) . ":" . asc($key) . ":" . $hash[$key]);

$map = [SleepUtils getMapFromHash: $hash];
@mapkeys = [SleepUtils getArrayWrapper: [$map keySet]];
$mapkey = @mapkeys[0];
println(strlen($mapkey) . ":" . asc($mapkey) . ":" . [$map get: $mapkey]);

$wrapped = [SleepUtils getHashWrapper: $map];
$wrappedkey = keys($wrapped)[0];
println(strlen($wrappedkey) . ":" . asc($wrappedkey) . ":" . $wrapped[$wrappedkey]);
`

const utf16HashKeyProbeOutput = "1:55357:H\n1:55357:H\n1:55357:H\n"

func TestHashKeyValuesPreserveUTF16IdentityAndProvenance(t *testing.T) {
	t.Parallel()

	high := sleepUTF16CharacterValue(0xd83d)
	binary := BinaryString([]byte{0xc3, 0xa9})
	text := String("é")
	hash := NewOrderedHash()
	hash.SetValue(high, String("high"))
	hash.SetValue(binary, String("binary"))
	hash.SetValue(text, String("text"))

	keys := hash.KeyValues()
	if len(keys) != 3 {
		t.Fatalf("KeyValues length = %d, want 3", len(keys))
	}
	if got := sleepStringUnits(keys[0]); len(got) != 1 || got[0] != 0xd83d {
		t.Fatalf("surrogate key units = %x, want d83d", got)
	}
	if got, ok := keys[1].Bytes(); !ok || !keys[1].IsBinaryString() || !bytes.Equal(got, []byte{0xc3, 0xa9}) {
		t.Fatalf("binary key = %x/binary=%v, want c3a9/binary", got, keys[1].IsBinaryString())
	}
	if got := sleepStringUnits(keys[2]); len(got) != 1 || got[0] != 0x00e9 || keys[2].IsBinaryString() {
		t.Fatalf("text key = %x/binary=%v, want 00e9/text", got, keys[2].IsBinaryString())
	}

	for index, key := range []Value{high, binary, text} {
		value, ok := hash.GetValue(key)
		if !ok || value.String() != []string{"high", "binary", "text"}[index] {
			t.Fatalf("GetValue(%d) = %s/%v", index, value.Describe(), ok)
		}
	}

	// Java-equal replacements update the value while retaining the first key
	// object. This matters to OPFOR because raw-byte provenance is host-visible.
	hash.SetValue(String("Ã©"), String("replacement"))
	keys = hash.KeyValues()
	if len(keys) != 3 || !keys[1].IsBinaryString() {
		t.Fatalf("equal-key replacement changed key provenance: %#v", keys)
	}
	if value, ok := hash.GetValue(binary); !ok || value.String() != "replacement" {
		t.Fatalf("replacement lookup = %s/%v", value.Describe(), ok)
	}
}

func TestHashBuiltinsAndPortableJavaMapPreserveUTF16Keys(t *testing.T) {
	t.Parallel()

	if got := runUTF16HashKeyProbe(t); !bytes.Equal(got, []byte(utf16HashKeyProbeOutput)) {
		t.Fatalf("UTF-16 hash-key probe mismatch\nwant:\n%sgot:\n%s", utf16HashKeyProbeOutput, got)
	}

	binary := BinaryString([]byte{0xc3, 0xa9})
	hash := NewOrderedHash()
	hash.SetValue(binary, String("value"))
	mapping := newPortableJavaMap("HashMap", hash)
	keySet, handled, err := mapping.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "keySet"})
	if err != nil || !handled {
		t.Fatalf("HashMap.keySet = %s, handled=%v, err=%v", keySet.Describe(), handled, err)
	}
	values, ok := portableCollectionValues(keySet)
	if !ok || len(values) != 1 || !values[0].IsBinaryString() {
		t.Fatalf("HashMap.keySet values = %#v/%v, want one binary key", values, ok)
	}
	roundTrip := mapping.snapshotHash().KeyValues()
	if len(roundTrip) != 1 || !roundTrip[0].IsBinaryString() {
		t.Fatalf("HashMap -> Hash key provenance = %#v", roundTrip)
	}
}

func TestSleepHashSerializationPreservesUTF16KeyIdentity(t *testing.T) {
	t.Parallel()

	hash := NewOrderedHash()
	hash.SetValue(sleepUTF16CharacterValue(0xd83d), String("H"))
	hash.SetValue(BinaryString([]byte{0xc3, 0xa9}), String("B"))
	stream, err := encodeSleepScalarStream(HashValue(hash))
	if err != nil {
		t.Fatal(err)
	}
	decoded, consumed, err := decodeSleepScalarStream(bytes.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if consumed != int64(len(stream)) {
		t.Fatalf("consumed = %d, want %d", consumed, len(stream))
	}
	decodedHash, ok := decoded.Hash()
	if !ok {
		t.Fatalf("decoded = %s, want hash", decoded.Describe())
	}
	keys := decodedHash.KeyValues()
	if len(keys) != 2 || len(sleepStringUnits(keys[0])) != 1 || sleepStringUnits(keys[0])[0] != 0xd83d {
		t.Fatalf("decoded surrogate key = %#v", keys)
	}
	if got, ok := keys[1].Bytes(); !ok || !keys[1].IsBinaryString() || !bytes.Equal(got, []byte{0xc3, 0xa9}) {
		t.Fatalf("decoded binary key = %x/binary=%v", got, keys[1].IsBinaryString())
	}
	if value, ok := decodedHash.GetValue(keys[0]); !ok || value.String() != "H" {
		t.Fatalf("decoded surrogate lookup = %s/%v", value.Describe(), ok)
	}
	if value, ok := decodedHash.GetValue(keys[1]); !ok || value.String() != "B" {
		t.Fatalf("decoded binary lookup = %s/%v", value.Describe(), ok)
	}
	reencoded, err := encodeSleepScalarStream(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, stream) {
		t.Fatal("UTF-16 hash keys changed across Sleep wire round trip")
	}
}

func TestUTF16HashKeysOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for UTF-16 hash-key verification")
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
	want, err := osexec.Command(java, "-jar", jar, "-e", utf16HashKeyProbeSource).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep UTF-16 hash-key probe: %v\n%s", err, want)
	}
	got := runUTF16HashKeyProbe(t)
	if !bytes.Equal(got, want) {
		t.Fatalf("official Sleep UTF-16 hash-key output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func TestOfficialSleepRoundTripsOPFORUTF16HashKeys(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for UTF-16 hash serialization verification")
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

	hash := NewOrderedHash()
	hash.SetValue(sleepUTF16CharacterValue(0xd83d), String("H"))
	hash.SetValue(BinaryString([]byte{0xc3, 0xa9}), String("B"))
	stream, err := encodeSleepScalarStream(HashValue(hash))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	input := filepath.Join(temporary, "opfor.ser")
	output := filepath.Join(temporary, "sleep.ser")
	if err := os.WriteFile(input, stream, 0o600); err != nil {
		t.Fatal(err)
	}
	consumer := filepath.Join("testdata", "serialization", "consume_utf16_hash_keys.sl")
	probe, err := osexec.Command(java, "-jar", jar, consumer, input, output).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep UTF-16 hash consumer: %v\n%s", err, probe)
	}
	const want = "2\n1:55357:H\n2:195:169:B\n"
	if string(probe) != want {
		t.Fatalf("official Sleep UTF-16 hash consumer mismatch\nwant:\n%sgot:\n%s", want, probe)
	}

	roundTripBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, _, err := decodeSleepScalarStream(bytes.NewReader(roundTripBytes))
	if err != nil {
		t.Fatal(err)
	}
	roundTripHash, ok := roundTrip.Hash()
	if !ok {
		t.Fatalf("round trip = %s, want hash", roundTrip.Describe())
	}
	keys := roundTripHash.KeyValues()
	if len(keys) != 2 || len(sleepStringUnits(keys[0])) != 1 || sleepStringUnits(keys[0])[0] != 0xd83d {
		t.Fatalf("round-trip surrogate key = %#v", keys)
	}
	if got, ok := keys[1].Bytes(); !ok || !keys[1].IsBinaryString() || !bytes.Equal(got, []byte{0xc3, 0xa9}) {
		t.Fatalf("round-trip binary key = %x/binary=%v", got, keys[1].IsBinaryString())
	}
}

func runUTF16HashKeyProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if _, err := runtime.Eval(context.Background(), "utf16-hash-keys.sl", utf16HashKeyProbeSource); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), output.Bytes()...)
}
