package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sliverarmory/opfor"
)

func TestServeKeepsScriptLoadedAndSeparatesProtocolOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "persistent.cna")
	source := `
println("loaded");
on ping { return @($1, $2); }
alias echo { return "$1 $+ : $+ $2"; }
alias nuller { return $null; }
alias binary { return base64_encode($1); }
sub direct { return @($0, $1, $2); }
command inspect { return @($0, $1, $2, $3); }
alias console_alias { return @($0, $1, $2, $3); }
command exact { return "first"; }
command exact { return "second"; }
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"id":1,"method":"event","name":"ping","args":["payload",7]}`,
		`{"id":2,"method":"binding","kind":"alias","name":"echo","args":["left","right"]}`,
		`{"id":3,"method":"invoke","name":"size","args":[[1,2,3]]}`,
		`{"id":4,"method":"bindings","kind":"on"}`,
		`{"id":5,"method":"binding","kind":"alias","name":"nuller"}`,
		`{"id":6,"method":"invoke","name":"base64_decode","args":["AP8="]}`,
		`{"id":7,"method":"binding","kind":"alias","name":"binary","args":[{"$opfor":"binary","base64":"AP8="}]}`,
		`{"id":8,"method":"call","name":"direct","args":["left","right"]}`,
		`{"id":9,"method":"console","kind":"command","name":"inspect","raw":"inspect \"two words\" \"\" tail"}`,
		`{"id":10,"method":"console","kind":"alias","name":"console_alias","raw":"console_alias \"two words\" tail","session":42}`,
		`{"id":11,"method":"binding","script":1,"binding_id":8}`,
		`{"id":12,"method":"shutdown"}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(context.Background(), Options{
		Stdin:  strings.NewReader(input),
		Stdout: stdout,
		Stderr: stderr,
	}, []string{"serve", path, "script-arg"})
	if status != 0 {
		t.Fatalf("Execute status = %d, stderr = %q", status, stderr.String())
	}
	if got := stderr.String(); got != "loaded\n" {
		t.Fatalf("script stderr = %q, want %q", got, "loaded\n")
	}

	responses := decodeServeResponses(t, stdout.String())
	if len(responses) != 12 {
		t.Fatalf("responses = %#v, want 12 entries", responses)
	}
	assertServeJSON(t, responses[0], map[string]any{"id": float64(1), "result": []any{[]any{"payload", float64(7)}}})
	assertServeJSON(t, responses[1], map[string]any{"id": float64(2), "result": "left:right"})
	assertServeJSON(t, responses[2], map[string]any{"id": float64(3), "result": float64(3)})
	bindings, ok := responses[3]["result"].([]any)
	if !ok || len(bindings) != 1 {
		t.Fatalf("bindings response = %#v", responses[3])
	}
	binding, ok := bindings[0].(map[string]any)
	if !ok || binding["kind"] != "on" || binding["name"] != "ping" || binding["script"] != float64(1) {
		t.Fatalf("binding metadata = %#v", bindings[0])
	}
	if binding["id"] != float64(1) || binding["keyword"] != "on" || binding["environment"] != "ordinary" ||
		binding["lifetime"] != "persistent" || binding["predicate"] != false {
		t.Fatalf("binding identity metadata = %#v", bindings[0])
	}
	span, ok := binding["span"].(map[string]any)
	if !ok || span["source"] != path {
		t.Fatalf("binding span metadata = %#v", binding["span"])
	}
	selectors, ok := binding["selectors"].([]any)
	if !ok || len(selectors) != 1 {
		t.Fatalf("binding selector metadata = %#v", binding["selectors"])
	}
	assertServeJSON(t, responses[4], map[string]any{"id": float64(5), "result": nil})
	assertServeJSON(t, responses[5], map[string]any{
		"id": float64(6), "result": map[string]any{"$opfor": "binary", "base64": "AP8="},
	})
	assertServeJSON(t, responses[6], map[string]any{"id": float64(7), "result": "AP8="})
	assertServeJSON(t, responses[7], map[string]any{
		"id": float64(8), "result": []any{"&direct", "left", "right"},
	})
	assertServeJSON(t, responses[8], map[string]any{
		"id": float64(9), "result": []any{"inspect \"two words\" \"\" tail", "two words", "", "tail"},
	})
	assertServeJSON(t, responses[9], map[string]any{
		"id": float64(10), "result": []any{"console_alias \"two words\" tail", float64(42), "two words", "tail"},
	})
	assertServeJSON(t, responses[10], map[string]any{"id": float64(11), "result": "first"})
	assertServeJSON(t, responses[11], map[string]any{"id": float64(12), "result": "bye"})
}

func TestServeDispatchesEveryPopupLayerAndReportsBindingLifetime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "popup-layers.cna")
	source := `
popup layered { return "first:" . $1; }
popup layered { return "second:" . $1; }
when next_ping { return "once"; }
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"id":"popup","method":"popup","name":"layered","args":["value"]}`,
		`{"id":"missing","method":"popup","name":"missing"}`,
		`{"id":"popup-bindings","method":"bindings","kind":"popup","name":"layered"}`,
		`{"id":"once-binding","method":"bindings","kind":"on","name":"next_ping"}`,
		`{"id":"done","method":"shutdown"}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(context.Background(), Options{
		Stdin:  strings.NewReader(input),
		Stdout: stdout,
		Stderr: stderr,
	}, []string{"serve", path})
	if status != 0 {
		t.Fatalf("Execute status = %d, stderr = %q", status, stderr.String())
	}

	responses := decodeServeResponses(t, stdout.String())
	if len(responses) != 5 {
		t.Fatalf("responses = %#v, want 5 entries", responses)
	}
	assertServeJSON(t, responses[0], map[string]any{
		"id": "popup", "result": []any{"first:value", "second:value"},
	})
	assertServeJSON(t, responses[1], map[string]any{"id": "missing", "result": []any{}})

	popupBindings, ok := responses[2]["result"].([]any)
	if !ok || len(popupBindings) != 2 {
		t.Fatalf("popup bindings response = %#v", responses[2])
	}
	for _, value := range popupBindings {
		binding, ok := value.(map[string]any)
		if !ok || binding["kind"] != "popup" || binding["name"] != "layered" || binding["lifetime"] != "persistent" {
			t.Fatalf("popup binding metadata = %#v", value)
		}
	}
	onceBindings, ok := responses[3]["result"].([]any)
	if !ok || len(onceBindings) != 1 {
		t.Fatalf("once binding response = %#v", responses[3])
	}
	once, ok := onceBindings[0].(map[string]any)
	if !ok || once["kind"] != "on" || once["keyword"] != "when" || once["lifetime"] != "once" {
		t.Fatalf("once binding metadata = %#v", onceBindings[0])
	}
	assertServeJSON(t, responses[4], map[string]any{"id": "done", "result": "bye"})
}

func TestServeReportsBadRequestsAndContinues(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.cna")
	if err := os.WriteFile(path, []byte("# persistent runtime\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`not-json`,
		`{"id":"unknown","method":"missing"}`,
		`{"id":"bindings","method":"bindings"}`,
		`{"id":"done","method":"shutdown"}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(context.Background(), Options{
		Stdin:  strings.NewReader(input),
		Stdout: stdout,
		Stderr: stderr,
	}, []string{"serve", path})
	if status != 0 {
		t.Fatalf("Execute status = %d, stderr = %q", status, stderr.String())
	}
	responses := decodeServeResponses(t, stdout.String())
	if len(responses) != 4 {
		t.Fatalf("responses = %#v, want 4 entries", responses)
	}
	if got, ok := responses[0]["error"].(string); !ok || !strings.Contains(got, "invalid JSON request") {
		t.Fatalf("malformed response = %#v", responses[0])
	}
	if got, ok := responses[1]["error"].(string); !ok || !strings.Contains(got, `unknown request method "missing"`) {
		t.Fatalf("unknown-method response = %#v", responses[1])
	}
	if got, ok := responses[2]["error"].(string); !ok || got != "binding kind is required" {
		t.Fatalf("bindings error response = %#v", responses[2])
	}
	assertServeJSON(t, responses[3], map[string]any{"id": "done", "result": "bye"})
}

func TestServeFireReadyAndRejectsStdinScript(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ready.cna")
	if err := os.WriteFile(path, []byte(`on ready { println("ready-fired"); }`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(context.Background(), Options{
		Stdin:  strings.NewReader("{\"method\":\"shutdown\"}\n"),
		Stdout: stdout,
		Stderr: stderr,
	}, []string{"serve", "--fire-ready", path})
	if status != 0 || stderr.String() != "ready-fired\n" {
		t.Fatalf("serve --fire-ready = status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	status = Execute(context.Background(), Options{
		Stdin:  strings.NewReader("println('not a protocol');"),
		Stdout: stdout,
		Stderr: stderr,
	}, []string{"serve", "-"})
	if status != 1 || !strings.Contains(stderr.String(), "standard input is reserved for protocol requests") {
		t.Fatalf("serve stdin-script = status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
	}
}

func TestRunServeCancellationDoesNotWaitForInput(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	runtime, err := opfor.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	done := make(chan error, 1)
	go func() {
		done <- runServe(ctx, newServeSession(runtime, inertDependencies()), reader, io.Discard)
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runServe error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runServe did not return after context cancellation")
	}
}

func decodeServeResponses(t *testing.T, output string) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var responses []map[string]any
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode response %q: %v", output, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func assertServeJSON(t *testing.T, got, want map[string]any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("response = %s, want %s", gotJSON, wantJSON)
	}
}
