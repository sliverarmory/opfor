package opfor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestOfficialMkImportExampleParsesInertCredentialFile(t *testing.T) {
	t.Parallel()

	const ntlm = "0123456789abcdef0123456789abcdef"
	files := newOfficialMkImportFiles([]string{
		"this malformed line is ignored",
		"        * Username : alice ",
		"        * Password : S3cret! ",
		"        * Domain   : ACME ",
		"        * Username : WORKSTATION$ ",
		"        * Password : machine-secret ",
		"        * Domain   : LAB ",
		"        * Username : carol ",
		"        * Password : (null) ",
		"        * Domain   : DEV ",
		"        * Username : dave ",
		"this malformed credential field is ignored too",
		"        * Domain   : OPS ",
		"        * Username : bob ",
		"        * NTLM     : " + ntlm + " ",
		"        * Domain   : LAB ",
		"        * Username : (null) ",
		"        * Password : ignored ",
		"        * Domain   : VOID ",
	})
	host := &officialBehaviorHost{}
	objects := &officialMkImportObjectHost{}
	runtime, _, output := loadOfficialAdapterExample(
		t,
		"mkimport.cna",
		host,
		objects,
		WithFunction("openf", files.openf),
		WithFunction("readln", files.readln),
		WithFunction("closef", files.closef),
	)
	assertOfficialBehaviorCalls(t, host.takeCalls())

	if _, err := runtime.InvokeConsole(context.Background(), ConsoleInvocation{
		Kind: BindingCommand, Name: "importcreds", RawInput: `importcreds "/fixture/mimikatz output.txt"`,
	}); err != nil {
		t.Fatalf("InvokeConsole(importcreds): %v", err)
	}

	wantOutput := "" +
		"ADD alice (ACME): 'S3cret!' and ''\n" +
		"WORKSTATION$ () rejected because computer account\n" +
		"carol () rejected because empty hash/password fields\n" +
		"dave () rejected because empty hash/password fields\n" +
		"ADD bob (LAB): '' and '" + ntlm + "'\n"
	if got := output.String(); got != wantOutput {
		t.Fatalf("stdout mismatch\n got: %q\nwant: %q", got, wantOutput)
	}
	assertOfficialBehaviorCalls(t, host.takeCalls(),
		expectedOfficialCall("credential_add", "alice", "S3cret!", "ACME", "mimikatz-imported", ""),
		expectedOfficialCall("credential_add", "bob", ntlm, "LAB", "mimikatz-imported", ""),
	)
	objectCalls := objects.takeCalls()
	if len(objectCalls) != 5 {
		// endsWith('$') is routed through the ObjectHost first and then handled by
		// OPFOR's portable String fallback once for each username that reaches
		// the computer-account check.
		t.Fatalf("String object call count = %d, want 5", len(objectCalls))
	}
	for index, username := range []string{"alice", "WORKSTATION$", "carol", "dave", "bob"} {
		assertOfficialMouseObjectCall(t, objectCalls[index], ObjectInvoke, String(username), "", "endsWith", String("$"))
	}

	fileCalls, closeArgument, handleClosed := files.snapshot()
	wantFileCalls := make([]officialExpectedCall, 0, len(files.lines)+3)
	wantFileCalls = append(wantFileCalls, expectedOfficialCall("openf", "/fixture/mimikatz output.txt"))
	for range files.lines {
		wantFileCalls = append(wantFileCalls, expectedOfficialCall("readln", files.handle))
	}
	// The while condition performs one final EOF read.
	wantFileCalls = append(wantFileCalls, expectedOfficialCall("readln", files.handle))
	// The pinned source calls closef($temp), not closef($handle). At EOF $temp
	// is null, so the actual in-memory handle must remain open.
	wantFileCalls = append(wantFileCalls, expectedOfficialCall("closef", officialNullArgument{}))
	assertOfficialBehaviorCalls(t, fileCalls, wantFileCalls...)
	if !closeArgument.IsNull() {
		t.Fatalf("closef argument = %s, want EOF null from $temp", closeArgument.Describe())
	}
	if handleClosed {
		t.Fatal("in-memory handle was closed, but source passes $temp instead of $handle")
	}
}

func TestOfficialRandomStringExampleUsesDeterministicRandOverride(t *testing.T) {
	t.Parallel()

	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890"
	indices := []int32{0, 25, 26, 51, 52, 61, 62, 1, 24, 27, 50, 53, 60, 2, 23, 28, 49, 54, 59, 3}
	random := &officialRandomSequence{indices: append([]int32(nil), indices...)}
	host := &officialBehaviorHost{}
	objects := &officialBehaviorObjectHost{}
	_, _, output := loadOfficialAdapterExample(
		t,
		"random_string.cna",
		host,
		objects,
		WithFunction("rand", random.rand),
	)
	assertOfficialBehaviorCalls(t, host.takeCalls())
	if got := objects.takeCalls(); len(got) != 0 {
		t.Fatalf("object call count = %d, want 0", len(got))
	}

	var expected strings.Builder
	for _, index := range indices {
		expected.WriteByte(alphabet[index])
	}
	wantRandom := expected.String()
	wantOutput := "" +
		"-------------------------\n" +
		"\x034Print Random AlphaNum\n" +
		"Random 20: " + wantRandom + "\n" +
		"-------------------------\n"
	if got := output.String(); got != wantOutput {
		t.Fatalf("stdout mismatch\n got: %q\nwant: %q", got, wantOutput)
	}

	generated := strings.TrimSuffix(strings.TrimPrefix(output.String(),
		"-------------------------\n\x034Print Random AlphaNum\nRandom 20: "),
		"\n-------------------------\n")
	if len(generated) != 20 {
		t.Fatalf("generated length = %d, want 20", len(generated))
	}
	if generated != wantRandom {
		t.Fatalf("generated sequence = %q, want %q", generated, wantRandom)
	}
	for index, character := range generated {
		if !strings.ContainsRune(alphabet, character) {
			t.Fatalf("generated byte %d = %q, outside source alphabet", index, character)
		}
	}
	bounds, consumed := random.snapshot()
	if consumed != len(indices) {
		t.Fatalf("rand call count = %d, want %d", consumed, len(indices))
	}
	if len(bounds) != len(indices) {
		t.Fatalf("recorded rand bounds = %d, want %d", len(bounds), len(indices))
	}
	for index, bound := range bounds {
		if bound != int32(len(alphabet)) {
			t.Fatalf("rand call %d bound = %d, want alphabet length %d", index, bound, len(alphabet))
		}
	}
}

type officialMkImportFile struct{}

func (*officialMkImportFile) SleepDescribe() string { return "<inert-mimikatz-file>" }

type officialMkImportObjectHost struct {
	mu    sync.Mutex
	calls []ObjectInvocation
}

func (host *officialMkImportObjectHost) Object(_ context.Context, invocation ObjectInvocation) (Value, error) {
	host.mu.Lock()
	host.calls = append(host.calls, snapshotOfficialObjectInvocation(invocation))
	host.mu.Unlock()
	return Null(), &UnsupportedError{Operation: "test object operation", Name: invocation.Message, Span: invocation.Span}
}

func (host *officialMkImportObjectHost) takeCalls() []ObjectInvocation {
	host.mu.Lock()
	defer host.mu.Unlock()
	calls := append([]ObjectInvocation(nil), host.calls...)
	host.calls = nil
	return calls
}

type officialMkImportFiles struct {
	mu            sync.Mutex
	handle        Value
	lines         []string
	index         int
	calls         []officialBehaviorCall
	closeArgument Value
	handleClosed  bool
}

func newOfficialMkImportFiles(lines []string) *officialMkImportFiles {
	return &officialMkImportFiles{
		handle: ObjectValue(&officialMkImportFile{}),
		lines:  append([]string(nil), lines...),
	}
}

func (files *officialMkImportFiles) openf(_ context.Context, invocation Invocation) (Value, error) {
	values := invocation.Values()
	if len(values) != 1 || values[0].String() != "/fixture/mimikatz output.txt" {
		return Null(), fmt.Errorf("inert openf arguments = %s", ArrayValue(NewArray(values...)).Describe())
	}
	files.mu.Lock()
	files.calls = append(files.calls, officialBehaviorCall{name: invocation.Name, values: append([]Value(nil), values...)})
	files.mu.Unlock()
	return files.handle, nil
}

func (files *officialMkImportFiles) readln(_ context.Context, invocation Invocation) (Value, error) {
	values := invocation.Values()
	if len(values) != 1 || !values[0].IdentityEqual(files.handle) {
		return Null(), fmt.Errorf("inert readln arguments = %s", ArrayValue(NewArray(values...)).Describe())
	}
	files.mu.Lock()
	defer files.mu.Unlock()
	files.calls = append(files.calls, officialBehaviorCall{name: invocation.Name, values: append([]Value(nil), values...)})
	if files.index >= len(files.lines) {
		return Null(), nil
	}
	line := files.lines[files.index]
	files.index++
	return String(line), nil
}

func (files *officialMkImportFiles) closef(_ context.Context, invocation Invocation) (Value, error) {
	values := invocation.Values()
	if len(values) != 1 {
		return Null(), fmt.Errorf("inert closef arguments = %s", ArrayValue(NewArray(values...)).Describe())
	}
	files.mu.Lock()
	files.calls = append(files.calls, officialBehaviorCall{name: invocation.Name, values: append([]Value(nil), values...)})
	files.closeArgument = values[0]
	if values[0].IdentityEqual(files.handle) {
		files.handleClosed = true
	}
	files.mu.Unlock()
	return Null(), nil
}

func (files *officialMkImportFiles) snapshot() ([]officialBehaviorCall, Value, bool) {
	files.mu.Lock()
	defer files.mu.Unlock()
	return append([]officialBehaviorCall(nil), files.calls...), files.closeArgument, files.handleClosed
}

type officialRandomSequence struct {
	mu      sync.Mutex
	indices []int32
	bounds  []int32
	index   int
}

func (random *officialRandomSequence) rand(_ context.Context, invocation Invocation) (Value, error) {
	values := invocation.Values()
	if len(values) != 1 {
		return Null(), fmt.Errorf("deterministic rand received %d arguments, want 1", len(values))
	}
	random.mu.Lock()
	defer random.mu.Unlock()
	random.bounds = append(random.bounds, values[0].Int32())
	if random.index >= len(random.indices) {
		return Null(), fmt.Errorf("deterministic rand exhausted after %d calls", random.index)
	}
	value := random.indices[random.index]
	random.index++
	return Int(value), nil
}

func (random *officialRandomSequence) snapshot() ([]int32, int) {
	random.mu.Lock()
	defer random.mu.Unlock()
	return append([]int32(nil), random.bounds...), random.index
}
