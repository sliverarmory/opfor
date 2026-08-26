package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestServeSessionReloadPreservesRawArgumentsAndTransfersPrimary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reload.cna")
	sources := map[string][]byte{
		path: []byte(`sub arguments { return @ARGV; }`),
	}
	runtime, session := newTestServeSession(t, &bytes.Buffer{}, func(name string) ([]byte, error) {
		data, ok := sources[name]
		if !ok {
			return nil, errors.New("missing")
		}
		return append([]byte(nil), data...), nil
	})

	argumentTemplate := json.RawMessage(`[{"nested":[1,2]},9223372036854775807]`)
	loaded, err := session.load(ctx, path, argumentTemplate)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	first := onlyServeScript(t, session)
	if loaded.(map[string]any)["primary"] != true {
		t.Fatalf("load metadata = %#v, want primary", loaded)
	}
	// Mutate the old script's nested @ARGV graph. Reload must decode the stored
	// JSON template rather than reusing this capability-bearing Value graph.
	arguments, err := first.script.Call(ctx, "arguments")
	if err != nil {
		t.Fatalf("call old arguments: %v", err)
	}
	array, ok := arguments.Array()
	if !ok {
		t.Fatalf("old arguments = %s, want array", arguments.Describe())
	}
	values := array.Values()
	hash, ok := values[0].Hash()
	if !ok {
		t.Fatalf("old first argument = %s, want hash", values[0].Describe())
	}
	hash.Set("nested", opfor.String("mutated"))
	for index := range argumentTemplate {
		argumentTemplate[index] = 'x'
	}

	result, _, err := session.reload(ctx, serveRequest{Script: uint64(first.script.ID())})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	second := onlyServeScript(t, session)
	if second.script.ID() == first.script.ID() || first.script.Active() {
		t.Fatalf("reload identities old=%d active=%v new=%d", first.script.ID(), first.script.Active(), second.script.ID())
	}
	metadata := result.(map[string]any)
	if metadata["primary"] != true || metadata["id"] != uint64(second.script.ID()) {
		t.Fatalf("reload metadata = %#v", metadata)
	}
	arguments, err = second.script.Call(ctx, "arguments")
	if err != nil {
		t.Fatalf("call replacement arguments: %v", err)
	}
	array, _ = arguments.Array()
	values = array.Values()
	hash, _ = values[0].Hash()
	nested, _ := hash.Get("nested")
	nestedArray, ok := nested.Array()
	if !ok || len(nestedArray.Values()) != 2 || nestedArray.Values()[0].Int32() != 1 || values[1].Int64() != 9223372036854775807 {
		t.Fatalf("replacement arguments = %s, want pristine JSON template", arguments.Describe())
	}
	if got := runtime.Scripts(); len(got) != 1 || got[0].ID() != second.script.ID() {
		t.Fatalf("runtime scripts = %#v, want replacement only", got)
	}

	result, _, err = session.reload(ctx, serveRequest{
		Script:   uint64(second.script.ID()),
		argsSet:  true,
		argsJSON: json.RawMessage(`null`),
	})
	if err != nil {
		t.Fatalf("reload with null arguments: %v", err)
	}
	third := onlyServeScript(t, session)
	arguments, err = third.script.Call(ctx, "arguments")
	if err != nil {
		t.Fatalf("call null-template arguments: %v", err)
	}
	array, ok = arguments.Array()
	if !ok || len(array.Values()) != 0 {
		t.Fatalf("null-template arguments = %s, want empty array", arguments.Describe())
	}
	metadata = result.(map[string]any)
	if got, ok := metadata["args"].(json.RawMessage); !ok || string(got) != "null" {
		t.Fatalf("null-template metadata args = %#v", metadata["args"])
	}
}

func TestServeSessionReloadPreservesOldOnReadAndCompileFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "preserve.cna")
	source := []byte(`sub value { return "old"; }`)
	readErr := error(nil)
	_, session := newTestServeSession(t, &bytes.Buffer{}, func(string) ([]byte, error) {
		if readErr != nil {
			return nil, readErr
		}
		return append([]byte(nil), source...), nil
	})
	if _, err := session.load(ctx, path, json.RawMessage(`[]`)); err != nil {
		t.Fatalf("load: %v", err)
	}
	old := onlyServeScript(t, session)

	readErr = errors.New("permission denied")
	if _, _, err := session.reload(ctx, serveRequest{Script: uint64(old.script.ID())}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("read-failing reload error = %v", err)
	}
	assertServeScriptString(t, old.script, "value", "old")
	if got := onlyServeScript(t, session); got != old || !old.script.Active() {
		t.Fatalf("read failure replaced old script: %#v", got)
	}

	readErr = nil
	source = []byte(`sub value {`)
	if _, _, err := session.reload(ctx, serveRequest{Script: uint64(old.script.ID())}); err == nil || !strings.Contains(err.Error(), "compile") {
		t.Fatalf("compile-failing reload error = %v", err)
	}
	assertServeScriptString(t, old.script, "value", "old")
	if got := onlyServeScript(t, session); got != old || !old.script.Active() {
		t.Fatalf("compile failure replaced old script: %#v", got)
	}
}

func TestServeSessionReloadDoesNotRollbackAfterReplacementEffects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "effects.cna")
	source := []byte(`sub value { return "old"; }`)
	stderr := &bytes.Buffer{}
	_, session := newTestServeSession(t, stderr, func(string) ([]byte, error) {
		return append([]byte(nil), source...), nil
	})
	if _, err := session.load(ctx, path, json.RawMessage(`[]`)); err != nil {
		t.Fatalf("load: %v", err)
	}
	old := onlyServeScript(t, session)

	source = []byte(`println("replacement-effect"); replacement_load_failure();`)
	if _, _, err := session.reload(ctx, serveRequest{Script: uint64(old.script.ID())}); err == nil || !strings.Contains(err.Error(), "replacement_load_failure") {
		t.Fatalf("effectful reload error = %v", err)
	}
	if old.script.Active() {
		t.Fatal("old script remained active after replacement execution began")
	}
	if got := session.list(); len(got) != 0 || session.primary != 0 {
		t.Fatalf("post-failure session = scripts %#v primary %d", got, session.primary)
	}
	if got := stderr.String(); !strings.Contains(got, "replacement-effect\n") {
		t.Fatalf("replacement output = %q, want visible top-level effect", got)
	}
}

func TestServeSessionReloadUnloadErrorLeavesNoSelectedScript(t *testing.T) {
	t.Parallel()

	cleanupErr := errors.New("cleanup failed")
	runtimeInstance, err := opfor.New(
		opfor.WithScriptLifecycleObserver(opfor.ScriptLifecycleFuncs{
			Unloaded: func(context.Context, *opfor.Script) error { return cleanupErr },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	path := filepath.Join(t.TempDir(), "unload-error.cna")
	dependencies := dependencies{
		readFile: func(string) ([]byte, error) {
			return []byte(`sub value { return "old"; }`), nil
		},
		compile: func(source opfor.Source) (*opfor.Program, error) {
			return opfor.Compile(source)
		},
	}
	session := newServeSession(runtimeInstance, dependencies)
	if _, err := session.load(context.Background(), path, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	old := onlyServeScript(t, session)

	_, _, reloadErr := session.reload(context.Background(), serveRequest{Script: uint64(old.script.ID())})
	if !errors.Is(reloadErr, cleanupErr) {
		t.Fatalf("reload error = %v, want cleanup failure", reloadErr)
	}
	if old.script.Active() {
		t.Fatal("old script remained active after unload cleanup failed")
	}
	if got := session.list(); len(got) != 0 || session.primary != 0 {
		t.Fatalf("post-unload-error session = scripts %#v primary %d", got, session.primary)
	}
}

func TestServeSessionPathResolutionDuplicatesAndReconciliation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	exactPath := directory + string(filepath.Separator) + "." + string(filepath.Separator) + "duplicate.cna"
	cleanPath := filepath.Join(directory, "duplicate.cna")
	alternatePath := directory + string(filepath.Separator) + "child" + string(filepath.Separator) + ".." + string(filepath.Separator) + "duplicate.cna"
	_, session := newTestServeSession(t, &bytes.Buffer{}, func(string) ([]byte, error) {
		return []byte(`sub value { return $__SCRIPT__; }`), nil
	})
	if _, err := session.load(ctx, exactPath, json.RawMessage(`[]`)); err != nil {
		t.Fatalf("load exact: %v", err)
	}
	first := session.scripts[0]
	if _, err := session.load(ctx, cleanPath, json.RawMessage(`[]`)); err != nil {
		t.Fatalf("load clean: %v", err)
	}
	second := session.scripts[1]

	if got, err := session.resolve(uint64(first.script.ID()), alternatePath, false); err != nil || got != first {
		t.Fatalf("numeric selector did not win: got %#v, err %v", got, err)
	}
	if got, err := session.resolve(0, exactPath, false); err != nil || got != first {
		t.Fatalf("exact selector = %#v, %v, want first", got, err)
	}
	if got, err := session.resolve(0, cleanPath, false); err != nil || got != second {
		t.Fatalf("second exact selector = %#v, %v, want second", got, err)
	}
	_, err := session.resolve(0, alternatePath, false)
	want := fmt.Sprintf("script path %q is ambiguous; matching script IDs: %d, %d", alternatePath, first.script.ID(), second.script.ID())
	if err == nil || err.Error() != want {
		t.Fatalf("normalized ambiguity = %v, want %q", err, want)
	}

	if err := first.script.Unload(ctx); err != nil {
		t.Fatalf("external unload: %v", err)
	}
	listed := session.list()
	if len(listed) != 1 || session.primary != 0 {
		t.Fatalf("reconciled list = %#v, primary %d", listed, session.primary)
	}
	if got, err := session.resolve(0, alternatePath, false); err != nil || got != second {
		t.Fatalf("normalized selector after reconcile = %#v, %v", got, err)
	}
}

func TestServeWithoutStartupSupportsMultiScriptLifecycleE2E(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first.cna")
	secondPath := filepath.Join(directory, "second.cna")
	for _, path := range []string{firstPath, secondPath} {
		if err := writeServeTestFile(path, `on ready { println("unexpected-ready"); } sub who { return @ARGV[0]; }`); err != nil {
			t.Fatal(err)
		}
	}
	input := strings.Join([]string{
		`{"id":"before","method":"call","name":"who"}`,
		fmt.Sprintf(`{"id":"load-1","method":"load","path":%q,"args":["first"]}`, firstPath),
		fmt.Sprintf(`{"id":"load-2","method":"load","path":%q,"args":["second"]}`, secondPath),
		`{"id":"primary","method":"call","name":"who"}`,
		`{"id":"targeted","method":"call","script":2,"name":"who"}`,
		`{"id":"list","method":"ls"}`,
		`{"id":"reload","method":"reload","script":1}`,
		`{"id":"reloaded-primary","method":"call","name":"who"}`,
		`{"id":"unload-primary","method":"unload","script":3}`,
		`{"id":"no-promotion","method":"call","name":"who"}`,
		`{"id":"remaining","method":"call","script":2,"name":"who"}`,
		fmt.Sprintf(`{"id":"late-load","method":"load","path":%q,"args":["late"]}`, firstPath),
		`{"id":"still-no-primary","method":"call","name":"who"}`,
		`{"id":"done","method":"shutdown"}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(ctx, Options{Stdin: strings.NewReader(input), Stdout: stdout, Stderr: stderr}, []string{"serve", "--fire-ready"})
	if status != 0 {
		t.Fatalf("serve status = %d, stderr = %q", status, stderr.String())
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("dynamic load synthesized ready event: stderr = %q", got)
	}
	responses := decodeServeResponses(t, stdout.String())
	if len(responses) != 14 {
		t.Fatalf("responses = %#v, want 14", responses)
	}
	if got := responses[0]["error"]; got != "script function calls are unavailable" {
		t.Fatalf("pre-load call = %#v", responses[0])
	}
	assertServeResult(t, responses[3], "first")
	assertServeResult(t, responses[4], "second")
	list, ok := responses[5]["result"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("script list = %#v", responses[5])
	}
	firstMetadata := list[0].(map[string]any)
	secondMetadata := list[1].(map[string]any)
	if firstMetadata["id"] != float64(1) || firstMetadata["primary"] != true || firstMetadata["path"] != firstPath ||
		secondMetadata["id"] != float64(2) || secondMetadata["primary"] != false {
		t.Fatalf("script metadata = %#v", list)
	}
	assertServeResult(t, responses[7], "first")
	if got := responses[8]["result"].(map[string]any); got["id"] != float64(3) || got["path"] != firstPath ||
		got["primary"] != true || got["normalized_path"] != filepath.Clean(firstPath) {
		t.Fatalf("unload result = %#v", responses[8])
	}
	if got := responses[9]["error"]; got != "script function calls are unavailable" {
		t.Fatalf("primary promotion occurred: %#v", responses[9])
	}
	assertServeResult(t, responses[10], "second")
	if got := responses[12]["error"]; got != "script function calls are unavailable" {
		t.Fatalf("late load recreated primary: %#v", responses[12])
	}
	assertServeResult(t, responses[13], "bye")
}

func TestServeDuplicatePathRequiresNumericTargetE2E(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "duplicate.cna")
	if err := writeServeTestFile(path, `sub identity { return $__SCRIPT__; }`); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		fmt.Sprintf(`{"id":1,"method":"load","path":%q}`, path),
		fmt.Sprintf(`{"id":2,"method":"load","path":%q}`, path),
		fmt.Sprintf(`{"id":3,"method":"call","path":%q,"name":"identity"}`, path),
		fmt.Sprintf(`{"id":4,"method":"call","script":1,"path":%q,"name":"identity"}`, filepath.Join(t.TempDir(), "ignored.cna")),
		`{"id":5,"method":"unload","script":1}`,
		fmt.Sprintf(`{"id":6,"method":"call","path":%q,"name":"identity"}`, path),
		`{"id":7,"method":"shutdown"}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(context.Background(), Options{Stdin: strings.NewReader(input), Stdout: stdout, Stderr: stderr}, []string{"serve"})
	if status != 0 {
		t.Fatalf("serve status = %d, stderr = %q", status, stderr.String())
	}
	responses := decodeServeResponses(t, stdout.String())
	wantAmbiguous := fmt.Sprintf("script path %q is ambiguous; matching script IDs: 1, 2", path)
	if responses[2]["error"] != wantAmbiguous {
		t.Fatalf("ambiguous response = %#v, want %q", responses[2], wantAmbiguous)
	}
	assertServeResult(t, responses[3], path)
	assertServeResult(t, responses[5], path)
}

func newTestServeSession(
	t *testing.T,
	stderr io.Writer,
	readFile func(string) ([]byte, error),
) (*opfor.Runtime, *serveSession) {
	t.Helper()
	runtime, err := opfor.New(opfor.WithStdout(stderr), opfor.WithStderr(stderr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	dependencies := dependencies{
		readFile: readFile,
		compile: func(source opfor.Source) (*opfor.Program, error) {
			return opfor.Compile(source)
		},
	}
	return runtime, newServeSession(runtime, dependencies)
}

func onlyServeScript(t *testing.T, session *serveSession) *serveScript {
	t.Helper()
	session.reconcile()
	if len(session.scripts) != 1 {
		t.Fatalf("session scripts = %#v, want one", session.scripts)
	}
	return session.scripts[0]
}

func assertServeScriptString(t *testing.T, script *opfor.Script, name, want string) {
	t.Helper()
	value, err := script.Call(context.Background(), name)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if got := value.String(); got != want {
		t.Fatalf("call %s = %q, want %q", name, got, want)
	}
}

func assertServeResult(t *testing.T, response map[string]any, want any) {
	t.Helper()
	if got := response["result"]; got != want {
		t.Fatalf("response = %#v, want result %#v", response, want)
	}
}

func writeServeTestFile(path, source string) error {
	return os.WriteFile(path, []byte(source), 0o600)
}
