package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAggressorTeamServerRPCProvider struct {
	mu       sync.Mutex
	requests []AggressorTeamServerRPCRequest
	call     func(context.Context, AggressorTeamServerRPCRequest) error
}

func (provider *recordingAggressorTeamServerRPCProvider) CallAggressorTeamServerRPC(
	ctx context.Context,
	request AggressorTeamServerRPCRequest,
) error {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	call := provider.call
	provider.mu.Unlock()
	if call == nil {
		return nil
	}
	return call(ctx, request)
}

func (provider *recordingAggressorTeamServerRPCProvider) snapshot() []AggressorTeamServerRPCRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorTeamServerRPCRequest(nil), provider.requests...)
}

func TestAggressorTeamServerRPCFunctionSetAndMinimumArity(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorTeamServerRPCFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{"call"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor Team Server RPC names = %q, want %q", names, want)
	}
	if aggressorTeamServerRPCMinimumArguments != 3 {
		t.Fatalf("call minimum arguments = %d, want 3", aggressorTeamServerRPCMinimumArguments)
	}
}

func TestAggressorTeamServerRPCResolvedRequestAndUnboundedArguments(t *testing.T) {
	t.Parallel()

	command := BinaryString([]byte{0x00, 0xff, 'C'})
	first := ArrayValue(NewArray(String("first")))
	last := HashValue(NewHash())
	commandCell := NewCell(command)
	firstCell := NewCell(first)
	lastCell := NewCell(last)
	span := Span{Source: "team-server-rpc-values.cna", Start: Position{Line: 17, Column: 5}}

	var captured AggressorTeamServerRPCRequest
	provider := AggressorTeamServerRPCProviderFunc(func(
		_ context.Context,
		request AggressorTeamServerRPCRequest,
	) error {
		captured = request
		commandCell.Set(String("command-mutated-after-resolution"))
		firstCell.Set(String("first-mutated-after-resolution"))
		lastCell.Set(String("last-mutated-after-resolution"))
		return nil
	})
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed call request reached Host")
		})),
		WithAggressorTeamServerRPCProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	arguments := []Argument{
		{Name: "$command", Reference: commandCell},
		{Value: Null()},
		{Name: "$first", Reference: firstCell},
	}
	for index := 0; index < 125; index++ {
		arguments = append(arguments, Argument{Value: Int(int32(index))})
	}
	arguments = append(arguments, Argument{Name: "$last", Reference: lastCell})
	invocation := Invocation{
		Runtime:   runtimeInstance,
		Script:    91,
		Name:      "call",
		Arguments: arguments,
		Span:      span,
	}
	result, err := runtimeInstance.aggressorTeamServerRPC(context.Background(), invocation)
	if err != nil || !result.IsNull() {
		t.Fatalf("call = (%s, %v), want null/nil", result.Describe(), err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("configured provider route reached Host %d time(s)", hostCalls.Load())
	}
	if captured.Name != "call" || captured.RuntimeID != runtimeInstance.ID() || captured.RuntimeID == 0 ||
		captured.Script != 91 || captured.Span != span {
		t.Fatalf("request route/provenance = %#v", captured)
	}
	if !captured.Command.IdentityEqual(command) || len(captured.Arguments) != len(arguments)-2 ||
		!captured.Arguments[0].IdentityEqual(first) ||
		!captured.Arguments[len(captured.Arguments)-1].IdentityEqual(last) {
		t.Fatalf("request values = command %s arguments %d/%s/%s",
			captured.Command.Describe(), len(captured.Arguments),
			captured.Arguments[0].Describe(), captured.Arguments[len(captured.Arguments)-1].Describe())
	}
	if captured.Callback.Valid() {
		t.Fatal("explicit null callback produced a valid capability")
	}
	if got, callbackErr := captured.Callback.Respond(context.Background(), String("ignored")); !errors.Is(callbackErr, ErrInvalidCallable) || !got.IsNull() {
		t.Fatalf("zero callback Respond = (%s, %v), want null/ErrInvalidCallable", got.Describe(), callbackErr)
	}

	// The request owns its top-level payload slice. Replacing an entry cannot
	// mutate the original Invocation or its source Cell.
	captured.Arguments[0] = String("provider-local-slice-mutation")
	if firstCell.Get().String() != "first-mutated-after-resolution" ||
		invocation.Arguments[2].Reference != firstCell {
		t.Fatalf("request Arguments slice retained Invocation backing state: %#v", invocation.Arguments[2])
	}
}

func TestAggressorTeamServerRPCCallbackABIIsMultiShotAndLifecycleBound(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorTeamServerRPCProvider{}
	runtimeInstance, err := New(WithAggressorTeamServerRPCProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("team-server-rpc-callback.cna", `
$calls = 0;
$seen_command = $null;
$seen_response = $null;
$ready = {
    $calls++;
    $seen_command = $1;
    $seen_response = $2;
    return $2;
};
sub issue_rpc {
    return call($1, $ready, $2);
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	command := ArrayValue(NewArray(String("beacons.task")))
	payload := HashValue(NewHash())
	result, err := owner.Call(context.Background(), "issue_rpc", command, payload)
	if err != nil || !result.IsNull() {
		t.Fatalf("call = (%s, %v), want null/nil", result.Describe(), err)
	}
	requests := provider.snapshot()
	if len(requests) != 1 {
		t.Fatalf("provider requests = %d, want one", len(requests))
	}
	request := requests[0]
	if request.Name != "call" || request.RuntimeID != runtimeInstance.ID() || request.Script != owner.ID() ||
		request.Span.Source != "team-server-rpc-callback.cna" || request.Span.Start.Line == 0 ||
		!request.Command.IdentityEqual(command) || len(request.Arguments) != 1 ||
		!request.Arguments[0].IdentityEqual(payload) || !request.Callback.Valid() {
		t.Fatalf("callback request = %#v", request)
	}

	for index := 1; index <= 2; index++ {
		response := HashValue(NewHash())
		callbackResult, callbackErr := request.Callback.Respond(context.Background(), response)
		if callbackErr != nil || !callbackResult.IdentityEqual(response) {
			t.Fatalf("Respond %d = (%s, %v), want identical response", index, callbackResult.Describe(), callbackErr)
		}
		if !owner.Get("$seen_command").IdentityEqual(command) || !owner.Get("$seen_response").IdentityEqual(response) {
			t.Fatalf("Respond %d ABI values = %s/%s, want original command/current response",
				index, owner.Get("$seen_command").Describe(), owner.Get("$seen_response").Describe())
		}
	}
	if calls := owner.Get("$calls").Int32(); calls != 2 {
		t.Fatalf("callback calls = %d, want 2", calls)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	callbackResult, callbackErr := request.Callback.Respond(canceled, String("must-not-run"))
	if !errors.Is(callbackErr, context.Canceled) || !callbackResult.IsNull() || owner.Get("$calls").Int32() != 2 {
		t.Fatalf("canceled Respond = (%s, %v), calls %d", callbackResult.Describe(), callbackErr, owner.Get("$calls").Int32())
	}
	if err := owner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	callbackResult, callbackErr = request.Callback.Respond(context.Background(), String("after unload"))
	if !errors.Is(callbackErr, ErrScriptUnloaded) || !callbackResult.IsNull() {
		t.Fatalf("Respond after unload = (%s, %v), want null/ErrScriptUnloaded", callbackResult.Describe(), callbackErr)
	}
}

func TestAggressorTeamServerRPCCallbackConcurrentResponsesAndRuntimeClose(t *testing.T) {
	provider := &recordingAggressorTeamServerRPCProvider{}
	runtimeInstance, err := New(WithAggressorTeamServerRPCProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("team-server-rpc-concurrent.cna", `
$ready = { return $2; };
call("concurrent.command", $ready, "payload");
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	requests := provider.snapshot()
	if len(requests) != 1 || !requests[0].Callback.Valid() {
		t.Fatalf("provider requests = %#v, want one valid callback", requests)
	}
	callback := requests[0].Callback

	const responders = 32
	start := make(chan struct{})
	errorsByIndex := make([]error, responders)
	var wait sync.WaitGroup
	for index := 0; index < responders; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response := ArrayValue(NewArray(Int(int32(index))))
			result, responseErr := callback.Respond(context.Background(), response)
			if responseErr != nil {
				errorsByIndex[index] = responseErr
				return
			}
			if !result.IdentityEqual(response) {
				errorsByIndex[index] = fmt.Errorf("result %s did not retain response identity", result.Describe())
			}
		}()
	}
	close(start)
	wait.Wait()
	for index, responseErr := range errorsByIndex {
		if responseErr != nil {
			t.Fatalf("concurrent Respond %d: %v", index, responseErr)
		}
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner.Active() {
		t.Fatal("owner remained active after Runtime.Close")
	}
	result, err := callback.Respond(context.Background(), String("after close"))
	if !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("Respond after Runtime.Close = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
}

func TestAggressorTeamServerRPCExplicitNullAndNonCallablePolicy(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	var captured AggressorTeamServerRPCRequest
	runtimeInstance, err := New(WithAggressorTeamServerRPCProvider(AggressorTeamServerRPCProviderFunc(func(
		_ context.Context,
		request AggressorTeamServerRPCRequest,
	) error {
		providerCalls.Add(1)
		captured = request
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "call", String("null.callback"), Null(), String("argument"))
	if err != nil || !result.IsNull() || providerCalls.Load() != 1 || captured.Callback.Valid() {
		t.Fatalf("null callback call = (%s, %v), provider calls %d callback valid %v",
			result.Describe(), err, providerCalls.Load(), captured.Callback.Valid())
	}
	result, err = runtimeInstance.Invoke(context.Background(), "call", String("bad.callback"), String("not callable"), String("argument"))
	if !errors.Is(err, ErrInvalidCallable) || !result.IsNull() || providerCalls.Load() != 1 {
		t.Fatalf("non-callable call = (%s, %v), provider calls %d; want null/ErrInvalidCallable/1",
			result.Describe(), err, providerCalls.Load())
	}
}

func TestAggressorTeamServerRPCArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorTeamServerRPCProvider(AggressorTeamServerRPCProviderFunc(func(
			context.Context,
			AggressorTeamServerRPCRequest,
		) error {
			providerCalls.Add(1)
			return nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for count := 0; count < aggressorTeamServerRPCMinimumArguments; count++ {
		arguments := make([]Value, count)
		for index := range arguments {
			arguments[index] = Null()
		}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), "call", arguments...)
		if invokeErr == nil || !result.IsNull() ||
			!strings.Contains(invokeErr.Error(), "expected at least 3 argument(s)") {
			t.Errorf("call/%d = (%s, %v), want null/minimum arity error", count, result.Describe(), invokeErr)
		}
	}
	if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid arities reached provider/Host %d/%d time(s)", providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorTeamServerRPCUnsetProviderPreservesExactHostInvocationOnce(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Host call result")
	wantResult := ArrayValue(NewArray(String("Host result")))
	commandCell := NewCell(String("command-before"))
	callbackCell := NewCell(String("Host-owned callback shape"))
	payloadCell := NewCell(BinaryString([]byte{0x00, 0xff}))
	span := Span{Source: "team-server-rpc-host.cna", Start: Position{Line: 11, Column: 3}}
	original := Invocation{
		Script: 74,
		Name:   "call",
		Span:   span,
		Arguments: []Argument{
			{Name: "$command", Reference: commandCell},
			{Name: "&callback", Reference: callbackCell},
			{Name: "$payload", Reference: payloadCell},
		},
	}
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("command-mutated-by-Host"))
		invocation.Arguments[1].Set(String("callback-mutated-by-Host"))
		invocation.Arguments[2].Set(String("payload-mutated-by-Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original.Runtime = runtimeInstance

	result, err := runtimeInstance.aggressorTeamServerRPC(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != original.Script ||
		captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 3 {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
	}
	if captured.Arguments[0].Reference != commandCell || captured.Arguments[1].Reference != callbackCell ||
		captured.Arguments[2].Reference != payloadCell || captured.Arguments[1].Name != "&callback" {
		t.Fatalf("Host did not receive exact reference-bearing arguments: %#v", captured.Arguments)
	}
	if commandCell.Get().String() != "command-mutated-by-Host" ||
		callbackCell.Get().String() != "callback-mutated-by-Host" ||
		payloadCell.Get().String() != "payload-mutated-by-Host" {
		t.Fatal("Host reference mutations were not preserved")
	}
}

func TestAggressorTeamServerRPCProviderErrorsAndCancellationAreAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Team Server rejected call")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	var cancelDuring context.CancelFunc
	provider := AggressorTeamServerRPCProviderFunc(func(ctx context.Context, request AggressorTeamServerRPCRequest) error {
		providerCalls.Add(1)
		switch request.Command.String() {
		case "error":
			return wantErr
		case "cancel-during":
			cancelDuring()
			if !errors.Is(ctx.Err(), context.Canceled) {
				return fmt.Errorf("provider context error = %v", ctx.Err())
			}
		}
		return nil
	})
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorTeamServerRPCProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invoke := func(ctx context.Context, command string) (Value, error) {
		return runtimeInstance.Invoke(ctx, "call", String(command), Null(), String("argument"))
	}

	result, err := invoke(context.Background(), "error")
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("provider error = (%s, %v), want null/%v", result.Describe(), err, wantErr)
	}
	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = invoke(preCanceled, "pre-canceled")
	if !errors.Is(err, context.Canceled) || !result.IsNull() || providerCalls.Load() != 1 {
		t.Fatalf("pre-canceled = (%s, %v), provider calls %d", result.Describe(), err, providerCalls.Load())
	}
	during, cancel := context.WithCancel(context.Background())
	cancelDuring = cancel
	result, err = invoke(during, "cancel-during")
	if !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("cancel-during = (%s, %v), want null/context.Canceled", result.Describe(), err)
	}
	if providerCalls.Load() != 2 || hostCalls.Load() != 0 {
		t.Fatalf("provider/Host calls = %d/%d, want 2/0", providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorTeamServerRPCRuntimeCloseCancelsBlockingProvider(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorTeamServerRPCProvider(AggressorTeamServerRPCProviderFunc(func(
		ctx context.Context,
		_ AggressorTeamServerRPCRequest,
	) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}

	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(
			context.Background(), "call", String("blocking"), Null(), String("argument"),
		)
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("Team Server RPC provider did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtimeInstance.Close(context.Background()) }()
	select {
	case invokeErr := <-invokeDone:
		if !errors.Is(invokeErr, context.Canceled) {
			t.Errorf("blocking provider error = %v, want context.Canceled", invokeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocking Team Server RPC provider did not stop on Runtime.Close")
	}
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime.Close did not finish")
	}
}

func TestAggressorTeamServerRPCWithFunctionOverridesInBothOptionOrders(t *testing.T) {
	for _, overrideFirst := range []bool{false, true} {
		overrideFirst := overrideFirst
		t.Run(fmt.Sprintf("override-first=%v", overrideFirst), func(t *testing.T) {
			var providerCalls atomic.Int32
			var hostCalls atomic.Int32
			providerOption := WithAggressorTeamServerRPCProvider(AggressorTeamServerRPCProviderFunc(func(
				context.Context,
				AggressorTeamServerRPCRequest,
			) error {
				providerCalls.Add(1)
				return nil
			}))
			overrideOption := WithFunction("call", func(_ context.Context, invocation Invocation) (Value, error) {
				return String("override:" + invocation.Name), nil
			})
			hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
				hostCalls.Add(1)
				return Null(), nil
			}))
			options := []Option{hostOption, providerOption, overrideOption}
			if overrideFirst {
				options = []Option{hostOption, overrideOption, providerOption}
			}
			runtimeInstance, err := New(options...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			// Zero arguments is invalid for the stock wrapper. Success proves the
			// importer override won before native arity validation.
			result, err := runtimeInstance.Invoke(context.Background(), "call")
			if err != nil || result.String() != "override:call" {
				t.Fatalf("override = (%s, %v)", result.Describe(), err)
			}
			if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
				t.Fatalf("override provider/Host calls = %d/%d", providerCalls.Load(), hostCalls.Load())
			}
		})
	}
}

func TestPortableScriptLoaderInheritsAggressorTeamServerRPCProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-team-server-rpc.cna")
	if err := os.WriteFile(childPath, []byte(`call("child", $null, "child-argument");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-team-server-rpc.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
call("parent", $null, "parent-argument");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorTeamServerRPCProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader Team Server RPC route reached Host")
		})),
		WithAggressorTeamServerRPCProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited provider requests reached Host %d time(s)", hostCalls.Load())
	}
	requests := provider.snapshot()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want parent plus child", len(requests))
	}
	if requests[0].Command.String() != "parent" || requests[1].Command.String() != "child" ||
		len(requests[0].Arguments) != 1 || len(requests[1].Arguments) != 1 ||
		requests[0].Arguments[0].String() != "parent-argument" ||
		requests[1].Arguments[0].String() != "child-argument" ||
		requests[0].Callback.Valid() || requests[1].Callback.Valid() {
		t.Fatalf("parent/child Team Server RPC requests = %#v", requests)
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[0].RuntimeID == 0 ||
		requests[1].RuntimeID == 0 || requests[1].RuntimeID == requests[0].RuntimeID {
		t.Fatalf("parent/child RuntimeIDs = %d/%d", requests[0].RuntimeID, requests[1].RuntimeID)
	}
	if requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-team-server-rpc.cna" ||
		requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", requests)
	}
}
