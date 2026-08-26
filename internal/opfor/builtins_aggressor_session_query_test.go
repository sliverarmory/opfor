package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingAggressorSessionQueryProvider struct {
	mu      sync.Mutex
	queries []AggressorSessionQuery
	query   func(context.Context, AggressorSessionQuery) (Value, error)
}

func (provider *recordingAggressorSessionQueryProvider) QueryAggressorSession(
	ctx context.Context,
	query AggressorSessionQuery,
) (Value, error) {
	provider.mu.Lock()
	provider.queries = append(provider.queries, query)
	function := provider.query
	provider.mu.Unlock()
	if function == nil {
		return Null(), nil
	}
	return function(ctx, query)
}

func (provider *recordingAggressorSessionQueryProvider) snapshot() []AggressorSessionQuery {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorSessionQuery(nil), provider.queries...)
}

func TestAggressorSessionQueryFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorSessionQueryFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{
		"-is64",
		"-isactive",
		"-isadmin",
		"-isbeacon",
		"-isssh",
		"barch",
		"bdata",
		"beacon_data",
		"beacon_ids",
		"beacon_info",
		"beacons",
		"binfo",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor session query names = %q, want %q", names, want)
	}
}

func TestAggressorSessionQueryCanonicalKindsAliasesAndResults(t *testing.T) {
	t.Parallel()

	beacons := ArrayValue(NewArray(String("A"), String("B")))
	beaconIDs := ArrayValue(NewArray(String("A"), String("B")))
	beaconData := HashValue(NewHash())
	aliasBeaconData := HashValue(NewHash())
	tests := []struct {
		name       string
		kind       AggressorSessionQueryKind
		arguments  []Value
		provided   Value
		wantTruth  *bool
		wantResult Value
	}{
		{name: "beacons", kind: AggressorSessionQueryBeacons, provided: beacons, wantResult: beacons},
		{name: "beacon_ids", kind: AggressorSessionQueryBeaconIDs, provided: beaconIDs, wantResult: beaconIDs},
		{name: "bdata", kind: AggressorSessionQueryBeaconData, arguments: []Value{String("B-data")}, provided: beaconData, wantResult: beaconData},
		{name: "beacon_data", kind: AggressorSessionQueryBeaconData, arguments: []Value{String("B-alias")}, provided: aliasBeaconData, wantResult: aliasBeaconData},
		{name: "binfo", kind: AggressorSessionQueryBeaconInfo, arguments: []Value{String("B-info"), String("internal")}, provided: String("10.0.0.1"), wantResult: String("10.0.0.1")},
		{name: "beacon_info", kind: AggressorSessionQueryBeaconInfo, arguments: []Value{String("B-info-alias"), BinaryString([]byte("external"))}, provided: BinaryString([]byte("203.0.113.1")), wantResult: BinaryString([]byte("203.0.113.1"))},
		{name: "barch", kind: AggressorSessionQueryBeaconArchitecture, arguments: []Value{String("B-arch")}, provided: String("x64"), wantResult: String("x64")},
		{name: "-is64", kind: AggressorSessionQueryIs64, arguments: []Value{String("B-64")}, provided: String("truthy"), wantTruth: boolPointer(true)},
		{name: "-isactive", kind: AggressorSessionQueryIsActive, arguments: []Value{String("B-active")}, provided: String("0"), wantTruth: boolPointer(false)},
		{name: "-isadmin", kind: AggressorSessionQueryIsAdmin, arguments: []Value{String("B-admin")}, provided: Int(-1), wantTruth: boolPointer(true)},
		{name: "-isbeacon", kind: AggressorSessionQueryIsBeacon, arguments: []Value{String("B-beacon")}, provided: String(""), wantTruth: boolPointer(false)},
		{name: "-isssh", kind: AggressorSessionQueryIsSSH, arguments: []Value{String("B-ssh")}, provided: HashValue(NewHash()), wantTruth: boolPointer(true)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var hostCalls atomic.Int32
			provider := &recordingAggressorSessionQueryProvider{
				query: func(context.Context, AggressorSessionQuery) (Value, error) {
					return test.provided, nil
				},
			}
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("metadata provider route reached Host")
				})),
				WithAggressorSessionQueryProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantTruth != nil {
				if result.Kind() != Bool(*test.wantTruth).Kind() || result.Truth() != *test.wantTruth {
					t.Fatalf("predicate result = %s, want canonical Bool(%v)", result.Describe(), *test.wantTruth)
				}
			} else if !result.IdentityEqual(test.wantResult) {
				t.Fatalf("result = %s, want identical provider Value %s", result.Describe(), test.wantResult.Describe())
			}
			if hostCalls.Load() != 0 {
				t.Fatalf("configured provider route reached Host %d time(s)", hostCalls.Load())
			}
			queries := provider.snapshot()
			if len(queries) != 1 {
				t.Fatalf("provider queries = %d, want exactly one", len(queries))
			}
			query := queries[0]
			if query.Name != test.name || query.Kind != test.kind {
				t.Errorf("query route = %q/%q, want %q/%q", query.Name, query.Kind, test.name, test.kind)
			}
			if query.RuntimeID != runtimeInstance.ID() || query.RuntimeID == 0 || query.Script != 0 || query.Span != (Span{}) {
				t.Errorf("query provenance = runtime %d script %d span %s, want runtime %d and zero direct span", query.RuntimeID, query.Script, query.Span, runtimeInstance.ID())
			}
			if len(test.arguments) == 0 {
				if !query.SessionID.IsNull() || !query.Key.IsNull() {
					t.Errorf("argument-free query fields = %s/%s, want null/null", query.SessionID.Describe(), query.Key.Describe())
				}
				return
			}
			if !query.SessionID.IdentityEqual(test.arguments[0]) {
				t.Errorf("SessionID = %s, want identical %s", query.SessionID.Describe(), test.arguments[0].Describe())
			}
			if len(test.arguments) == 2 {
				if !query.Key.IdentityEqual(test.arguments[1]) {
					t.Errorf("Key = %s, want identical %s", query.Key.Describe(), test.arguments[1].Describe())
				}
			} else if !query.Key.IsNull() {
				t.Errorf("Key = %s, want null", query.Key.Describe())
			}
		})
	}
}

func TestAggressorSessionQueryExactArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	var hostCalls atomic.Int32
	provider := &recordingAggressorSessionQueryProvider{}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorSessionQueryProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for name, spec := range aggressorSessionQuerySpecs {
		for _, count := range []int{spec.arity - 1, spec.arity + 1} {
			if count < 0 {
				continue
			}
			arguments := make([]Value, count)
			for index := range arguments {
				arguments[index] = String(fmt.Sprintf("argument-%d", index))
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if invokeErr == nil || !result.IsNull() {
				t.Errorf("%s/%d = (%s, %v), want null exact-arity error", name, count, result.Describe(), invokeErr)
			}
		}
	}
	if got := len(provider.snapshot()); got != 0 {
		t.Fatalf("invalid arities reached provider %d time(s)", got)
	}
	if got := hostCalls.Load(); got != 0 {
		t.Fatalf("invalid arities reached Host %d time(s)", got)
	}
}

func TestAggressorSessionQueryBarchFallbackAndCompoundOwnership(t *testing.T) {
	t.Parallel()

	results := []Value{Null(), String(""), String("x64"), BinaryString([]byte("arm64"))}
	var index atomic.Int32
	returnedArray := NewArray(String("provider-owned"))
	provider := AggressorSessionQueryProviderFunc(func(_ context.Context, query AggressorSessionQuery) (Value, error) {
		if query.Name == "beacons" {
			return ArrayValue(returnedArray), nil
		}
		return results[int(index.Add(1))-1], nil
	})
	runtimeInstance, err := New(WithAggressorSessionQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for resultIndex, want := range []Value{String("x86"), String("x86"), String("x64"), BinaryString([]byte("arm64"))} {
		got, invokeErr := runtimeInstance.Invoke(context.Background(), "barch", String("B"))
		if invokeErr != nil || !got.IdentityEqual(want) {
			t.Errorf("barch result %d = (%s, %v), want %s", resultIndex, got.Describe(), invokeErr, want.Describe())
		}
	}
	compound, err := runtimeInstance.Invoke(context.Background(), "beacons")
	if err != nil || !compound.IdentityEqual(ArrayValue(returnedArray)) {
		t.Fatalf("compound result = (%s, %v), want transferred provider identity", compound.Describe(), err)
	}
	returnedArray.Append(String("script-visible"))
	if got := returnedArray.Len(); got != 2 {
		t.Fatalf("transferred compound length = %d, want 2", got)
	}
}

func TestAggressorSessionQueryBarchAndIs64RemainIndependentForWoW64(t *testing.T) {
	t.Parallel()

	sessionID := String("wow64-beacon")
	provider := &recordingAggressorSessionQueryProvider{
		query: func(_ context.Context, query AggressorSessionQuery) (Value, error) {
			if !query.SessionID.IdentityEqual(sessionID) {
				return Null(), fmt.Errorf("SessionID = %s, want %s", query.SessionID.Describe(), sessionID.Describe())
			}
			switch query.Kind {
			case AggressorSessionQueryBeaconArchitecture:
				return String("x86"), nil
			case AggressorSessionQueryIs64:
				return Bool(true), nil
			default:
				return Null(), fmt.Errorf("unexpected query kind %q", query.Kind)
			}
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("WoW64 query reached Host")
		})),
		WithAggressorSessionQueryProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	architecture, err := runtimeInstance.Invoke(context.Background(), "barch", sessionID)
	if err != nil || architecture.String() != "x86" {
		t.Fatalf("barch = (%s, %v), want x86", architecture.Describe(), err)
	}
	is64, err := runtimeInstance.Invoke(context.Background(), "-is64", sessionID)
	if err != nil || is64.Kind() != KindInt || !is64.Truth() {
		t.Fatalf("-is64 = (%s, %v), want canonical true", is64.Describe(), err)
	}
	queries := provider.snapshot()
	if len(queries) != 2 || queries[0].Kind != AggressorSessionQueryBeaconArchitecture || queries[1].Kind != AggressorSessionQueryIs64 {
		t.Fatalf("WoW64 query kinds = %#v", queries)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("WoW64 provider queries reached Host %d time(s)", hostCalls.Load())
	}
}

func TestAggressorSessionQueryResolvesReferencesOnceWithoutArrayFanout(t *testing.T) {
	t.Parallel()

	ids := NewArray(String("A"), String("B"))
	idCell := NewCell(ArrayValue(ids))
	keyCell := NewCell(String("internal"))
	providerCalls := 0
	var capturedQuery AggressorSessionQuery
	provider := AggressorSessionQueryProviderFunc(func(_ context.Context, query AggressorSessionQuery) (Value, error) {
		providerCalls++
		capturedQuery = query
		idCell.Set(String("replacement"))
		keyCell.Set(String("changed"))
		return query.SessionID, nil
	})
	runtimeInstance, err := New(WithAggressorSessionQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	span := Span{Source: "snapshot.cna", Start: Position{Line: 7, Column: 3}}
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  41,
		Name:    "binfo",
		Span:    span,
		Arguments: []Argument{
			{Reference: idCell},
			{Reference: keyCell},
		},
	}
	result, err := runtimeInstance.aggressorSessionQuery(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 {
		t.Fatalf("array SessionID provider calls = %d, want one", providerCalls)
	}
	if !result.IdentityEqual(ArrayValue(ids)) {
		t.Fatalf("result = %s, want original compound snapshot identity", result.Describe())
	}
	if !capturedQuery.SessionID.IdentityEqual(ArrayValue(ids)) || capturedQuery.Key.String() != "internal" {
		t.Fatalf("provider snapshots = %s/%s, want original array/internal", capturedQuery.SessionID.Describe(), capturedQuery.Key.Describe())
	}
	if capturedQuery.RuntimeID != runtimeInstance.ID() || capturedQuery.Script != 41 || capturedQuery.Span != span {
		t.Fatalf("provider provenance = runtime %d script %d span %s", capturedQuery.RuntimeID, capturedQuery.Script, capturedQuery.Span)
	}
}

func TestAggressorSessionQueryUnsetProviderPreservesHostInvocationResultAndError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("host metadata result")
	wantResult := HashValue(NewHash())
	idCell := NewCell(String("before"))
	keyCell := NewCell(String("key"))
	span := Span{Source: "host-fallback.cna", Start: Position{Line: 9, Column: 4}}
	original := Invocation{
		Script: 29,
		Name:   "binfo",
		Span:   span,
		Arguments: []Argument{
			{Name: "$id", Reference: idCell},
			{Name: "$key", Reference: keyCell},
		},
	}
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		invocation.Arguments[0].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original.Runtime = runtimeInstance

	result, err := runtimeInstance.aggressorSessionQuery(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 {
		t.Fatalf("Host calls = %d, want exactly one", hostCalls.Load())
	}
	if captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 2 {
		t.Fatalf("captured invocation metadata differs: %#v", captured)
	}
	if captured.Arguments[0].Name != "$id" || captured.Arguments[0].Reference != idCell || captured.Arguments[1].Name != "$key" || captured.Arguments[1].Reference != keyCell {
		t.Fatalf("Host did not receive original argument references: %#v", captured.Arguments)
	}
	if got := idCell.Get().String(); got != "mutated by Host" {
		t.Fatalf("Host reference mutation = %q, want visible", got)
	}
}

func TestAggressorSessionQueryUnsetProviderRoutesAllNamesToHost(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		return String("host:" + invocation.Name), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for name, spec := range aggressorSessionQuerySpecs {
		arguments := make([]Value, spec.arity)
		for index := range arguments {
			arguments[index] = String("argument")
		}
		result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
		if invokeErr != nil || result.String() != "host:"+name {
			t.Errorf("%s Host fallback = (%s, %v)", name, result.Describe(), invokeErr)
		}
	}
	if got, want := calls.Load(), int32(len(aggressorSessionQuerySpecs)); got != want {
		t.Fatalf("Host calls = %d, want %d", got, want)
	}
}

func TestAggressorSessionQueryProviderErrorsNeverFallBackToHost(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider rejected query")
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("host result"), nil
		})),
		WithAggressorSessionQueryProvider(AggressorSessionQueryProviderFunc(func(context.Context, AggressorSessionQuery) (Value, error) {
			return String("discarded provider partial result"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Invoke(context.Background(), "binfo", String("B"), String("internal"))
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("provider error = (%s, %v), want null/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("provider error fell back to Host %d time(s)", hostCalls.Load())
	}
}

func TestAggressorSessionQueryBoundaryErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		for _, route := range []string{"Host", "provider"} {
			route := route
			t.Run(route+"/"+boundaryErr.Error(), func(t *testing.T) {
				var hostCalls atomic.Int32
				options := []Option{WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return String("Host partial result"), boundaryErr
				}))}
				if route == "provider" {
					options = append(options, WithAggressorSessionQueryProvider(AggressorSessionQueryProviderFunc(func(context.Context, AggressorSessionQuery) (Value, error) {
						return String("discarded provider partial result"), boundaryErr
					})))
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				result, err := runtimeInstance.Invoke(context.Background(), "binfo", String("B"), String("internal"))
				if !errors.Is(err, boundaryErr) {
					t.Fatalf("Invoke error = %v, want authoritative %v", err, boundaryErr)
				}
				if route == "Host" {
					if result.String() != "Host partial result" || hostCalls.Load() != 1 {
						t.Fatalf("Host result/calls = (%s, %d), want preserved partial result/one call", result.Describe(), hostCalls.Load())
					}
				} else if !result.IsNull() || hostCalls.Load() != 0 {
					t.Fatalf("provider result/Host calls = (%s, %d), want null/zero", result.Describe(), hostCalls.Load())
				}

				_, err = runtimeInstance.Eval(context.Background(), "boundary-error.cna", `binfo("B", "internal");`)
				if !errors.Is(err, boundaryErr) {
					t.Fatalf("script error = %v, want authoritative %v", err, boundaryErr)
				}
			})
		}
	}
}

func TestAggressorSessionQueryBoundaryMarkerDoesNotLeak(t *testing.T) {
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			return String("partial"), ErrUnsafeArrayView
		})),
		WithFunction("ordinary_native", func(context.Context, Invocation) (Value, error) {
			return String("discarded"), ErrUnsafeArrayView
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	if _, err := runtimeInstance.Invoke(context.Background(), "binfo", String("B"), String("internal")); !errors.Is(err, ErrUnsafeArrayView) {
		t.Fatalf("boundary error = %v, want ErrUnsafeArrayView", err)
	}
	result, err := runtimeInstance.Invoke(context.Background(), "ordinary_native")
	if err != nil || !result.IsNull() {
		t.Fatalf("ordinary native result = (%s, %v), want native warning translation", result.Describe(), err)
	}
}

func TestAggressorSessionQueryWithFunctionOverrideWins(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorSessionQueryProvider(AggressorSessionQueryProviderFunc(func(context.Context, AggressorSessionQuery) (Value, error) {
			providerCalls.Add(1)
			return String("provider"), nil
		})),
		WithFunction("barch", func(context.Context, Invocation) (Value, error) {
			return String("override"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Invoke(context.Background(), "barch")
	if err != nil || result.String() != "override" {
		t.Fatalf("barch override = (%s, %v), want override", result.Describe(), err)
	}
	if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("override routed to provider/Host = %d/%d", providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorSessionQueryScriptProvenance(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorSessionQueryProvider{
		query: func(context.Context, AggressorSessionQuery) (Value, error) {
			return String("10.0.0.7"), nil
		},
	}
	runtimeInstance, err := New(WithAggressorSessionQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("query-provenance.cna", `return binfo("B-7", "internal");`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil || result.String() != "10.0.0.7" {
		t.Fatalf("script query = (%s, %v)", result.Describe(), err)
	}
	queries := provider.snapshot()
	if len(queries) != 1 {
		t.Fatalf("queries = %d, want one", len(queries))
	}
	query := queries[0]
	if query.RuntimeID != runtimeInstance.ID() || query.Script == 0 || query.Span.Source != "query-provenance.cna" || query.Span.Start.Line == 0 {
		t.Fatalf("query provenance = runtime %d script %d span %s", query.RuntimeID, query.Script, query.Span)
	}
}

func TestAggressorSessionQueryCancellationAndClose(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var cancelDuringQuery context.CancelFunc
	provider := AggressorSessionQueryProviderFunc(func(ctx context.Context, query AggressorSessionQuery) (Value, error) {
		calls.Add(1)
		if query.SessionID.String() == "cancel-during" {
			cancelDuringQuery()
			return String("late"), nil
		}
		return String("ok"), nil
	})
	runtimeInstance, err := New(WithAggressorSessionQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "barch", String("pre-canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled error = %v, want context.Canceled", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("pre-canceled query reached provider %d time(s)", calls.Load())
	}

	during, cancelDuring := context.WithCancel(context.Background())
	cancelDuringQuery = cancelDuring
	if result, err := runtimeInstance.Invoke(during, "barch", String("cancel-during")); !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("canceled-during-provider = (%s, %v), want null/context.Canceled", result.Describe(), err)
	}
	if calls.Load() != 1 {
		t.Fatalf("canceled-during provider calls = %d, want one", calls.Load())
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Invoke(context.Background(), "barch", String("closed")); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("closed Runtime error = %v, want ErrRuntimeClosed", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("closed Runtime reached provider %d total time(s), want one", calls.Load())
	}
}

func TestAggressorSessionQueryCloseCancelsBlockingProvider(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorSessionQueryProvider(AggressorSessionQueryProviderFunc(func(ctx context.Context, _ AggressorSessionQuery) (Value, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}

	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(context.Background(), "binfo", String("B"), String("internal"))
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtimeInstance.Close(context.Background()) }()
	select {
	case invokeErr := <-invokeDone:
		if !errors.Is(invokeErr, context.Canceled) {
			t.Errorf("blocking provider error = %v, want context.Canceled", invokeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocking provider did not stop on Runtime.Close")
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

func TestAggressorSessionQueryProviderSupportsConcurrentCalls(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorSessionQueryProvider{
		query: func(_ context.Context, query AggressorSessionQuery) (Value, error) {
			return String(query.SessionID.String() + ":" + query.Key.String()), nil
		},
	}
	runtimeInstance, err := New(WithAggressorSessionQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	const calls = 48
	var wait sync.WaitGroup
	errorsByCall := make(chan error, calls)
	for index := 0; index < calls; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			id := fmt.Sprintf("B-%d", index)
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "binfo", String(id), String("internal"))
			if invokeErr == nil && result.String() != id+":internal" {
				invokeErr = fmt.Errorf("result = %s", result.Describe())
			}
			errorsByCall <- invokeErr
		}()
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	queries := provider.snapshot()
	if len(queries) != calls {
		t.Fatalf("concurrent provider calls = %d, want %d", len(queries), calls)
	}
	for index, query := range queries {
		if query.RuntimeID != runtimeInstance.ID() || query.Kind != AggressorSessionQueryBeaconInfo || query.Name != "binfo" {
			t.Errorf("query %d route/provenance = %#v", index, query)
		}
	}
}

func TestRuntimeIDsAndPortableScriptLoaderQueryProviderInheritance(t *testing.T) {
	directory := t.TempDir()
	sharedSource := filepath.Join(directory, "shared-query-source.cna")
	if err := os.WriteFile(sharedSource, []byte(`binfo("child", "internal");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString(filepath.ToSlash(sharedSource), fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$firstLoader = [new ScriptLoader];
$first = [$firstLoader loadScript: %q];
$secondLoader = [new ScriptLoader];
$second = [$secondLoader loadScript: %q];
binfo("parent", "internal");
[$first runScript];
[$second runScript];
`, filepath.ToSlash(sharedSource), filepath.ToSlash(sharedSource)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorSessionQueryProvider{
		query: func(context.Context, AggressorSessionQuery) (Value, error) {
			return String("metadata"), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("query provider was not inherited")
		})),
		WithAggressorSessionQueryProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if runtimeInstance.ID() == 0 {
		t.Fatal("parent Runtime ID is zero")
	}
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited provider queries reached Host %d time(s)", hostCalls.Load())
	}
	queries := provider.snapshot()
	if len(queries) != 3 {
		t.Fatalf("queries = %d, want parent plus two children", len(queries))
	}
	origins := make(map[RuntimeID]struct{}, len(queries))
	for index, query := range queries {
		if query.RuntimeID == 0 {
			t.Fatalf("query %d has zero RuntimeID", index)
		}
		origins[query.RuntimeID] = struct{}{}
		if query.Script != 1 {
			t.Errorf("query %d Script = %d, want colliding runtime-local ID 1", index, query.Script)
		}
		if query.Span.Source != filepath.ToSlash(sharedSource) {
			t.Errorf("query %d source = %q, want %q", index, query.Span.Source, filepath.ToSlash(sharedSource))
		}
	}
	if len(origins) != 3 {
		t.Fatalf("RuntimeIDs = %d, want three distinct parent/child origins", len(origins))
	}
	if queries[0].RuntimeID != runtimeInstance.ID() {
		t.Errorf("parent query RuntimeID = %d, want %d", queries[0].RuntimeID, runtimeInstance.ID())
	}

	secondRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondRuntime.Close(context.Background()) })
	if secondRuntime.ID() == 0 || secondRuntime.ID() <= runtimeInstance.ID() {
		t.Fatalf("independent Runtime IDs = %d/%d, want later monotonic nonzero value", runtimeInstance.ID(), secondRuntime.ID())
	}
	var comparable map[RuntimeID]struct{} = map[RuntimeID]struct{}{runtimeInstance.ID(): {}, secondRuntime.ID(): {}}
	if len(comparable) != 2 {
		t.Fatal("RuntimeID is not usable as a distinct comparable map key")
	}
	var nilRuntime *Runtime
	if nilRuntime.ID() != 0 {
		t.Fatalf("nil Runtime ID = %d, want zero", nilRuntime.ID())
	}
}

func boolPointer(value bool) *bool { return &value }
