package opfor

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

const sleepBasicStringsIsNumberUTF16ProbeName = "sleep-basic-strings-isnumber-utf16.sl"

// BasicStrings.decide calls Character.isDigit(char) while walking the Java
// String one UTF-16 code unit at a time. BMP decimal digits are consequently
// accepted, but every supplementary-plane digit is seen as two non-digit
// surrogate chars and rejected.
const sleepBasicStringsIsNumberUTF16Probe = `if (-isnumber "123") { println("ascii=true"); } else { println("ascii=false"); }
if (-isnumber "١٢٣") { println("arabic=true"); } else { println("arabic=false"); }
if (-isnumber "１２３") { println("fullwidth=true"); } else { println("fullwidth=false"); }
if (-isnumber "𝟎") { println("supplementary=true"); } else { println("supplementary=false"); }
if (!-isnumber "𝟎") { println("negated=true"); } else { println("negated=false"); }
if (-isnumber ".1") { println("leading-dot=true"); } else { println("leading-dot=false"); }
if (-isnumber "1.") { println("trailing-dot=true"); } else { println("trailing-dot=false"); }
if (-isnumber "1.2") { println("decimal=true"); } else { println("decimal=false"); }
`

const sleepBasicStringsIsNumberUTF16Output = `ascii=true
arabic=true
fullwidth=true
supplementary=false
negated=true
leading-dot=true
trailing-dot=false
decimal=true
`

func TestSleepBasicStringsIsNumberUTF16Compatibility(t *testing.T) {
	if got := runSleepBasicStringsIsNumberUTF16Probe(t); !bytes.Equal(got, []byte(sleepBasicStringsIsNumberUTF16Output)) {
		t.Fatalf("BasicStrings -isnumber UTF-16 output mismatch\nwant:\n%sgot:\n%s", sleepBasicStringsIsNumberUTF16Output, got)
	}
}

func TestSleepBasicStringsIsNumberUTF16OfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepBasicStringsIsNumberUTF16ProbeName)
	if err := os.WriteFile(path, []byte(sleepBasicStringsIsNumberUTF16Probe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicStrings -isnumber UTF-16 probe: %v\n%s", err, want)
	}
	if got := runSleepBasicStringsIsNumberUTF16Probe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep BasicStrings -isnumber UTF-16 output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepBasicStringsIsNumberUTF16Probe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), sleepBasicStringsIsNumberUTF16ProbeName, sleepBasicStringsIsNumberUTF16Probe); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return append([]byte(nil), output.Bytes()...)
}
