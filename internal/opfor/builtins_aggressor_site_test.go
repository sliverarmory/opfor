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

type recordingAggressorSiteProvider struct {
	mu       sync.Mutex
	requests []AggressorSiteRequest
	handle   func(context.Context, AggressorSiteRequest) (Value, error)
}

func (provider *recordingAggressorSiteProvider) HandleAggressorSite(
	ctx context.Context,
	request AggressorSiteRequest,
) (Value, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	function := provider.handle
	provider.mu.Unlock()
	if function == nil {
		return Null(), nil
	}
	return function(ctx, request)
}

func (provider *recordingAggressorSiteProvider) snapshot() []AggressorSiteRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorSiteRequest(nil), provider.requests...)
}

func TestAggressorSiteFunctionSet(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorSiteFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{"localip", "site_host", "site_kill", "sites"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Aggressor site function names = %q, want %q", names, want)
	}
}

func TestAggressorSiteKindsFieldsAndResultPolicy(t *testing.T) {
	t.Parallel()

	hostedContent := BinaryString([]byte{0x00, 0xff, 'M', 'Z'})
	sslMarker := ArrayValue(NewArray(String("enabled")))
	siteHash := NewOrderedHash()
	siteHash.Set("URI", String("/payload"))
	siteHash.Set("Port", Int(8443))
	siteList := ArrayValue(NewArray(HashValue(siteHash)))
	killResult := ObjectValue(&struct{ removed bool }{removed: true})

	tests := []struct {
		name          string
		arguments     []Value
		kind          AggressorSiteKind
		provided      Value
		discardResult bool
		wantHost      Value
		wantPort      Value
		wantURI       Value
		wantContent   Value
		wantMIME      Value
		wantDesc      Value
		wantSSL       Value
		wantHasSSL    bool
		wantSSLTruth  bool
	}{
		{
			name:     "localip",
			kind:     AggressorSiteLocalIP,
			provided: String("10.20.30.40"),
		},
		{
			name: "site_host",
			arguments: []Value{
				String("downloads.example"),
				Int(8080),
				String("/payload"),
				hostedContent,
				String("application/octet-stream"),
				String("official six-argument form"),
			},
			kind:        AggressorSiteHost,
			provided:    BinaryString([]byte("http://downloads.example:8080/payload")),
			wantHost:    String("downloads.example"),
			wantPort:    Int(8080),
			wantURI:     String("/payload"),
			wantContent: hostedContent,
			wantMIME:    String("application/octet-stream"),
			wantDesc:    String("official six-argument form"),
		},
		{
			name: "site_host",
			arguments: []Value{
				String("secure.example"),
				Int(8443),
				String("/payload"),
				hostedContent,
				String("application/octet-stream"),
				String("current seven-argument form"),
				sslMarker,
			},
			kind:         AggressorSiteHost,
			provided:     String("https://secure.example:8443/payload"),
			wantHost:     String("secure.example"),
			wantPort:     Int(8443),
			wantURI:      String("/payload"),
			wantContent:  hostedContent,
			wantMIME:     String("application/octet-stream"),
			wantDesc:     String("current seven-argument form"),
			wantSSL:      sslMarker,
			wantHasSSL:   true,
			wantSSLTruth: true,
		},
		{
			name:          "site_kill",
			arguments:     []Value{Int(8443), String("/payload")},
			kind:          AggressorSiteKill,
			provided:      killResult,
			discardResult: true,
			wantPort:      Int(8443),
			wantURI:       String("/payload"),
		},
		{
			name:     "sites",
			kind:     AggressorSiteList,
			provided: siteList,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%s/%d", test.name, len(test.arguments)), func(t *testing.T) {
			t.Parallel()

			var hostCalls atomic.Int32
			provider := &recordingAggressorSiteProvider{
				handle: func(context.Context, AggressorSiteRequest) (Value, error) {
					return test.provided, nil
				},
			}
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("site provider route reached Host")
				})),
				WithAggressorSiteProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
			wantResult := test.provided
			if test.discardResult {
				wantResult = Null()
			}
			if err != nil || !result.IdentityEqual(wantResult) {
				t.Fatalf("%s result = (%s, %v), want %s",
					test.name, result.Describe(), err, wantResult.Describe())
			}
			if hostCalls.Load() != 0 {
				t.Fatalf("%s provider route reached Host %d time(s)", test.name, hostCalls.Load())
			}
			requests := provider.snapshot()
			if len(requests) != 1 {
				t.Fatalf("%s provider calls = %d, want one", test.name, len(requests))
			}
			request := requests[0]
			if request.Kind != test.kind || request.Name != test.name || request.RuntimeID != runtimeInstance.ID() || request.RuntimeID == 0 || request.Script != 0 || request.Span != (Span{}) {
				t.Fatalf("%s request provenance = %#v", test.name, request)
			}
			assertAggressorSiteRequestValue(t, "Host", request.Host, test.wantHost)
			assertAggressorSiteRequestValue(t, "Port", request.Port, test.wantPort)
			assertAggressorSiteRequestValue(t, "URI", request.URI, test.wantURI)
			assertAggressorSiteRequestValue(t, "Content", request.Content, test.wantContent)
			assertAggressorSiteRequestValue(t, "MIMEType", request.MIMEType, test.wantMIME)
			assertAggressorSiteRequestValue(t, "Description", request.Description, test.wantDesc)
			assertAggressorSiteRequestValue(t, "SSL", request.SSL, test.wantSSL)
			if request.HasSSL != test.wantHasSSL || request.SSLTruth != test.wantSSLTruth {
				t.Fatalf("SSL state = HasSSL %v/SSLTruth %v, want %v/%v",
					request.HasSSL, request.SSLTruth, test.wantHasSSL, test.wantSSLTruth)
			}
			if test.name == "site_host" && !request.Content.IsBinaryString() {
				t.Fatal("site_host Content lost binary provenance")
			}
		})
	}

	// The sites result is transferred, not cloned. This also demonstrates that
	// the documented array-of-dictionaries shape remains a live provider Value.
	siteHash.Set("Description", String("script-visible mutation"))
	if got, ok := siteHash.Get("Description"); !ok || got.String() != "script-visible mutation" {
		t.Fatalf("transferred sites dictionary mutation = (%s, %v)", got.Describe(), ok)
	}
}

func assertAggressorSiteRequestValue(t *testing.T, field string, got, want Value) {
	t.Helper()
	if !got.IdentityEqual(want) {
		t.Fatalf("request %s = %s, want identical %s", field, got.Describe(), want.Describe())
	}
}

func TestAggressorSiteHostSSLOmissionNullFalseAndTruth(t *testing.T) {
	t.Parallel()

	base := []Value{
		String("host"), Int(443), String("/"), String("content"),
		String("text/plain"), String("description"),
	}
	tests := []struct {
		name      string
		ssl       Value
		supplied  bool
		wantTruth bool
	}{
		{name: "omitted"},
		{name: "explicit-null", supplied: true, ssl: Null()},
		{name: "integer-zero", supplied: true, ssl: Int(0)},
		{name: "empty-string", supplied: true, ssl: String("")},
		{name: "string-zero", supplied: true, ssl: String("0")},
		{name: "nonempty-string", supplied: true, ssl: String("false"), wantTruth: true},
		{name: "compound", supplied: true, ssl: HashValue(NewHash()), wantTruth: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var captured AggressorSiteRequest
			runtimeInstance, err := New(WithAggressorSiteProvider(AggressorSiteProviderFunc(func(_ context.Context, request AggressorSiteRequest) (Value, error) {
				captured = request
				return request.SSL, nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			arguments := append([]Value(nil), base...)
			if test.supplied {
				arguments = append(arguments, test.ssl)
			}
			result, err := runtimeInstance.Invoke(context.Background(), "site_host", arguments...)
			if err != nil {
				t.Fatal(err)
			}
			if captured.HasSSL != test.supplied || captured.SSLTruth != test.wantTruth {
				t.Fatalf("SSL state = HasSSL %v/SSLTruth %v, want %v/%v",
					captured.HasSSL, captured.SSLTruth, test.supplied, test.wantTruth)
			}
			if !captured.SSL.IdentityEqual(test.ssl) || !result.IdentityEqual(test.ssl) {
				t.Fatalf("SSL exact Value/result = %s/%s, want %s",
					captured.SSL.Describe(), result.Describe(), test.ssl.Describe())
			}
		})
	}
}

func TestAggressorSiteExactArityStopsProviderAndHost(t *testing.T) {
	t.Parallel()

	var hostCalls atomic.Int32
	provider := &recordingAggressorSiteProvider{}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorSiteProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, test := range []struct {
		name      string
		arguments []Value
		wantText  string
	}{
		{name: "localip", arguments: []Value{String("extra")}, wantText: "expected exactly 0 argument(s), received 1"},
		{name: "site_host", arguments: make([]Value, 5), wantText: "expected 6 to 7 argument(s), received 5"},
		{name: "site_host", arguments: make([]Value, 8), wantText: "expected 6 to 7 argument(s), received 8"},
		{name: "site_kill", arguments: []Value{Int(80)}, wantText: "expected exactly 2 argument(s), received 1"},
		{name: "site_kill", arguments: []Value{Int(80), String("/"), String("extra")}, wantText: "expected exactly 2 argument(s), received 3"},
		{name: "sites", arguments: []Value{String("extra")}, wantText: "expected exactly 0 argument(s), received 1"},
	} {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if invokeErr == nil || !result.IsNull() || !strings.Contains(invokeErr.Error(), test.wantText) {
			t.Errorf("%s/%d = (%s, %v), want null error containing %q",
				test.name, len(test.arguments), result.Describe(), invokeErr, test.wantText)
		}
	}
	if got := len(provider.snapshot()); got != 0 {
		t.Fatalf("invalid arities reached provider %d time(s)", got)
	}
	if got := hostCalls.Load(); got != 0 {
		t.Fatalf("invalid arities reached Host %d time(s)", got)
	}
}

func TestAggressorSiteResolvesReferencesOnceWithExactIdentityAndProvenance(t *testing.T) {
	t.Parallel()

	originals := []Value{
		ObjectValue(&struct{ name string }{name: "host"}),
		Long(8443),
		BinaryString([]byte{'/', 0x00, 'x'}),
		BinaryString([]byte{0x00, 0xff, 0x7f}),
		HashValue(NewHash()),
		ArrayValue(NewArray(String("description"))),
		ArrayValue(NewArray(String("ssl"))),
	}
	cells := make([]*Cell, len(originals))
	arguments := make([]Argument, len(originals))
	for index, value := range originals {
		cells[index] = NewCell(value)
		arguments[index] = Argument{Name: fmt.Sprintf("$argument%d", index+1), Reference: cells[index]}
	}

	var captured AggressorSiteRequest
	provider := AggressorSiteProviderFunc(func(_ context.Context, request AggressorSiteRequest) (Value, error) {
		captured = request
		for _, cell := range cells {
			cell.Set(String("changed after snapshot"))
		}
		return request.Content, nil
	})
	runtimeInstance, err := New(WithAggressorSiteProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	span := Span{Source: "site-provenance.cna", Start: Position{Line: 17, Column: 5}}
	invocation := Invocation{
		Runtime:   runtimeInstance,
		Script:    41,
		Name:      "site_host",
		Arguments: arguments,
		Span:      span,
	}

	result, err := runtimeInstance.aggressorSite(context.Background(), invocation)
	if err != nil || !result.IdentityEqual(originals[3]) || !result.IsBinaryString() {
		t.Fatalf("site_host snapshot result = (%s, %v), want original binary Content", result.Describe(), err)
	}
	if captured.RuntimeID != runtimeInstance.ID() || captured.Script != 41 || captured.Span != span || captured.Name != "site_host" || captured.Kind != AggressorSiteHost {
		t.Fatalf("request provenance = %#v", captured)
	}
	got := []Value{captured.Host, captured.Port, captured.URI, captured.Content, captured.MIMEType, captured.Description, captured.SSL}
	for index := range originals {
		if !got[index].IdentityEqual(originals[index]) {
			t.Errorf("request field %d = %s, want original identity %s", index, got[index].Describe(), originals[index].Describe())
		}
		if cells[index].Get().String() != "changed after snapshot" {
			t.Errorf("argument cell %d was not mutated by provider setup", index)
		}
	}
	if !captured.HasSSL || !captured.SSLTruth {
		t.Fatalf("captured SSL state = HasSSL %v/SSLTruth %v, want true/true", captured.HasSSL, captured.SSLTruth)
	}
}

func TestAggressorSiteUnsetProviderPreservesHostInvocationExactlyOnce(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Host site result")
	wantResult := ObjectValue(&struct{ host bool }{host: true})
	hostCell := NewCell(String("before"))
	contentCell := NewCell(BinaryString([]byte{0x00, 0xff}))
	span := Span{Source: "site-host-fallback.cna", Start: Position{Line: 9, Column: 4}}
	original := Invocation{
		Script: 29,
		Name:   "site_host",
		Span:   span,
		Arguments: []Argument{
			{Name: "$host", Reference: hostCell},
			{Value: Int(8080)},
			{Value: String("/payload")},
			{Name: "$content", Reference: contentCell},
			{Value: String("application/octet-stream")},
			{Value: String("description")},
			{Value: Int(1)},
		},
	}
	var captured Invocation
	var capturedContext context.Context
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(ctx context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		capturedContext = ctx
		captured = invocation
		invocation.Arguments[0].Set(String("mutated by Host"))
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original.Runtime = runtimeInstance
	contextKey := &struct{ name string }{name: "site-host-context"}
	ctx := context.WithValue(context.Background(), contextKey, "preserved")

	result, err := runtimeInstance.aggressorSite(ctx, original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 {
		t.Fatalf("Host calls = %d, want exactly one", hostCalls.Load())
	}
	if capturedContext != ctx || capturedContext.Value(contextKey) != "preserved" {
		t.Fatal("Host fallback did not receive the original context")
	}
	if captured.Runtime != original.Runtime || captured.Script != original.Script || captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != len(original.Arguments) {
		t.Fatalf("captured Host invocation metadata differs: %#v", captured)
	}
	if &captured.Arguments[0] != &original.Arguments[0] || captured.Arguments[0].Reference != hostCell || captured.Arguments[3].Reference != contentCell {
		t.Fatalf("Host did not receive original argument slice/references: %#v", captured.Arguments)
	}
	if hostCell.Get().String() != "mutated by Host" || !contentCell.Get().IsBinaryString() {
		t.Fatalf("Host reference capabilities changed: host=%s content=%s",
			hostCell.Get().Describe(), contentCell.Get().Describe())
	}
}

func TestAggressorSiteUnsetProviderRoutesEveryValidFormToHost(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		return String(fmt.Sprintf("host:%s:%d", invocation.Name, len(invocation.Arguments))), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name      string
		arguments []Value
	}{
		{name: "localip"},
		{name: "site_host", arguments: make([]Value, 6)},
		{name: "site_host", arguments: make([]Value, 7)},
		{name: "site_kill", arguments: make([]Value, 2)},
		{name: "sites"},
	}
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		want := fmt.Sprintf("host:%s:%d", test.name, len(test.arguments))
		if invokeErr != nil || result.String() != want {
			t.Errorf("%s/%d Host fallback = (%s, %v), want %q",
				test.name, len(test.arguments), result.Describe(), invokeErr, want)
		}
	}
	if got, want := calls.Load(), int32(len(tests)); got != want {
		t.Fatalf("Host calls = %d, want %d", got, want)
	}
}

func TestAggressorSiteProviderErrorsAreAuthoritativeForQueriesAndEffects(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("site provider rejected request")
	var hostCalls atomic.Int32
	var providerCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host result"), nil
		})),
		WithAggressorSiteProvider(AggressorSiteProviderFunc(func(context.Context, AggressorSiteRequest) (Value, error) {
			providerCalls.Add(1)
			return String("discarded provider partial result"), wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	tests := []struct {
		name      string
		arguments []Value
	}{
		{name: "localip"},
		{name: "site_host", arguments: make([]Value, 6)},
		{name: "site_kill", arguments: make([]Value, 2)},
		{name: "sites"},
	}
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		if !errors.Is(invokeErr, wantErr) || !result.IsNull() {
			t.Errorf("%s provider error = (%s, %v), want null/%v",
				test.name, result.Describe(), invokeErr, wantErr)
		}
	}
	if got, want := providerCalls.Load(), int32(len(tests)); got != want {
		t.Fatalf("provider calls = %d, want %d", got, want)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("provider errors retried through Host %d time(s)", hostCalls.Load())
	}
}

func TestAggressorSiteBoundaryErrorsRemainAuthoritative(t *testing.T) {
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
					WithFunction("ordinary_site_boundary_control", func(context.Context, Invocation) (Value, error) {
						return String("discarded ordinary partial result"), ErrUnsafeArrayView
					}),
				}
				if route == "provider" {
					options = append(options, WithAggressorSiteProvider(AggressorSiteProviderFunc(func(context.Context, AggressorSiteRequest) (Value, error) {
						return String("discarded provider partial result"), boundaryErr
					})))
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				result, err := runtimeInstance.Invoke(context.Background(), "localip")
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

				_, err = runtimeInstance.Eval(context.Background(), "site-boundary.cna", `localip();`)
				if !errors.Is(err, boundaryErr) {
					t.Fatalf("script boundary error = %v, want %v", err, boundaryErr)
				}
				ordinary, err := runtimeInstance.Invoke(context.Background(), "ordinary_site_boundary_control")
				if err != nil || !ordinary.IsNull() {
					t.Fatalf("boundary marker leaked to ordinary native = (%s, %v)", ordinary.Describe(), err)
				}
			})
		}
	}
}

func TestAggressorSiteWithFunctionPrecedenceAndNilProviderPolicy(t *testing.T) {
	for _, name := range []string{"localip", "site_host", "site_kill", "sites"} {
		for _, overrideFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/override-first=%v", name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				providerOption := WithAggressorSiteProvider(AggressorSiteProviderFunc(func(context.Context, AggressorSiteRequest) (Value, error) {
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

				// Invalid stock-wrapper arity proves the override is selected before
				// native validation, independent of option order.
				result, err := runtimeInstance.Invoke(context.Background(), name, String("override-only argument"))
				if err != nil || result.String() != "override" || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), provider/Host %d/%d; want override/zero/zero",
						result.Describe(), err, providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}

	var typedNil *recordingAggressorSiteProvider
	if _, err := New(WithAggressorSiteProvider(typedNil)); err == nil {
		t.Fatal("typed-nil site provider was accepted")
	}
	var nilFunction AggressorSiteProviderFunc
	if _, err := New(WithAggressorSiteProvider(nilFunction)); err == nil {
		t.Fatal("nil site provider function was accepted")
	}
	if result, err := nilFunction.HandleAggressorSite(context.Background(), AggressorSiteRequest{}); err == nil || !result.IsNull() {
		t.Fatalf("direct nil provider function call = (%s, %v), want null/error", result.Describe(), err)
	}
}

func TestAggressorSiteScriptProvenance(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorSiteProvider{
		handle: func(context.Context, AggressorSiteRequest) (Value, error) {
			return String("10.0.0.7"), nil
		},
	}
	runtimeInstance, err := New(WithAggressorSiteProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("site-script-provenance.cna", `return localip();`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil || result.String() != "10.0.0.7" {
		t.Fatalf("script localip = (%s, %v)", result.Describe(), err)
	}
	requests := provider.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want one", len(requests))
	}
	request := requests[0]
	if request.RuntimeID != runtimeInstance.ID() || request.Script == 0 || request.Span.Source != "site-script-provenance.cna" || request.Span.Start.Line == 0 {
		t.Fatalf("request provenance = runtime %d script %d span %s", request.RuntimeID, request.Script, request.Span)
	}
}

func TestAggressorSiteCancellationAndNilContext(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var nilContextObserved atomic.Bool
	var cancelDuringRequest context.CancelFunc
	provider := AggressorSiteProviderFunc(func(ctx context.Context, request AggressorSiteRequest) (Value, error) {
		calls.Add(1)
		if ctx == nil {
			nilContextObserved.Store(true)
		}
		if request.URI.String() == "/cancel" {
			cancelDuringRequest()
			return String("late"), nil
		}
		return String("ok"), nil
	})
	runtimeInstance, err := New(WithAggressorSiteProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := runtimeInstance.Invoke(preCanceled, "localip"); !errors.Is(err, context.Canceled) || !result.IsNull() || calls.Load() != 0 {
		t.Fatalf("pre-canceled call = (%s, %v), provider calls %d", result.Describe(), err, calls.Load())
	}

	during, cancelDuring := context.WithCancel(context.Background())
	cancelDuringRequest = cancelDuring
	if result, err := runtimeInstance.Invoke(during, "site_kill", Int(80), String("/cancel")); !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("canceled-during-provider = (%s, %v), want null/context.Canceled", result.Describe(), err)
	}

	invocation := Invocation{Runtime: runtimeInstance, Name: "localip"}
	if result, err := runtimeInstance.aggressorSite(nil, invocation); err != nil || result.String() != "ok" {
		t.Fatalf("nil-context provider call = (%s, %v), want ok", result.Describe(), err)
	}
	if nilContextObserved.Load() {
		t.Fatal("provider observed a nil context")
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want canceled-during plus nil-context calls", calls.Load())
	}
}

func TestAggressorSiteCloseCancelsBlockingProvider(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	runtimeInstance, err := New(WithAggressorSiteProvider(AggressorSiteProviderFunc(func(ctx context.Context, _ AggressorSiteRequest) (Value, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return Null(), ctx.Err()
	})))
	if err != nil {
		t.Fatal(err)
	}
	invokeDone := make(chan error, 1)
	go func() {
		_, invokeErr := runtimeInstance.Invoke(context.Background(), "sites")
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("site provider did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtimeInstance.Close(context.Background()) }()
	select {
	case invokeErr := <-invokeDone:
		if !errors.Is(invokeErr, context.Canceled) {
			t.Errorf("blocking site provider error = %v, want context.Canceled", invokeErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocking site provider did not stop on Runtime.Close")
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

func TestAggressorSiteProviderSupportsConcurrentRequestsAndCompoundIdentity(t *testing.T) {
	t.Parallel()

	const concurrentCalls = 24
	entered := make(chan struct{}, concurrentCalls)
	release := make(chan struct{})
	provider := &recordingAggressorSiteProvider{
		handle: func(_ context.Context, request AggressorSiteRequest) (Value, error) {
			entered <- struct{}{}
			<-release
			return request.Content, nil
		},
	}
	runtimeInstance, err := New(WithAggressorSiteProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	contents := make([]Value, concurrentCalls)
	var wait sync.WaitGroup
	errorsByCall := make(chan error, concurrentCalls)
	for index := 0; index < concurrentCalls; index++ {
		index := index
		contents[index] = ArrayValue(NewArray(Int(int32(index))))
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, invokeErr := runtimeInstance.Invoke(
				context.Background(),
				"site_host",
				String("host"), Int(8080), String(fmt.Sprintf("/%d", index)),
				contents[index], String("application/octet-stream"), String("concurrent"),
			)
			if invokeErr == nil && !result.IdentityEqual(contents[index]) {
				invokeErr = fmt.Errorf("result = %s, want Content identity %s", result.Describe(), contents[index].Describe())
			}
			errorsByCall <- invokeErr
		}()
	}
	for index := 0; index < concurrentCalls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatalf("only %d/%d site provider calls entered concurrently", index, concurrentCalls)
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

	requests := provider.snapshot()
	if len(requests) != concurrentCalls {
		t.Fatalf("provider requests = %d, want %d", len(requests), concurrentCalls)
	}
	seen := make(map[*Array]struct{}, concurrentCalls)
	for _, request := range requests {
		array, ok := request.Content.Array()
		if !ok {
			t.Fatalf("provider Content = %s, want array identity", request.Content.Describe())
		}
		seen[array] = struct{}{}
	}
	if len(seen) != concurrentCalls {
		t.Fatalf("unique compound provider Content Values = %d, want %d", len(seen), concurrentCalls)
	}
}

func TestPortableScriptLoaderInheritsAggressorSiteProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-site.cna")
	if err := os.WriteFile(childPath, []byte(`localip();`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-site.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
localip();
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorSiteProvider{
		handle: func(context.Context, AggressorSiteRequest) (Value, error) {
			return String("192.0.2.1"), nil
		},
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader site route reached Host")
		})),
		WithAggressorSiteProvider(provider),
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
	if requests[0].Kind != AggressorSiteLocalIP || requests[1].Kind != AggressorSiteLocalIP {
		t.Fatalf("parent/child site requests = %#v", requests)
	}
	if requests[0].RuntimeID != runtimeInstance.ID() || requests[0].RuntimeID == 0 ||
		requests[1].RuntimeID == 0 || requests[1].RuntimeID == requests[0].RuntimeID {
		t.Fatalf("parent/child RuntimeIDs = %d/%d", requests[0].RuntimeID, requests[1].RuntimeID)
	}
	if requests[0].Script != 1 || requests[1].Script != 1 ||
		requests[0].Span.Source != "parent-site.cna" || requests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", requests)
	}
}

func TestAggressorSiteUnsupportedAndNilRuntimePolicy(t *testing.T) {
	t.Parallel()

	span := Span{Source: "unsupported-site.cna", Start: Position{Line: 3, Column: 2}}
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.aggressorSite(context.Background(), Invocation{Name: "site_unknown", Span: span})
	var unsupported *UnsupportedError
	if !result.IsNull() || !errors.As(err, &unsupported) || unsupported.Operation != "Aggressor site delivery" || unsupported.Name != "site_unknown" || unsupported.Span != span {
		t.Fatalf("unknown site function = (%s, %#v), want typed UnsupportedError", result.Describe(), err)
	}

	var nilRuntime *Runtime
	result, err = nilRuntime.aggressorSite(context.Background(), Invocation{Name: "localip"})
	if !result.IsNull() || err == nil || err.Error() != "opfor: runtime is nil" {
		t.Fatalf("nil Runtime localip = (%s, %v), want null/runtime error", result.Describe(), err)
	}
}
