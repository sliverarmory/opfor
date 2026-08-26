// Package conformance provides a versioned, Team-Server-free compatibility
// suite for OPFOR importer adapters. It is intended primarily for use from an
// importing project's Go tests.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/sliverarmory/opfor"
)

const (
	// SuiteVersion identifies the behavioral contract exercised by Run. A
	// breaking probe change increments the major version; additive probes
	// increment the minor version.
	SuiteVersion = "1.0.0"

	hostCallbackCase  = "host/retained-callback-lifecycle"
	objectCase        = "object/opaque-construct-invoke"
	loadableCase      = "loadable/register-and-unload"
	errorCase         = "errors/authoritative-boundaries"
	lifecycleFailCase = "lifecycle/load-error-pairing"
)

// ErrProbe is returned by reference endpoints when a case exercises an
// authoritative importer failure. Adapters should preserve errors.Is when
// they wrap an in-process endpoint.
var ErrProbe = errors.New("opfor conformance probe error")

// Endpoints are reference importer implementations supplied to Factory. A
// direct adapter installs them as OPFOR options. A transport or controller
// adapter may proxy them, but must preserve the semantics exercised by Run.
type Endpoints struct {
	Host              opfor.Host
	ObjectHost        opfor.ObjectHost
	LoadableProvider  opfor.LoadableProvider
	LifecycleObserver opfor.ScriptLifecycleObserver
}

// Configuration is one fresh adapter instance. Options configure the Runtime
// under test. Close, when non-nil, runs after Runtime.Close has reached terminal
// quiescence. It receives a cancellation-detached copy of the case context and
// should release adapter-owned resources without retaining that context.
type Configuration struct {
	Options []opfor.Option
	Close   func(context.Context) error
}

// Factory creates a fresh adapter configuration for each conformance case.
// Implementations may be called repeatedly and should not retain Endpoints
// after Configuration.Close returns. Run deliberately does not recover panics
// from importer code; they remain ordinary Go test failures.
type Factory interface {
	Configure(context.Context, Endpoints) (Configuration, error)
}

// FactoryFunc adapts a function to Factory.
type FactoryFunc func(context.Context, Endpoints) (Configuration, error)

// Configure invokes function.
func (function FactoryFunc) Configure(ctx context.Context, endpoints Endpoints) (Configuration, error) {
	if function == nil {
		return Configuration{}, errors.New("opfor conformance: factory function is nil")
	}
	return function(ctx, endpoints)
}

// ReferenceAdapter is the in-process reference importer. It directly installs
// every supplied endpoint and is useful both as an executable example and as a
// control when diagnosing another adapter.
type ReferenceAdapter struct{}

// Configure installs the reference endpoints without an intermediate
// transport.
func (ReferenceAdapter) Configure(_ context.Context, endpoints Endpoints) (Configuration, error) {
	if endpoints.Host == nil || endpoints.ObjectHost == nil || endpoints.LoadableProvider == nil || endpoints.LifecycleObserver == nil {
		return Configuration{}, errors.New("opfor conformance: reference endpoint is nil")
	}
	return Configuration{Options: []opfor.Option{
		opfor.WithHost(endpoints.Host),
		opfor.WithObjectHost(endpoints.ObjectHost),
		opfor.WithLoadableProvider(endpoints.LoadableProvider),
		opfor.WithScriptLifecycleObserver(endpoints.LifecycleObserver),
	}}, nil
}

// Result is one stable conformance-case outcome.
type Result struct {
	Name string
	Err  error
}

// Report is the complete result of one SuiteVersion run. Results use the order
// returned by CaseNames.
type Report struct {
	Version string
	Results []Result
}

// Passed reports whether every case succeeded.
func (report Report) Passed() bool {
	return report.Err() == nil
}

// Err joins every failed case with its stable case name.
func (report Report) Err() error {
	errorsFound := make([]error, 0)
	for _, result := range report.Results {
		if result.Err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("%s: %w", result.Name, result.Err))
		}
	}
	return errors.Join(errorsFound...)
}

// CaseNames returns the stable case inventory for SuiteVersion.
func CaseNames() []string {
	return []string{hostCallbackCase, objectCase, loadableCase, errorCase, lifecycleFailCase}
}

type caseSpec struct {
	name    string
	prepare func(*probeState)
	run     func(context.Context, *opfor.Runtime, *probeState) error
}

// Run executes every conformance case against fresh Factory state. A nil
// context is treated as context.Background. Run performs no network, process,
// filesystem, UI, or Cobalt effects. Suite 1.0.0 executes its cases
// sequentially and does not establish adapter concurrency safety.
func Run(ctx context.Context, factory Factory) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	report := Report{Version: SuiteVersion}
	for _, spec := range suiteCases() {
		report.Results = append(report.Results, Result{Name: spec.name, Err: runCase(ctx, factory, spec)})
	}
	return report
}

func runCase(ctx context.Context, factory Factory, spec caseSpec) (resultErr error) {
	if isNilFactory(factory) {
		return errors.New("opfor conformance: factory is nil")
	}
	state := &probeState{}
	if spec.prepare != nil {
		spec.prepare(state)
	}
	configuration, err := factory.Configure(ctx, Endpoints{
		Host: state, ObjectHost: state, LoadableProvider: state, LifecycleObserver: state,
	})
	if err != nil {
		return fmt.Errorf("configure adapter: %w", err)
	}
	if configuration.Close != nil {
		defer func() {
			resultErr = errors.Join(resultErr, wrapCloseError("close adapter", configuration.Close(context.WithoutCancel(ctx))))
		}()
	}
	runtimeInstance, err := opfor.New(configuration.Options...)
	if err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	state.setRuntime(runtimeInstance)
	defer func() {
		// A caller cancellation must not let adapter resources disappear while
		// Runtime lifecycle or Loadable cleanup is still active. Background also
		// avoids carrying a foreign OPFOR execution token into terminal Close.
		resultErr = errors.Join(resultErr, wrapCloseError("close runtime", runtimeInstance.Close(context.Background())))
	}()
	return spec.run(ctx, runtimeInstance, state)
}

func isNilFactory(factory Factory) bool {
	if factory == nil {
		return true
	}
	value := reflect.ValueOf(factory)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func wrapCloseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func suiteCases() []caseSpec {
	return []caseSpec{
		{name: hostCallbackCase, run: runHostCallbackCase},
		{name: objectCase, run: runObjectCase},
		{name: loadableCase, run: runLoadableCase},
		{name: errorCase, run: runErrorCase},
		{
			name: lifecycleFailCase,
			prepare: func(state *probeState) {
				state.failLifecycleLoad = true
			},
			run: runLifecycleFailureCase,
		},
	}
}

type probeObject struct {
	seed string
}

type probeState struct {
	mu sync.Mutex

	runtime *opfor.Runtime

	hostCalls      int
	hostErrorCalls int
	callback       opfor.Callable

	objectConstructs int
	objectInvokes    int
	objectErrors     int

	loadableResolutions int
	loadableLoads       int
	loadableUnloads     int
	loadableCalls       int
	loadableScript      *opfor.Script

	lifecycleLoads       int
	lifecycleUnloads     int
	lifecycleScript      *opfor.Script
	failLifecycleLoad    bool
	lifecycleFailureSeen bool
}

func (state *probeState) setRuntime(runtimeInstance *opfor.Runtime) {
	state.mu.Lock()
	state.runtime = runtimeInstance
	state.mu.Unlock()
}

func (state *probeState) checkProvenance(runtimeInstance *opfor.Runtime, script opfor.ScriptID, source string) error {
	if runtimeInstance == nil || script == 0 {
		return errors.New("endpoint lost runtime or script provenance")
	}
	state.mu.Lock()
	wantRuntime := state.runtime
	state.mu.Unlock()
	if runtimeInstance != wantRuntime {
		return errors.New("endpoint received a different runtime")
	}
	if source == "" || !strings.HasPrefix(source, "conformance/") {
		return fmt.Errorf("endpoint source = %q, want conformance/*", source)
	}
	return nil
}

func (state *probeState) Call(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
	switch invocation.Name {
	case "opfor_conformance_host":
		if err := state.checkProvenance(invocation.Runtime, invocation.Script, invocation.Span.Source); err != nil {
			return opfor.Null(), err
		}
		if len(invocation.Arguments) != 1 {
			return opfor.Null(), fmt.Errorf("host arguments = %d, want 1", len(invocation.Arguments))
		}
		callback, err := invocation.Callback(0)
		if err != nil {
			return opfor.Null(), fmt.Errorf("retain callback: %w", err)
		}
		state.mu.Lock()
		state.hostCalls++
		state.callback = callback
		state.mu.Unlock()
		return opfor.String("host-ok"), nil
	case "opfor_conformance_error":
		state.mu.Lock()
		state.hostErrorCalls++
		state.mu.Unlock()
		return opfor.String("partial-host-result"), ErrProbe
	default:
		return opfor.Null(), &opfor.UnsupportedError{
			Operation: "conformance host function", Name: invocation.Name, Span: invocation.Span,
		}
	}
}

func (state *probeState) Object(_ context.Context, invocation opfor.ObjectInvocation) (opfor.Value, error) {
	if err := state.checkProvenance(invocation.Runtime, invocation.Script, invocation.Span.Source); err != nil {
		return opfor.Null(), err
	}
	if invocation.Op == opfor.ObjectConstruct && invocation.Class == "OPFORConformanceError" {
		state.mu.Lock()
		state.objectErrors++
		state.mu.Unlock()
		return opfor.Null(), ErrProbe
	}
	switch invocation.Op {
	case opfor.ObjectConstruct:
		if invocation.Class != "OPFORConformanceWidget" || len(invocation.Arguments) != 1 {
			return opfor.Null(), &opfor.UnsupportedError{
				Operation: "conformance object construction", Name: invocation.Class, Span: invocation.Span,
			}
		}
		state.mu.Lock()
		state.objectConstructs++
		state.mu.Unlock()
		return opfor.ObjectValue(&probeObject{seed: invocation.Arg(0).String()}), nil
	case opfor.ObjectInvoke:
		object, ok := invocation.Target.Object()
		widget, widgetOK := object.(*probeObject)
		if !ok || !widgetOK || widget == nil || invocation.Message != "echo" || len(invocation.Arguments) != 1 {
			return opfor.Null(), &opfor.UnsupportedError{
				Operation: "conformance object invocation", Name: invocation.Message, Span: invocation.Span,
			}
		}
		state.mu.Lock()
		state.objectInvokes++
		state.mu.Unlock()
		return opfor.String(widget.seed + ":" + invocation.Arg(0).String()), nil
	default:
		return opfor.Null(), &opfor.UnsupportedError{
			Operation: "conformance object operation", Name: invocation.Message, Span: invocation.Span,
		}
	}
}

func (state *probeState) ResolveLoadable(_ context.Context, request opfor.LoadableRequest) (opfor.LoadableBridge, error) {
	if request.ClassName != "OPFORConformanceBridge" {
		return nil, &opfor.UnsupportedError{Operation: "conformance Loadable", Name: request.ClassName, Span: request.Span}
	}
	if err := state.checkProvenanceForLoadable(request); err != nil {
		return nil, err
	}
	state.mu.Lock()
	state.loadableResolutions++
	state.mu.Unlock()
	return &probeLoadable{state: state}, nil
}

func (state *probeState) checkProvenanceForLoadable(request opfor.LoadableRequest) error {
	state.mu.Lock()
	runtimeInstance := state.runtime
	state.mu.Unlock()
	if runtimeInstance == nil || request.RuntimeID != runtimeInstance.ID() || request.Script == 0 ||
		request.Span.Source == "" || !strings.HasPrefix(request.Span.Source, "conformance/") {
		return fmt.Errorf("loadable provenance = runtime %d script %d span %s", request.RuntimeID, request.Script, request.Span)
	}
	if request.HasSource || request.ClassLiteral != true {
		return fmt.Errorf("loadable source/class-literal flags = %v/%v, want false/true", request.HasSource, request.ClassLiteral)
	}
	return nil
}

func (state *probeState) ScriptLoaded(ctx context.Context, script *opfor.Script) error {
	if script == nil || !script.Active() {
		return errors.New("lifecycle load received an inactive script")
	}
	state.mu.Lock()
	if state.lifecycleScript != nil && state.lifecycleScript != script {
		state.mu.Unlock()
		return errors.New("lifecycle load received a different script identity")
	}
	state.lifecycleScript = script
	state.lifecycleLoads++
	fail := state.failLifecycleLoad
	if fail {
		state.lifecycleFailureSeen = true
	}
	state.mu.Unlock()
	if fail {
		return ErrProbe
	}
	return script.SetContext(ctx, "$opfor_conformance_lifecycle", opfor.String("loaded"))
}

func (state *probeState) ScriptUnloaded(_ context.Context, script *opfor.Script) error {
	if script == nil || script.Active() {
		return errors.New("lifecycle unload received an active script")
	}
	if err := script.Set("$opfor_conformance_after_unload", opfor.String("invalid")); !errors.Is(err, opfor.ErrScriptUnloaded) {
		return fmt.Errorf("post-unload Script.Set error = %v, want ErrScriptUnloaded", err)
	}
	state.mu.Lock()
	if state.lifecycleScript == nil || state.lifecycleScript != script {
		state.mu.Unlock()
		return errors.New("lifecycle unload received a different script identity")
	}
	state.lifecycleUnloads++
	state.mu.Unlock()
	return nil
}

type probeLoadable struct {
	state *probeState
}

func (bridge *probeLoadable) ScriptLoaded(_ context.Context, script *opfor.Script) error {
	if bridge == nil || bridge.state == nil || script == nil || !script.Active() {
		return errors.New("loadable bridge received an inactive script")
	}
	bridge.state.mu.Lock()
	if bridge.state.loadableScript != nil && bridge.state.loadableScript != script {
		bridge.state.mu.Unlock()
		return errors.New("loadable bridge loaded a different script identity")
	}
	bridge.state.loadableScript = script
	bridge.state.loadableLoads++
	bridge.state.mu.Unlock()
	return script.RegisterFunction("opfor_conformance_loaded", func(_ context.Context, invocation opfor.Invocation) (opfor.Value, error) {
		if err := bridge.state.checkProvenance(invocation.Runtime, invocation.Script, invocation.Span.Source); err != nil {
			return opfor.Null(), err
		}
		bridge.state.mu.Lock()
		bridge.state.loadableCalls++
		bridge.state.mu.Unlock()
		return opfor.String("loadable-ok"), nil
	})
}

func (bridge *probeLoadable) ScriptUnloaded(_ context.Context, script *opfor.Script) error {
	if bridge == nil || bridge.state == nil {
		return errors.New("loadable bridge is nil")
	}
	if script == nil || script.Active() {
		return errors.New("loadable bridge unload received an active script")
	}
	bridge.state.mu.Lock()
	if bridge.state.loadableScript == nil || bridge.state.loadableScript != script {
		bridge.state.mu.Unlock()
		return errors.New("loadable bridge unload received a different script identity")
	}
	bridge.state.loadableUnloads++
	bridge.state.mu.Unlock()
	return nil
}

func runHostCallbackCase(ctx context.Context, runtimeInstance *opfor.Runtime, state *probeState) error {
	program, err := opfor.CompileString("conformance/host_callback.cna", `
sub exercise {
    return opfor_conformance_host(lambda({ return $1 . ":" . $2; }));
}
`)
	if err != nil {
		return fmt.Errorf("compile host probe: %w", err)
	}
	script, err := runtimeInstance.Load(ctx, program)
	if err != nil {
		return fmt.Errorf("load host probe: %w", err)
	}
	result, err := script.Call(ctx, "exercise")
	if err != nil || result.String() != "host-ok" {
		return fmt.Errorf("host result = %s, error %v; want host-ok", result.Describe(), err)
	}
	state.mu.Lock()
	callback := state.callback
	hostCalls := state.hostCalls
	state.mu.Unlock()
	if callback == nil || hostCalls != 1 {
		return fmt.Errorf("retained callback/calls = %v/%d, want non-nil/1", callback != nil, hostCalls)
	}
	callbackResult, err := callback.Invoke(ctx, opfor.String("left"), opfor.String("right"))
	if err != nil || callbackResult.String() != "left:right" {
		return fmt.Errorf("retained callback = %s, %v; want left:right", callbackResult.Describe(), err)
	}
	if err := script.Unload(ctx); err != nil {
		return fmt.Errorf("unload host probe: %w", err)
	}
	callbackResult, err = callback.Invoke(ctx, opfor.String("after"), opfor.String("unload"))
	if !callbackResult.IsNull() || !errors.Is(err, opfor.ErrScriptUnloaded) {
		return fmt.Errorf("revoked callback = %s, %v; want null/ErrScriptUnloaded", callbackResult.Describe(), err)
	}
	return nil
}

func runObjectCase(ctx context.Context, runtimeInstance *opfor.Runtime, state *probeState) error {
	program, err := opfor.CompileString("conformance/object.cna", `
$object = [new OPFORConformanceWidget: "seed"];
return [$object echo: "value"];
`)
	if err != nil {
		return fmt.Errorf("compile object probe: %w", err)
	}
	result, err := runtimeInstance.Execute(ctx, program)
	if err != nil || result.String() != "seed:value" {
		return fmt.Errorf("object result = %s, %v; want seed:value", result.Describe(), err)
	}
	state.mu.Lock()
	constructs, invokes := state.objectConstructs, state.objectInvokes
	state.mu.Unlock()
	if constructs != 1 || invokes != 1 {
		return fmt.Errorf("object operations = construct %d invoke %d, want 1/1", constructs, invokes)
	}
	return nil
}

func runLoadableCase(ctx context.Context, runtimeInstance *opfor.Runtime, state *probeState) error {
	program, err := opfor.CompileString("conformance/loadable.cna", `
use(^OPFORConformanceBridge);
sub exercise { return opfor_conformance_loaded(); }
`)
	if err != nil {
		return fmt.Errorf("compile loadable probe: %w", err)
	}
	script, err := runtimeInstance.Load(ctx, program)
	if err != nil {
		return fmt.Errorf("load loadable probe: %w", err)
	}
	result, err := script.Call(ctx, "exercise")
	if err != nil || result.String() != "loadable-ok" {
		return fmt.Errorf("loadable function = %s, %v; want loadable-ok", result.Describe(), err)
	}
	if err := script.Unload(ctx); err != nil {
		return fmt.Errorf("unload loadable probe: %w", err)
	}
	if script.Active() {
		return errors.New("loadable script remained active after unload")
	}
	state.mu.Lock()
	resolutions, loads := state.loadableResolutions, state.loadableLoads
	unloads, calls := state.loadableUnloads, state.loadableCalls
	state.mu.Unlock()
	if resolutions != 1 || loads != 1 || unloads != 1 || calls != 1 {
		return fmt.Errorf("loadable counts = resolve %d load %d unload %d call %d, want 1/1/1/1", resolutions, loads, unloads, calls)
	}
	if _, err := script.Call(ctx, "exercise"); !errors.Is(err, opfor.ErrScriptUnloaded) {
		return fmt.Errorf("post-unload Script.Call error = %v, want ErrScriptUnloaded", err)
	}
	return nil
}

func runErrorCase(ctx context.Context, runtimeInstance *opfor.Runtime, state *probeState) error {
	result, err := runtimeInstance.Invoke(ctx, "opfor_conformance_error")
	if result.String() != "partial-host-result" || !errors.Is(err, ErrProbe) {
		return fmt.Errorf("Host error result = %s, %v; want partial/ErrProbe", result.Describe(), err)
	}
	program, compileErr := opfor.CompileString("conformance/object_error.cna", `return [new OPFORConformanceError];`)
	if compileErr != nil {
		return fmt.Errorf("compile object error probe: %w", compileErr)
	}
	result, err = runtimeInstance.Execute(ctx, program)
	if !result.IsNull() || !errors.Is(err, ErrProbe) {
		return fmt.Errorf("ObjectHost error result = %s, %v; want null/ErrProbe", result.Describe(), err)
	}
	state.mu.Lock()
	hostCalls, objectCalls := state.hostErrorCalls, state.objectErrors
	state.mu.Unlock()
	if hostCalls != 1 || objectCalls != 1 {
		return fmt.Errorf("authoritative error calls = Host %d ObjectHost %d, want 1/1", hostCalls, objectCalls)
	}
	return nil
}

func runLifecycleFailureCase(ctx context.Context, runtimeInstance *opfor.Runtime, state *probeState) error {
	program, err := opfor.CompileString("conformance/lifecycle_error.cna", `return "body-must-not-run";`)
	if err != nil {
		return fmt.Errorf("compile lifecycle probe: %w", err)
	}
	script, loadErr := runtimeInstance.Load(ctx, program)
	if script != nil || !errors.Is(loadErr, ErrProbe) {
		return fmt.Errorf("lifecycle load = script %v, error %v; want nil/ErrProbe", script != nil, loadErr)
	}
	state.mu.Lock()
	loads, unloads := state.lifecycleLoads, state.lifecycleUnloads
	failureSeen := state.lifecycleFailureSeen
	state.mu.Unlock()
	if loads != 1 || unloads != 1 || !failureSeen {
		return fmt.Errorf("lifecycle failure counts = load %d unload %d seen %v, want 1/1/true", loads, unloads, failureSeen)
	}
	return nil
}
