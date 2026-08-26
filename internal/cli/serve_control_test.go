package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sliverarmory/opfor"
)

func TestDecodeServeTraceProfileRequests(t *testing.T) {
	request, err := decodeServeRequest([]byte(`{"id":"trace","method":" TRACE ","script":7,"flags":24}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != "trace" || !request.scriptSet || request.Script != 7 || !request.flagsSet || request.Flags == nil || *request.Flags != 24 {
		t.Fatalf("decoded trace request = %#v", request)
	}
	profile, err := decodeServeRequest([]byte(`{"method":"profile","script":9}`))
	if err != nil || profile.Method != "profile" || !profile.scriptSet || profile.Script != 9 || profile.flagsSet {
		t.Fatalf("decoded profile request = (%#v, %v)", profile, err)
	}
	nullFlags, err := decodeServeRequest([]byte(`{"method":"trace","script":1,"flags":null}`))
	if err != nil || !nullFlags.flagsSet || nullFlags.Flags != nil {
		t.Fatalf("decoded null flags = (%#v, %v)", nullFlags, err)
	}
	legacyExtra, err := decodeServeRequest([]byte(`{"method":"scripts","flags":"still ignored"}`))
	if err != nil || legacyExtra.Method != "scripts" || !legacyExtra.flagsSet || legacyExtra.Flags != nil {
		t.Fatalf("decoded ignored legacy extra field = (%#v, %v)", legacyExtra, err)
	}
	for _, source := range []string{
		`{"method":"trace","script":1,"flags":2147483648}`,
		`{"method":"trace","script":1,"flags":1.5}`,
		`{"method":"trace","script":1,"flags":"8"}`,
		`{"method":"profile","script":-1}`,
	} {
		if _, err := decodeServeRequest([]byte(source)); err == nil || !strings.Contains(err.Error(), "invalid JSON request") {
			t.Fatalf("decode %s error = %v", source, err)
		}
	}
}

func TestDispatchServeTraceProfileValidationAndSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.cna")
	source := []byte(`
sub work { return 7; }
sub exercise { work(); return 1; }
`)
	_, session := newTestServeSession(t, &bytes.Buffer{}, func(string) ([]byte, error) {
		return append([]byte(nil), source...), nil
	})
	if _, err := session.load(ctx, path, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	entry := onlyServeScript(t, session)
	id := uint64(entry.script.ID())

	result, stop, err := dispatchServeRequest(ctx, session, serveRequest{
		Method: "trace", Script: id, scriptSet: true,
	})
	if err != nil || stop {
		t.Fatalf("trace get = (%#v, stop %v, %v)", result, stop, err)
	}
	trace := result.(map[string]any)
	if trace["script"] != id || trace["flags"] != int32(1) {
		t.Fatalf("trace get = %#v", trace)
	}
	flags := int32(24)
	result, _, err = dispatchServeRequest(ctx, session, serveRequest{
		Method: "trace", Script: id, scriptSet: true, Flags: &flags, flagsSet: true,
	})
	if err != nil || result.(map[string]any)["flags"] != int32(24) {
		t.Fatalf("trace set = (%#v, %v)", result, err)
	}
	if _, err := entry.script.Call(ctx, "exercise"); err != nil {
		t.Fatal(err)
	}
	result, _, err = dispatchServeRequest(ctx, session, serveRequest{
		Method: "profile", Script: id, scriptSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	report := result.(opfor.ScriptProfileSnapshot)
	if report.Script != entry.script.ID() || len(report.Statistics) != 1 ||
		report.Statistics[0].FunctionName != "&work" || report.Statistics[0].Calls != 1 {
		t.Fatalf("profile = %#v", report)
	}

	for _, test := range []struct {
		name    string
		request serveRequest
		want    string
	}{
		{name: "trace missing script", request: serveRequest{Method: "trace"}, want: "trace requires script"},
		{name: "profile missing script", request: serveRequest{Method: "profile"}, want: "profile requires script"},
		{name: "zero script", request: serveRequest{Method: "trace", scriptSet: true}, want: "trace script must be a positive integer"},
		{name: "null flags", request: serveRequest{Method: "trace", Script: id, scriptSet: true, flagsSet: true}, want: "trace flags must be a 32-bit integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := dispatchServeRequest(ctx, session, test.request)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	_, _, err = dispatchServeRequest(ctx, session, serveRequest{
		Method: "profile", Script: id + 100, scriptSet: true,
	})
	if !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("unknown profile target error = %v, want ErrScriptUnloaded", err)
	}
}

func TestServeTraceProfileReloadUnloadIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "isolation.cna")
	_, session := newTestServeSession(t, &bytes.Buffer{}, func(string) ([]byte, error) {
		return []byte(`sub work { return 1; } sub exercise { return work(); }`), nil
	})
	if _, err := session.load(ctx, path, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	old := onlyServeScript(t, session)
	oldID := uint64(old.script.ID())
	flags := int32(24)
	if _, _, err := dispatchServeRequest(ctx, session, serveRequest{
		Method: "trace", Script: oldID, scriptSet: true, Flags: &flags, flagsSet: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := old.script.Call(ctx, "exercise"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.reload(ctx, serveRequest{Script: oldID}); err != nil {
		t.Fatal(err)
	}
	replacement := onlyServeScript(t, session)
	newID := uint64(replacement.script.ID())
	if newID == oldID {
		t.Fatal("reload reused the old script ID")
	}
	for _, method := range []string{"trace", "profile"} {
		_, _, err := dispatchServeRequest(ctx, session, serveRequest{Method: method, Script: oldID, scriptSet: true})
		if !errors.Is(err, opfor.ErrScriptUnloaded) {
			t.Fatalf("%s old ID error = %v", method, err)
		}
	}
	result, _, err := dispatchServeRequest(ctx, session, serveRequest{Method: "trace", Script: newID, scriptSet: true})
	if err != nil || result.(map[string]any)["flags"] != int32(1) {
		t.Fatalf("replacement trace = (%#v, %v), want default flags", result, err)
	}
	result, _, err = dispatchServeRequest(ctx, session, serveRequest{Method: "profile", Script: newID, scriptSet: true})
	if err != nil || len(result.(opfor.ScriptProfileSnapshot).Statistics) != 0 {
		t.Fatalf("replacement profile = (%#v, %v), want empty", result, err)
	}
	if _, _, err := session.unload(ctx, serveRequest{Script: newID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dispatchServeRequest(ctx, session, serveRequest{Method: "trace", Script: newID, scriptSet: true}); !errors.Is(err, opfor.ErrScriptUnloaded) {
		t.Fatalf("unloaded replacement trace error = %v", err)
	}
}

func TestDispatchServeTraceProfileConcurrent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent.cna")
	_, session := newTestServeSession(t, &bytes.Buffer{}, func(string) ([]byte, error) {
		return []byte(`sub work { return 1; } sub exercise { return work(); }`), nil
	})
	if _, err := session.load(ctx, path, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	entry := onlyServeScript(t, session)
	id := uint64(entry.script.ID())

	const workers = 12
	const iterations = 80
	var wait sync.WaitGroup
	errorsSeen := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				flags := int32(24 | ((worker + iteration) & 1))
				if _, _, err := dispatchServeRequest(ctx, session, serveRequest{
					Method: "trace", Script: id, scriptSet: true, Flags: &flags, flagsSet: true,
				}); err != nil {
					errorsSeen <- err
					return
				}
				if _, err := entry.script.Call(ctx, "exercise"); err != nil {
					errorsSeen <- err
					return
				}
				if _, _, err := dispatchServeRequest(ctx, session, serveRequest{
					Method: "profile", Script: id, scriptSet: true,
				}); err != nil {
					errorsSeen <- err
					return
				}
			}
		}(worker)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
}

func TestServeTraceProfileJSONLinesE2E(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.cna")
	if err := os.WriteFile(path, []byte(`sub work { return 7; } sub exercise { work(); return 1; }`), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"id":"get","method":"trace","script":1}`,
		`{"id":"set","method":"trace","script":1,"flags":24}`,
		`{"id":"call","method":"call","script":1,"name":"exercise"}`,
		`{"id":"profile","method":"profile","script":1}`,
		`{"id":"done","method":"shutdown"}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := Execute(context.Background(), Options{
		Stdin: strings.NewReader(input), Stdout: stdout, Stderr: stderr,
	}, []string{"serve", path})
	if status != 0 {
		t.Fatalf("serve status = %d, stderr = %q", status, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("suppressed trace wrote stderr %q", stderr.String())
	}
	responses := decodeServeResponses(t, stdout.String())
	if len(responses) != 5 {
		t.Fatalf("responses = %#v", responses)
	}
	assertServeJSON(t, responses[0], map[string]any{
		"id": "get", "result": map[string]any{"script": float64(1), "flags": float64(1)},
	})
	assertServeJSON(t, responses[1], map[string]any{
		"id": "set", "result": map[string]any{"script": float64(1), "flags": float64(24)},
	})
	assertServeResult(t, responses[2], float64(1))
	profile, ok := responses[3]["result"].(map[string]any)
	if !ok || profile["script"] != float64(1) || len(profile) != 2 {
		t.Fatalf("profile response = %#v", responses[3])
	}
	statistics, ok := profile["statistics"].([]any)
	if !ok || len(statistics) != 1 {
		t.Fatalf("profile statistics = %#v", profile["statistics"])
	}
	statistic, ok := statistics[0].(map[string]any)
	if !ok || len(statistic) != 3 || statistic["function_name"] != "&work" || statistic["calls"] != float64(1) {
		t.Fatalf("profile statistic = %#v", statistics[0])
	}
	if _, ok := statistic["ticks"].(float64); !ok {
		t.Fatalf("profile ticks = %#v", statistic["ticks"])
	}
	assertServeResult(t, responses[4], "bye")
}
