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

type recordingAggressorDataModelQueryProvider struct {
	mu      sync.Mutex
	queries []AggressorDataModelQuery
	query   func(context.Context, AggressorDataModelQuery) (Value, error)
}

func (provider *recordingAggressorDataModelQueryProvider) QueryAggressorDataModel(
	ctx context.Context,
	query AggressorDataModelQuery,
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

func (provider *recordingAggressorDataModelQueryProvider) snapshot() []AggressorDataModelQuery {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorDataModelQuery(nil), provider.queries...)
}

func TestAggressorDataModelQueryFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorDataModelQueryFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{"data_keys", "data_query", "pivots"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor data-model query names = %q, want %q", names, want)
	}
}

func TestAggressorDataModelQueryKindsProvenanceAndUnchangedResults(t *testing.T) {
	t.Parallel()

	keysArray := NewArray(String("metadata"), String("targets"))
	keys := ArrayValue(keysArray)
	modelHash := NewOrderedHash()
	modelHash.Set("opaque", ObjectValue(&officialDataModelProfile{}))
	model := HashValue(modelHash)
	pivotHash := NewOrderedHash()
	pivotHash.Set("bid", String("beacon-1"))
	pivots := ArrayValue(NewArray(HashValue(pivotHash)))
	tests := []struct {
		name     string
		kind     AggressorDataModelQueryKind
		argument Value
		provided Value
	}{
		{name: "data_keys", kind: AggressorDataModelQueryKeys, provided: keys},
		{name: "data_query", kind: AggressorDataModelQueryValue, argument: BinaryString([]byte("metadata")), provided: model},
		{name: "pivots", kind: AggressorDataModelQueryPivots, provided: pivots},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var hostCalls atomic.Int32
			provider := &recordingAggressorDataModelQueryProvider{
				query: func(context.Context, AggressorDataModelQuery) (Value, error) {
					return test.provided, nil
				},
			}
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("data-model provider route reached Host")
				})),
				WithAggressorDataModelQueryProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			arguments := []Value(nil)
			if test.name == "data_query" {
				arguments = append(arguments, test.argument)
			}
			result, err := runtimeInstance.Invoke(context.Background(), test.name, arguments...)
			if err != nil || !result.IdentityEqual(test.provided) {
				t.Fatalf("%s result = (%s, %v), want identical provider Value %s",
					test.name, result.Describe(), err, test.provided.Describe())
			}
			if hostCalls.Load() != 0 {
				t.Fatalf("%s provider route reached Host %d time(s)", test.name, hostCalls.Load())
			}
			queries := provider.snapshot()
			if len(queries) != 1 {
				t.Fatalf("%s provider calls = %d, want one", test.name, len(queries))
			}
			query := queries[0]
			if query.Name != test.name || query.Kind != test.kind || query.RuntimeID != runtimeInstance.ID() || query.RuntimeID == 0 || query.Script != 0 || query.Span != (Span{}) {
				t.Fatalf("%s query metadata = %#v", test.name, query)
			}
			if test.name != "data_query" {
				if !query.Key.IsNull() {
					t.Fatalf("%s Key = %s, want null", test.name, query.Key.Describe())
				}
			} else if !query.Key.IdentityEqual(test.argument) {
				t.Fatalf("data_query Key = %s, want identical %s", query.Key.Describe(), test.argument.Describe())
			}
		})
	}

	// A provider's compound result is deliberately transferred, not cloned.
	keysArray.Append(String("beaconlog"))
	if got := keysArray.Len(); got != 3 {
		t.Fatalf("transferred data_keys array length = %d, want 3", got)
	}
}

func TestAggressorDataModelQueryExactArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	var hostCalls atomic.Int32
	provider := &recordingAggressorDataModelQueryProvider{}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorDataModelQueryProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, test := range []struct {
		name      string
		arguments []Value
	}{
		{name: "data_keys", arguments: []Value{String("extra")}},
		{name: "data_query"},
		{name: "data_query", arguments: []Value{String("metadata"), String("extra")}},
		{name: "pivots", arguments: []Value{String("extra")}},
	} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if invokeErr == nil || !result.IsNull() {
			t.Errorf("%s/%d = (%s, %v), want null exact-arity error",
				test.name, len(test.arguments), result.Describe(), invokeErr)
		}
	}
	if got := len(provider.snapshot()); got != 0 {
		t.Fatalf("invalid arities reached provider %d time(s)", got)
	}
	if got := hostCalls.Load(); got != 0 {
		t.Fatalf("invalid arities reached Host %d time(s)", got)
	}
}

func TestAggressorDataModelQueryResolvesKeyOnceAndPreservesScriptProvenance(t *testing.T) {
	t.Parallel()

	compoundKey := ArrayValue(NewArray(String("metadata")))
	keyCell := NewCell(compoundKey)
	var captured AggressorDataModelQuery
	provider := AggressorDataModelQueryProviderFunc(func(_ context.Context, query AggressorDataModelQuery) (Value, error) {
		captured = query
		keyCell.Set(String("changed"))
		return query.Key, nil
	})
	runtimeInstance, err := New(WithAggressorDataModelQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	span := Span{Source: "data-model-provenance.cna", Start: Position{Line: 7, Column: 3}}
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  41,
		Name:    "data_query",
		Span:    span,
		Arguments: []Argument{
			{Name: "$key", Reference: keyCell},
		},
	}
	result, err := runtimeInstance.aggressorDataModelQuery(context.Background(), invocation)
	if err != nil || !result.IdentityEqual(compoundKey) || !captured.Key.IdentityEqual(compoundKey) {
		t.Fatalf("resolved query = result %s/captured %s/error %v, want original compound identity",
			result.Describe(), captured.Key.Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 41 || captured.Span != span {
		t.Fatalf("provider provenance = runtime %d script %d span %s", captured.RuntimeID, captured.Script, captured.Span)
	}
	if got := keyCell.Get().String(); got != "changed" {
		t.Fatalf("provider reference mutation setup = %q, want changed", got)
	}
}

func TestAggressorDataModelQueryUnsetProviderPreservesHostInvocation(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("host data-model result")
	wantResult := HashValue(NewHash())
	keyCell := NewCell(String("before"))
	span := Span{Source: "data-model-host.cna", Start: Position{Line: 9, Column: 4}}
	original := Invocation{
		Script: 29,
		Name:   "data_query",
		Span:   span,
		Arguments: []Argument{
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

	result, err := runtimeInstance.aggressorDataModelQuery(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 1 {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
	}
	if captured.Arguments[0].Name != "$key" || captured.Arguments[0].Reference != keyCell || keyCell.Get().String() != "mutated by Host" {
		t.Fatalf("Host did not receive the original reference-bearing argument: %#v", captured.Arguments)
	}
}

func TestAggressorDataModelQueryUnsetProviderRoutesEveryNameToHost(t *testing.T) {
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
	for _, test := range []struct {
		name      string
		arguments []Value
	}{
		{name: "data_keys"},
		{name: "data_query", arguments: []Value{Null()}},
		{name: "pivots"},
	} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if invokeErr != nil || result.String() != "host:"+test.name {
			t.Errorf("%s Host fallback = (%s, %v)", test.name, result.Describe(), invokeErr)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("Host calls = %d, want three", got)
	}
}

func TestAggressorDataModelQueryProviderErrorsNeverFallBackToHost(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider failed")
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host result"), nil
		})),
		WithAggressorDataModelQueryProvider(AggressorDataModelQueryProviderFunc(func(context.Context, AggressorDataModelQuery) (Value, error) {
			return String("discarded provider partial result"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Invoke(context.Background(), "data_query", String("metadata"))
	if !errors.Is(err, wantErr) || !result.IsNull() || hostCalls.Load() != 0 {
		t.Fatalf("provider error = (%s, %v), Host calls %d; want null/%v/zero",
			result.Describe(), err, hostCalls.Load(), wantErr)
	}
}

func TestAggressorDataModelQueryBoundaryErrorsBypassNativeWarningTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		for _, route := range []string{"Host", "provider"} {
			route := route
			t.Run(route+"/"+boundaryErr.Error(), func(t *testing.T) {
				var hostCalls atomic.Int32
				options := []Option{
					WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
						hostCalls.Add(1)
						return String("Host partial result"), boundaryErr
					})),
					WithFunction("ordinary_native", func(context.Context, Invocation) (Value, error) {
						return String("discarded ordinary partial result"), ErrUnsafeArrayView
					}),
				}
				if route == "provider" {
					options = append(options, WithAggressorDataModelQueryProvider(AggressorDataModelQueryProviderFunc(func(context.Context, AggressorDataModelQuery) (Value, error) {
						return String("discarded provider partial result"), boundaryErr
					})))
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				result, err := runtimeInstance.Invoke(context.Background(), "data_query", String("metadata"))
				if !errors.Is(err, boundaryErr) {
					t.Fatalf("Invoke error = %v, want authoritative %v", err, boundaryErr)
				}
				if route == "Host" {
					if result.String() != "Host partial result" || hostCalls.Load() != 1 {
						t.Fatalf("Host result/calls = (%s, %d), want partial/one", result.Describe(), hostCalls.Load())
					}
				} else if !result.IsNull() || hostCalls.Load() != 0 {
					t.Fatalf("provider result/Host calls = (%s, %d), want null/zero", result.Describe(), hostCalls.Load())
				}

				_, err = runtimeInstance.Eval(context.Background(), "data-model-boundary.cna", `data_query("metadata");`)
				if !errors.Is(err, boundaryErr) {
					t.Fatalf("script boundary error = %v, want %v", err, boundaryErr)
				}
				ordinary, err := runtimeInstance.Invoke(context.Background(), "ordinary_native")
				if err != nil || !ordinary.IsNull() {
					t.Fatalf("boundary marker leaked to ordinary native = (%s, %v)", ordinary.Describe(), err)
				}
			})
		}
	}
}

func TestAggressorDataModelQueryOverrideAndNilProviderPolicy(t *testing.T) {
	for _, name := range []string{"data_keys", "data_query", "pivots"} {
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				providerOption := WithAggressorDataModelQueryProvider(AggressorDataModelQueryProviderFunc(func(context.Context, AggressorDataModelQuery) (Value, error) {
					providerCalls.Add(1)
					return String("provider"), nil
				}))
				overrideOption := WithFunction(name, func(context.Context, Invocation) (Value, error) {
					return String("override"), nil
				})
				options := []Option{hostOption, providerOption, overrideOption}
				if overrideFirst {
					options = []Option{hostOption, overrideOption, providerOption}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				// Deliberately use stock-wrapper-invalid arity so success proves the
				// importer override was selected before wrapper validation.
				arguments := []Value(nil)
				if name != "data_query" {
					arguments = []Value{String("extra")}
				}
				result, err := runtimeInstance.Invoke(context.Background(), name, arguments...)
				if err != nil || result.String() != "override" || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider/Host %d/%d; want override/zero/zero",
						result.Describe(), err, providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorDataModelQueryProvider
	if _, err := New(WithAggressorDataModelQueryProvider(typedNil)); err == nil {
		t.Fatal("typed-nil data-model provider was accepted")
	}
	var nilFunction AggressorDataModelQueryProviderFunc
	if _, err := New(WithAggressorDataModelQueryProvider(nilFunction)); err == nil {
		t.Fatal("nil provider function was accepted")
	}
	if _, err := nilFunction.QueryAggressorDataModel(context.Background(), AggressorDataModelQuery{}); err == nil {
		t.Fatal("direct nil provider function call succeeded")
	}
}

func TestAggressorDataModelQueryCancellation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var cancelDuringQuery context.CancelFunc
	provider := &recordingAggressorDataModelQueryProvider{
		query: func(_ context.Context, query AggressorDataModelQuery) (Value, error) {
			calls.Add(1)
			if query.Key.String() == "cancel" {
				cancelDuringQuery()
				return String("late"), nil
			}
			return query.Key, nil
		},
	}
	runtimeInstance, err := New(WithAggressorDataModelQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Invoke(preCanceled, "data_keys"); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("pre-canceled call = error %v/provider calls %d", err, calls.Load())
	}
	during, cancelDuring := context.WithCancel(context.Background())
	cancelDuringQuery = cancelDuring
	if result, err := runtimeInstance.Invoke(during, "data_query", String("cancel")); !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("canceled provider result = (%s, %v), want null/context.Canceled", result.Describe(), err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want canceled-during call only", got)
	}
}

func TestAggressorDataModelQueryCloseCancelsBlockingProvider(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorDataModelQueryProvider(AggressorDataModelQueryProviderFunc(func(ctx context.Context, _ AggressorDataModelQuery) (Value, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}
	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(context.Background(), "data_keys")
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

func TestAggressorDataModelQueryProviderSupportsConcurrentCompoundIdentity(t *testing.T) {
	t.Parallel()

	const concurrentCalls = 24
	entered := make(chan struct{}, concurrentCalls)
	release := make(chan struct{})
	provider := &recordingAggressorDataModelQueryProvider{
		query: func(_ context.Context, query AggressorDataModelQuery) (Value, error) {
			entered <- struct{}{}
			<-release
			return query.Key, nil
		},
	}
	runtimeInstance, err := New(WithAggressorDataModelQueryProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	keys := make([]Value, concurrentCalls)
	var wait sync.WaitGroup
	errorsByCall := make(chan error, concurrentCalls)
	for index := 0; index < concurrentCalls; index++ {
		index := index
		keys[index] = ArrayValue(NewArray(Int(int32(index))))
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, invokeErr := runtimeInstance.Invoke(context.Background(), "data_query", keys[index])
			if invokeErr == nil && !result.IdentityEqual(keys[index]) {
				invokeErr = fmt.Errorf("result = %s, want compound key identity %s", result.Describe(), keys[index].Describe())
			}
			errorsByCall <- invokeErr
		}()
	}
	for index := 0; index < concurrentCalls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatalf("only %d/%d provider calls entered concurrently", index, concurrentCalls)
		}
	}
	close(release)
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	queries := provider.snapshot()
	if len(queries) != concurrentCalls {
		t.Fatalf("provider queries = %d, want %d", len(queries), concurrentCalls)
	}
	seen := make(map[*Array]struct{}, concurrentCalls)
	for _, query := range queries {
		array, ok := query.Key.Array()
		if !ok {
			t.Fatalf("provider Key = %s, want array identity", query.Key.Describe())
		}
		seen[array] = struct{}{}
	}
	if len(seen) != concurrentCalls {
		t.Fatalf("unique compound provider keys = %d, want %d", len(seen), concurrentCalls)
	}
}

func TestPortableScriptLoaderInheritsAggressorDataModelQueryProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-data-model.cna")
	if err := os.WriteFile(childPath, []byte(`data_query("child"); pivots();`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-data-model.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
data_query("parent");
pivots();
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorDataModelQueryProvider{
		query: func(_ context.Context, query AggressorDataModelQuery) (Value, error) {
			return query.Key, nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("data-model provider was not inherited")
		})),
		WithAggressorDataModelQueryProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited provider queries reached Host %d time(s)", hostCalls.Load())
	}
	queries := provider.snapshot()
	if len(queries) != 4 || queries[0].Kind != AggressorDataModelQueryValue || queries[0].Key.String() != "parent" ||
		queries[1].Kind != AggressorDataModelQueryPivots || !queries[1].Key.IsNull() ||
		queries[2].Kind != AggressorDataModelQueryValue || queries[2].Key.String() != "child" ||
		queries[3].Kind != AggressorDataModelQueryPivots || !queries[3].Key.IsNull() {
		t.Fatalf("parent/child queries = %#v", queries)
	}
	if queries[0].RuntimeID != runtimeInstance.ID() || queries[1].RuntimeID != queries[0].RuntimeID ||
		queries[2].RuntimeID == queries[0].RuntimeID || queries[2].RuntimeID == 0 || queries[3].RuntimeID != queries[2].RuntimeID {
		t.Fatalf("parent/child RuntimeIDs = %d/%d/%d/%d",
			queries[0].RuntimeID, queries[1].RuntimeID, queries[2].RuntimeID, queries[3].RuntimeID)
	}
	if queries[0].Script != 1 || queries[1].Script != 1 || queries[2].Script != 1 || queries[3].Script != 1 ||
		queries[0].Span.Source != "parent-data-model.cna" || queries[1].Span.Source != "parent-data-model.cna" ||
		queries[2].Span.Source != filepath.ToSlash(childPath) || queries[3].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", queries)
	}
}
