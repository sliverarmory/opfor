package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepBasicIODigestAlgorithmsProbeName = "sleep-basicio-digest-algorithms.sl"

// BasicIO.digest delegates its algorithm argument to
// MessageDigest.getInstance. The provider used by the pinned Sleep 2.1 JAR
// supports the two truncated SHA-512 variants and all four SHA-3 widths. The
// truncated SHA-512 aliases omit only the first hyphen; SHA-3 does not accept
// a fully compact spelling. A rejected algorithm is a checkError soft failure,
// so the active block continues.
const sleepBasicIODigestAlgorithmsProbe = `foreach $algorithm (@("SHA-512/224", "SHA-512/256", "SHA3-224", "SHA3-256", "SHA3-384", "SHA3-512")) {
  $value = digest("abc", $algorithm);
  checkError($problem);
  println($algorithm . "|" . unpack("H*", $value)[0] . "|" . $problem);
}
println("alias512=" . unpack("H*", digest("abc", "sha512/224"))[0]);
println("alias512b=" . unpack("H*", digest("abc", "SHA512/256"))[0]);
println("alias3=" . unpack("H*", digest("abc", "sha3-256"))[0]);
$handle = allocate();
$state = digest($handle, ">SHA3-256");
writeb($handle, "abc");
println("stream=" . unpack("H*", digest($state))[0]);
closef($handle);
$bad = digest("abc", "SHA3224");
checkError($problem);
println("bad=" . strlen($bad) . "|" . $problem);
println("after");
`

const sleepBasicIODigestAlgorithmsOutput = `SHA-512/224|4634270f707b6a54daae7530460842e20e37ed265ceee9a43e8924aa|
SHA-512/256|53048e2681941ef99b2e29b76b4c7dabe4c2d0c634fc6d46e0e2f13107e7af23|
SHA3-224|e642824c3f8cf24ad09234ee7d3c766fc9a3a5168d0c94ad73b46fdf|
SHA3-256|3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532|
SHA3-384|ec01498288516fc926459f58e2c6ad8df9b473cb0fc08c2596da7cf0e49be4b298d88cea927ac7f539f1edf228376d25|
SHA3-512|b751850b1a57168a5693cd924b6b096e08f621827444f70d884f5d0240d2712e10e116e9192af3c91a7ec57647e3934057340b4cf408d5a56592f8274eec53f0|
alias512=4634270f707b6a54daae7530460842e20e37ed265ceee9a43e8924aa
alias512b=53048e2681941ef99b2e29b76b4c7dabe4c2d0c634fc6d46e0e2f13107e7af23
alias3=3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532
stream=3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532
bad=0|java.security.NoSuchAlgorithmException: SHA3224 MessageDigest not available
after
`

func TestSleepBasicIODigestAlgorithmsCompatibility(t *testing.T) {
	got := runSleepBasicIODigestAlgorithmsProbe(t)
	if !bytes.Equal(got, []byte(sleepBasicIODigestAlgorithmsOutput)) {
		t.Fatalf("BasicIO digest algorithm output mismatch\nwant:\n%sgot:\n%s", sleepBasicIODigestAlgorithmsOutput, got)
	}
}

func TestSleepBasicIODigestAlgorithmsOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, sleepBasicIODigestAlgorithmsProbeName)
	if err := os.WriteFile(path, []byte(sleepBasicIODigestAlgorithmsProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep BasicIO digest-algorithm probe: %v\n%s", err, want)
	}
	if got := runSleepBasicIODigestAlgorithmsProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep BasicIO digest-algorithm mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepBasicIODigestAlgorithmsProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(
		context.Background(), sleepBasicIODigestAlgorithmsProbeName, sleepBasicIODigestAlgorithmsProbe,
	); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	return append([]byte(nil), output.Bytes()...)
}
