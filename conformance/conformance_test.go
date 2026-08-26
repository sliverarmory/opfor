package conformance_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sliverarmory/opfor"
	"github.com/sliverarmory/opfor/conformance"
)

type typedNilFactory struct{}

func (*typedNilFactory) Configure(context.Context, conformance.Endpoints) (conformance.Configuration, error) {
	panic("typed-nil factory method must not run")
}

func TestReferenceAdapterPassesVersionedSuite(t *testing.T) {
	t.Parallel()

	report := conformance.Run(context.Background(), conformance.ReferenceAdapter{})
	if report.Version != conformance.SuiteVersion {
		t.Fatalf("report version = %q, want %q", report.Version, conformance.SuiteVersion)
	}
	if err := report.Err(); err != nil || !report.Passed() {
		t.Fatalf("reference conformance = passed %v, error %v", report.Passed(), err)
	}
	wantNames := conformance.CaseNames()
	gotNames := make([]string, len(report.Results))
	for index, result := range report.Results {
		gotNames[index] = result.Name
		if result.Err != nil {
			t.Errorf("case %q: %v", result.Name, result.Err)
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("case order = %q, want %q", gotNames, wantNames)
	}
	wantNames[0] = "mutated"
	if conformance.CaseNames()[0] == "mutated" {
		t.Fatal("CaseNames returned shared mutable data")
	}
}

func TestFactoryFailureIsReportedForEveryStableCase(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("adapter setup failed")
	report := conformance.Run(context.Background(), conformance.FactoryFunc(func(context.Context, conformance.Endpoints) (conformance.Configuration, error) {
		return conformance.Configuration{}, sentinel
	}))
	if report.Passed() || !errors.Is(report.Err(), sentinel) {
		t.Fatalf("failure report = passed %v, error %v", report.Passed(), report.Err())
	}
	if len(report.Results) != len(conformance.CaseNames()) {
		t.Fatalf("failure results = %d, want %d", len(report.Results), len(conformance.CaseNames()))
	}
	for _, result := range report.Results {
		if !errors.Is(result.Err, sentinel) || !strings.Contains(result.Err.Error(), "configure adapter") {
			t.Errorf("case %q error = %v, want wrapped setup sentinel", result.Name, result.Err)
		}
	}
}

func TestNilFactoriesFailWithoutPanicking(t *testing.T) {
	t.Parallel()

	for name, factory := range map[string]conformance.Factory{
		"nil interface": nil,
		"nil function":  conformance.FactoryFunc(nil),
		"typed nil":     (*typedNilFactory)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			report := conformance.Run(nil, factory)
			if report.Passed() || report.Err() == nil || len(report.Results) != len(conformance.CaseNames()) {
				t.Fatalf("nil factory report = %#v, error %v", report, report.Err())
			}
		})
	}
}

func TestCanceledCaseReachesRuntimeQuiescenceBeforeAdapterClose(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var configurations atomic.Int32
	var firstRuntimeUnloaded atomic.Bool
	var firstAdapterClosed atomic.Bool

	factory := conformance.FactoryFunc(func(_ context.Context, endpoints conformance.Endpoints) (conformance.Configuration, error) {
		if configurations.Add(1) != 1 {
			return conformance.ReferenceAdapter{}.Configure(context.Background(), endpoints)
		}
		host := opfor.HostFunc(func(callCtx context.Context, invocation opfor.Invocation) (opfor.Value, error) {
			_, err := endpoints.Host.Call(callCtx, invocation)
			cancel()
			return opfor.String("forced-case-failure"), err
		})
		lifecycle := opfor.ScriptLifecycleFuncs{
			Loaded: endpoints.LifecycleObserver.ScriptLoaded,
			Unloaded: func(unloadCtx context.Context, script *opfor.Script) error {
				if err := unloadCtx.Err(); err != nil {
					return fmt.Errorf("runtime cleanup context is canceled: %w", err)
				}
				if err := endpoints.LifecycleObserver.ScriptUnloaded(unloadCtx, script); err != nil {
					return err
				}
				firstRuntimeUnloaded.Store(true)
				return nil
			},
		}
		return conformance.Configuration{
			Options: []opfor.Option{
				opfor.WithHost(host),
				opfor.WithObjectHost(endpoints.ObjectHost),
				opfor.WithLoadableProvider(endpoints.LoadableProvider),
				opfor.WithScriptLifecycleObserver(lifecycle),
			},
			Close: func(closeCtx context.Context) error {
				firstAdapterClosed.Store(true)
				if err := closeCtx.Err(); err != nil {
					return fmt.Errorf("adapter cleanup context is canceled: %w", err)
				}
				if !firstRuntimeUnloaded.Load() {
					return errors.New("adapter closed before runtime unload completed")
				}
				return nil
			},
		}, nil
	})

	report := conformance.Run(ctx, factory)
	if len(report.Results) == 0 || report.Results[0].Err == nil {
		t.Fatalf("canceled probe first result = %#v, want intentional case failure", report.Results)
	}
	if strings.Contains(report.Results[0].Err.Error(), "close runtime") ||
		strings.Contains(report.Results[0].Err.Error(), "close adapter") {
		t.Fatalf("canceled probe cleanup error = %v", report.Results[0].Err)
	}
	if !firstRuntimeUnloaded.Load() || !firstAdapterClosed.Load() {
		t.Fatalf("canceled cleanup = runtime unloaded %v adapter closed %v, want true/true",
			firstRuntimeUnloaded.Load(), firstAdapterClosed.Load())
	}
}

func TestAdapterCleanupErrorsRemainDiscoverable(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("adapter cleanup failed")
	report := conformance.Run(context.Background(), conformance.FactoryFunc(func(ctx context.Context, endpoints conformance.Endpoints) (conformance.Configuration, error) {
		configuration, err := conformance.ReferenceAdapter{}.Configure(ctx, endpoints)
		configuration.Close = func(context.Context) error { return sentinel }
		return configuration, err
	}))
	if report.Passed() || !errors.Is(report.Err(), sentinel) {
		t.Fatalf("cleanup report = passed %v, error %v", report.Passed(), report.Err())
	}
	for _, result := range report.Results {
		if !errors.Is(result.Err, sentinel) || !strings.Contains(result.Err.Error(), "close adapter") {
			t.Errorf("case %q cleanup error = %v, want wrapped sentinel", result.Name, result.Err)
		}
	}
}

func ExampleReferenceAdapter() {
	report := conformance.Run(context.Background(), conformance.ReferenceAdapter{})
	fmt.Println(report.Version, report.Passed())
	// Output: 1.0.0 true
}
