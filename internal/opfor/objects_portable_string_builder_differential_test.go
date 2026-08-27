package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOfficialSleepPortableJavaStringBuilderDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	source := `$builder = [new StringBuilder: 2];
println("new=" . [$builder length] . "/" . [$builder capacity] . "/" . [$builder toString]);
[$builder append: "ab"];
[$builder insert: 1, "😀"];
println("insert=" . [$builder length] . "/" . [$builder capacity] . "/" . [$builder toString]);
[$builder delete: 0, 1];
[$builder replace: 2, 3, "Z"];
println("edit=" . [$builder toString] . "/" . [$builder indexOf: "Z"] . "/" . [$builder substring: 0, 2]);
[$builder reverse];
println("reverse=" . [$builder toString]);
$buffer = [new StringBuffer: $builder];
[$buffer append: "!"];
println("buffer=" . [$buffer length] . "/" . [$buffer capacity] . "/" . [$buffer toString]);
`
	directory := t.TempDir()
	mainPath := filepath.Join(directory, "mutable-strings.sl")
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, mainPath)
	reference, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep: %v\n%s", err, reference)
	}
	program, err := CompileString(mainPath, source)
	if err != nil {
		t.Fatalf("CompileString: %v", err)
	}
	var output bytes.Buffer
	runtime, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute: %v\n%s", err, output.String())
	}
	if !bytes.Equal(output.Bytes(), reference) {
		t.Fatalf("official mutable-string mismatch\nwant:\n%s\ngot:\n%s", reference, output.Bytes())
	}
}
