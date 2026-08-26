package opfor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestOfficialSleepSerializationVectors pins the Java-produced inputs used by
// the interoperability tests. The generator, source artifact, and provenance
// are documented beside these files; the official Sleep JAR is deliberately
// not part of this repository.
func TestOfficialSleepSerializationVectors(t *testing.T) {
	const root = "testdata/serialization/official-sleep-2.1"
	vectors := map[string]string{
		"array-cycle.ser":          "adfc0f43ac9c85bd6c9a17f52d3b92de4a63b7cdf33a758e807c70dccf2c14ca",
		"array-shared.ser":         "0ff01d474fc09fa373e70c4e3f9ce43d2f4b6f163ace6c30e50c12cb2833dc4a",
		"closure-callcc.ser":       "f2f1e67967698572d2320958857a7898b14922f5f7ed21053f882519997b1b15",
		"closure-foreach.ser":      "b536ff6a39e51d369cb5b07712f7932827e34d4911e635b0a80019f46feeb0af",
		"closure-local-stack.ser":  "6b12f8076ce9ffd81a355b814c8ff4bc87d98b9d4a0a3a1a449d767efe656a31",
		"closure-print.ser":        "0a710eac7ba5cf057ee1480f28203cb42038e50fd32f6569ef23939789e68dfb",
		"closure-unsuspended.ser":  "a18df37926738a7b3a4ed2a2eeb3a5bbe848e14eca04057c10bfde69fb8de666",
		"closure-yielded.ser":      "49d62eb1152a4a584dcca8034f1a885d93e41048013168778eafcb318a20553d",
		"concatenated-scalars.ser": "78f3691dea693160dc8ccce857fc0fb97a790f0cb56bf509ad9bead286126b17",
		"hash.ser":                 "9789780e3e558775544e1ad58a49d9563a81e64adfbdfcefea08772cd35c8c94",
		"ordered-hash.ser":         "25ac4ec8007d88543b34578195ababa1e508d5a0206bf6e71b9fdfaec4bd9a70",
		"raw-binary-string.ser":    "0f2dcdda5878c80d1e33c92c2899c180df5817303188ab45873111c1e43732af",
		"raw-boolean.ser":          "9385c3517cc8b47aead513d05ebfd86802c3c090b8bec7378bc54c77684cad24",
		"raw-class-string.ser":     "fbeefdc004637a74435714c112939a414e21a16eb263f9a9dc5034f796f5684c",
		"raw-double.ser":           "db28ab319046694554413f4b7051e46ca001c47dfadb3ca288067cf3f3df8f5f",
		"raw-int.ser":              "1efc78cd29e7021a6ba7cb60935292205820b1cc74f757a2dc0763f24a72c408",
		"raw-long.ser":             "7675657e9c1fa861cac496e69c45fdceacbf4ad8603901f2292d7eb586dfaa2c",
		"raw-string.ser":           "6d120be3ec4588f0660fffb4dc841ff2acf2e86190d3559d4cbfb3d0861d9b62",
		"scalar-double.ser":        "c9b11879854f0e8de1f552811175c921c87ef8f6790167d0e90d3dd7a6333684",
		"scalar-binary-string.ser": "22d7ed77b1560a5e542f6ee5775613b464bde06ef80377a7ecf82afda62bedfe",
		"scalar-int.ser":           "8420263bb983e045f88caffb35a03c692b6f9753ccfd8d285bf0a5db4595d86e",
		"scalar-long.ser":          "ec55ccc148b3f9f4fecb062cb982655cc27e8a231c1d67040b54c031df06718b",
		"scalar-null.ser":          "a7094e4ce3799e943e31da29fb76811270480524bd7c262d3f23720700da3fb7",
		"scalar-string.ser":        "3641ab8912b7af5553f53367f0587d32fd3f28742bbb3af8b5269f64a7e0e853",
	}

	for name, wantHash := range vectors {
		name, wantHash := name, wantHash
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(data, []byte{0xac, 0xed, 0x00, 0x05}) {
				t.Fatalf("stream header = % x, want ac ed 00 05", data[:min(4, len(data))])
			}
			digest := sha256.Sum256(data)
			if got := hex.EncodeToString(digest[:]); got != wantHash {
				t.Fatalf("SHA-256 = %s, want %s", got, wantHash)
			}
		})
	}
}
