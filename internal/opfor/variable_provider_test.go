package opfor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type variableProviderTestFactory struct {
	request VariableContainerRequest
	parent  int
	id      int
}

type variableProviderTestEvent struct {
	operation VariableProviderOperation
	container int
	kind      VariableContainerKind
	access    VariableAccess
	cell      *Cell
}

type variableProviderTestProvider struct {
	mu sync.Mutex

	next            int
	factories       []variableProviderTestFactory
	containers      map[int]*variableProviderTestContainer
	factoryErr      map[VariableContainerKind]error
	nilFactory      map[VariableContainerKind]bool
	typedNilFactory map[VariableContainerKind]bool
	operationErr    map[string]error
	nilGet          map[string]bool
	seed            func(VariableContainerRequest) map[string]*Cell
	events          []variableProviderTestEvent
}

func newVariableProviderTestProvider() *variableProviderTestProvider {
	return &variableProviderTestProvider{
		containers:      make(map[int]*variableProviderTestContainer),
		factoryErr:      make(map[VariableContainerKind]error),
		nilFactory:      make(map[VariableContainerKind]bool),
		typedNilFactory: make(map[VariableContainerKind]bool),
		operationErr:    make(map[string]error),
		nilGet:          make(map[string]bool),
	}
}

func variableProviderTestErrorKey(operation VariableProviderOperation, name string) string {
	return string(operation) + "\x00" + normalizeVariableName(name)
}

func (provider *variableProviderTestProvider) CreateGlobalVariableContainer(
	_ context.Context,
	request VariableContainerRequest,
) (VariableContainer, error) {
	return provider.create(request, 0)
}

func (provider *variableProviderTestProvider) create(request VariableContainerRequest, parent int) (VariableContainer, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if err := provider.factoryErr[request.Kind]; err != nil {
		return nil, err
	}
	if provider.nilFactory[request.Kind] {
		return nil, nil
	}
	if provider.typedNilFactory[request.Kind] {
		var container *variableProviderTestContainer
		return container, nil
	}
	provider.next++
	container := &variableProviderTestContainer{
		provider: provider, id: provider.next, kind: request.Kind,
		cells: make(map[string]*Cell),
	}
	if provider.seed != nil {
		for name, cell := range provider.seed(request) {
			container.cells[normalizeVariableName(name)] = cell
		}
	}
	provider.containers[container.id] = container
	provider.factories = append(provider.factories, variableProviderTestFactory{
		request: request, parent: parent, id: container.id,
	})
	return container, nil
}

func (provider *variableProviderTestProvider) operationFailure(operation VariableProviderOperation, name string) (error, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	err := provider.operationErr[variableProviderTestErrorKey(operation, name)]
	return err, provider.nilGet[normalizeVariableName(name)]
}

func (provider *variableProviderTestProvider) record(event variableProviderTestEvent) {
	provider.mu.Lock()
	provider.events = append(provider.events, event)
	provider.mu.Unlock()
}

func (provider *variableProviderTestProvider) snapshot() ([]variableProviderTestFactory, []variableProviderTestEvent) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]variableProviderTestFactory(nil), provider.factories...), append([]variableProviderTestEvent(nil), provider.events...)
}

func (provider *variableProviderTestProvider) root(script ScriptID) *variableProviderTestContainer {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	for _, factory := range provider.factories {
		if factory.request.Kind == VariableContainerGlobal && factory.request.Script == script {
			return provider.containers[factory.id]
		}
	}
	return nil
}

type variableProviderTestContainer struct {
	provider *variableProviderTestProvider
	id       int
	kind     VariableContainerKind

	mu    sync.RWMutex
	cells map[string]*Cell
}

type variableProviderFatalLatchProvider struct {
	runtime   *Runtime
	operation VariableProviderOperation
	boom      error
	root      *variableProviderFatalLatchContainer
}

func (provider *variableProviderFatalLatchProvider) CreateGlobalVariableContainer(
	ctx context.Context,
	_ VariableContainerRequest,
) (VariableContainer, error) {
	return provider.root, provider.poison(ctx, VariableProviderCreateGlobal)
}

func (provider *variableProviderFatalLatchProvider) poison(ctx context.Context, operation VariableProviderOperation) error {
	if provider.operation != operation {
		return nil
	}
	_, _ = provider.runtime.Invoke(ctx, "println", String("xx"))
	return provider.boom
}

type variableProviderFatalLatchContainer struct {
	provider *variableProviderFatalLatchProvider

	mu    sync.Mutex
	cells map[string]*Cell
}

func (container *variableProviderFatalLatchContainer) ScalarExists(ctx context.Context, access VariableAccess) (bool, error) {
	container.mu.Lock()
	exists := container.cells[access.Name] != nil
	container.mu.Unlock()
	return exists, container.provider.poison(ctx, VariableProviderExists)
}

func (container *variableProviderFatalLatchContainer) GetScalar(ctx context.Context, access VariableAccess) (*Cell, error) {
	container.mu.Lock()
	cell := container.cells[access.Name]
	container.mu.Unlock()
	return cell, container.provider.poison(ctx, VariableProviderGet)
}

func (container *variableProviderFatalLatchContainer) PutScalar(ctx context.Context, access VariableAccess, cell *Cell) (*Cell, error) {
	container.mu.Lock()
	previous := container.cells[access.Name]
	container.cells[access.Name] = cell
	container.mu.Unlock()
	return previous, container.provider.poison(ctx, VariableProviderPut)
}

func (container *variableProviderFatalLatchContainer) RemoveScalar(ctx context.Context, access VariableAccess) error {
	container.mu.Lock()
	delete(container.cells, access.Name)
	container.mu.Unlock()
	return container.provider.poison(ctx, VariableProviderRemove)
}

func (container *variableProviderFatalLatchContainer) CreateLocalVariableContainer(
	ctx context.Context,
	_ VariableContainerRequest,
) (VariableContainer, error) {
	child := &variableProviderFatalLatchContainer{provider: container.provider, cells: make(map[string]*Cell)}
	return child, container.provider.poison(ctx, VariableProviderCreateLocal)
}

func (container *variableProviderFatalLatchContainer) CreateInternalVariableContainer(
	ctx context.Context,
	_ VariableContainerRequest,
) (VariableContainer, error) {
	child := &variableProviderFatalLatchContainer{provider: container.provider, cells: make(map[string]*Cell)}
	return child, container.provider.poison(ctx, VariableProviderCreateInternal)
}

func newVariableProviderFatalLatchFixture(t *testing.T, operation VariableProviderOperation, boom error) (*Runtime, context.Context, *scope, *variableProviderFatalLatchContainer) {
	t.Helper()
	provider := &variableProviderFatalLatchProvider{operation: operation, boom: boom}
	container := &variableProviderFatalLatchContainer{
		provider: provider,
		cells:    make(map[string]*Cell),
	}
	provider.root = container
	runtimeInstance, err := New(
		WithLimits(Limits{MaxOutputBytesPerRuntime: 1}),
		WithStdout(io.Discard),
		WithStderr(io.Discard),
		WithVariableProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	provider.runtime = runtimeInstance
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	executionCtx, release, err := runtimeInstance.acquireRuntimeExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	root := newVariableRootScope(runtimeInstance.ID(), ScriptID(77), container)
	return runtimeInstance, executionCtx, root, container
}

func assertVariableProviderFatalJoin(
	t *testing.T,
	err error,
	operation VariableProviderOperation,
	name string,
	boom error,
) {
	t.Helper()
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v, want fatal resource limit", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want provider error %v", err, boom)
	}
	var providerErr *VariableProviderError
	if !errors.As(err, &providerErr) || providerErr.Operation != operation || providerErr.Name != name {
		t.Fatalf("provider error = %#v, want operation %q name %q", providerErr, operation, name)
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("error = %T %v, want joined fatal and provider errors", err, err)
	}
	branches := joined.Unwrap()
	if len(branches) != 2 {
		t.Fatalf("joined branches = %#v, want fatal and provider errors", branches)
	}
	var limitErr *LimitError
	if !errors.As(branches[0], &limitErr) || limitErr.Resource != resourceOutputBytes || limitErr.Limit != 1 {
		t.Fatalf("first joined branch = %v, want output limit 1", branches[0])
	}
	var firstProviderErr *VariableProviderError
	if errors.As(branches[0], &firstProviderErr) {
		t.Fatalf("first joined branch = %v, want quota before provider error", branches[0])
	}
	if !errors.Is(branches[1], boom) {
		t.Fatalf("second joined branch = %v, want provider error %v", branches[1], boom)
	}
}

func (container *variableProviderTestContainer) ScalarExists(_ context.Context, access VariableAccess) (bool, error) {
	container.provider.record(variableProviderTestEvent{
		operation: VariableProviderExists, container: container.id, kind: container.kind, access: access,
	})
	if err, _ := container.provider.operationFailure(VariableProviderExists, access.Name); err != nil {
		return false, err
	}
	container.mu.RLock()
	cell := container.cells[access.Name]
	container.mu.RUnlock()
	return cell != nil, nil
}

func (container *variableProviderTestContainer) GetScalar(_ context.Context, access VariableAccess) (*Cell, error) {
	container.provider.record(variableProviderTestEvent{
		operation: VariableProviderGet, container: container.id, kind: container.kind, access: access,
	})
	if err, nilGet := container.provider.operationFailure(VariableProviderGet, access.Name); err != nil {
		return nil, err
	} else if nilGet {
		return nil, nil
	}
	container.mu.RLock()
	cell := container.cells[access.Name]
	container.mu.RUnlock()
	return cell, nil
}

func (container *variableProviderTestContainer) PutScalar(_ context.Context, access VariableAccess, cell *Cell) (*Cell, error) {
	container.provider.record(variableProviderTestEvent{
		operation: VariableProviderPut, container: container.id, kind: container.kind, access: access, cell: cell,
	})
	if err, _ := container.provider.operationFailure(VariableProviderPut, access.Name); err != nil {
		return nil, err
	}
	container.mu.Lock()
	previous := container.cells[access.Name]
	container.cells[access.Name] = cell
	container.mu.Unlock()
	return previous, nil
}

func (container *variableProviderTestContainer) RemoveScalar(_ context.Context, access VariableAccess) error {
	container.provider.record(variableProviderTestEvent{
		operation: VariableProviderRemove, container: container.id, kind: container.kind, access: access,
	})
	if err, _ := container.provider.operationFailure(VariableProviderRemove, access.Name); err != nil {
		return err
	}
	container.mu.Lock()
	delete(container.cells, access.Name)
	container.mu.Unlock()
	return nil
}

func (container *variableProviderTestContainer) CreateLocalVariableContainer(_ context.Context, request VariableContainerRequest) (VariableContainer, error) {
	return container.provider.create(request, container.id)
}

func (container *variableProviderTestContainer) CreateInternalVariableContainer(_ context.Context, request VariableContainerRequest) (VariableContainer, error) {
	return container.provider.create(request, container.id)
}

func (container *variableProviderTestContainer) cell(name string) *Cell {
	container.mu.RLock()
	defer container.mu.RUnlock()
	return container.cells[normalizeVariableName(name)]
}

func TestVariableProviderPreservesCellsAndSleepScopePrecedence(t *testing.T) {
	provider := newVariableProviderTestProvider()
	shared := NewCell(String("provider"))
	provider.seed = func(request VariableContainerRequest) map[string]*Cell {
		if request.Kind == VariableContainerGlobal {
			return map[string]*Cell{"$shared": shared}
		}
		return nil
	}
	runtimeInstance, err := New(WithVariableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	program, err := CompileString("variable-provider-scope.sl", `
$before = $shared;
$shared = "updated";
sub probe {
    local('$shared');
    $shared = "local";
    return $shared;
}
return @($before, probe(), $shared);
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := script.Result().Array()
	if !ok {
		t.Fatalf("result = %s, want array", script.Result().Describe())
	}
	items := values.Values()
	if len(items) != 3 || items[0].String() != "provider" || items[1].String() != "local" || items[2].String() != "updated" {
		t.Fatalf("scope values = %s", script.Result().Describe())
	}
	root := provider.root(script.ID())
	if root == nil {
		t.Fatal("provider did not retain the script root")
	}
	if got := root.cell("$shared"); got != shared {
		t.Fatalf("global cell identity = %p, want provider cell %p", got, shared)
	}
	if got := shared.Get().String(); got != "updated" {
		t.Fatalf("provider cell value = %q, want updated", got)
	}

	factories, events := provider.snapshot()
	haveLocal, haveInternal, haveLocalShadow := false, false, false
	for _, factory := range factories {
		if factory.request.RuntimeID != runtimeInstance.ID() || factory.request.Script != script.ID() {
			t.Fatalf("factory provenance = %#v, want runtime %d script %d", factory, runtimeInstance.ID(), script.ID())
		}
		switch factory.request.Kind {
		case VariableContainerLocal:
			haveLocal = true
		case VariableContainerInternal:
			haveInternal = true
		}
		if factory.request.Kind != VariableContainerGlobal && factory.parent != root.id {
			t.Fatalf("%s factory parent = %d, want global container %d", factory.request.Kind, factory.parent, root.id)
		}
	}
	for _, event := range events {
		if event.access.Name == "$shared" && event.operation == VariableProviderPut && event.kind == VariableContainerLocal {
			haveLocalShadow = true
		}
		if event.access.RuntimeID != runtimeInstance.ID() || event.access.Script != script.ID() {
			t.Fatalf("access provenance = %#v, want runtime %d script %d", event, runtimeInstance.ID(), script.ID())
		}
	}
	if !haveLocal || !haveInternal || !haveLocalShadow {
		t.Fatalf("factories/local shadow = local %v internal %v shadow %v", haveLocal, haveInternal, haveLocalShadow)
	}
}

func TestVariableProviderPublicOperationsIdentityUnsetAndCancellation(t *testing.T) {
	provider := newVariableProviderTestProvider()
	seed := NewCell(String("seed"))
	provider.seed = func(request VariableContainerRequest) map[string]*Cell {
		if request.Kind == VariableContainerGlobal {
			return map[string]*Cell{"$seed": seed}
		}
		return nil
	}
	runtimeInstance, err := New(
		WithVariableProvider(provider),
		WithInitialGlobals(map[string]Value{"$seed": String("initial")}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	program, err := CompileString("variable-provider-public.sl", `return $seed;`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	root := provider.root(script.ID())
	if root.cell("$seed") != seed || seed.Get().String() != "initial" {
		t.Fatalf("WithInitialGlobals replaced provider identity or value: %p %s", root.cell("$seed"), seed.Get().Describe())
	}
	if err := script.SetContext(context.Background(), "$seed", String("host")); err != nil {
		t.Fatal(err)
	}
	if root.cell("$seed") != seed || seed.Get().String() != "host" {
		t.Fatal("SetContext did not mutate the provider-owned cell in place")
	}
	bound := NewCell(Int(41))
	if err := script.BindVariable(context.Background(), "$bound", bound); err != nil {
		t.Fatal(err)
	}
	if root.cell("$bound") != bound {
		t.Fatalf("BindVariable cell = %p, want %p", root.cell("$bound"), bound)
	}
	if err := script.UnsetVariable(context.Background(), "$bound"); err != nil {
		t.Fatal(err)
	}
	if root.cell("$bound") != nil {
		t.Fatal("UnsetVariable left the provider binding installed")
	}
	if value, err := script.GetContext(context.Background(), "$seed"); err != nil || value.String() != "host" {
		t.Fatalf("GetContext = %s, %v", value.Describe(), err)
	}
	globals, err := script.GlobalsContext(context.Background())
	if err != nil || globals["$seed"].String() != "host" {
		t.Fatalf("GlobalsContext seed = %s, err %v", globals["$seed"].Describe(), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := script.GetContext(ctx, "$seed"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled GetContext error = %v", err)
	}

	provider.mu.Lock()
	provider.operationErr[variableProviderTestErrorKey(VariableProviderRemove, "$seed")] = errors.New("remove denied")
	provider.mu.Unlock()
	err = script.UnsetVariable(context.Background(), "$seed")
	var providerErr *VariableProviderError
	if !errors.As(err, &providerErr) || providerErr.Operation != VariableProviderRemove || providerErr.Name != "$seed" {
		t.Fatalf("remove error = %#v", err)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, before := provider.snapshot()
	if err := script.SetContext(context.Background(), "$seed", String("late")); !errors.Is(err, ErrScriptUnloaded) {
		t.Fatalf("SetContext after unload = %v", err)
	}
	_, after := provider.snapshot()
	if len(after) != len(before) {
		t.Fatal("SetContext consulted provider after unload")
	}
}

func TestVariableProviderErrorsAreTypedAuthoritativeAndProvenanced(t *testing.T) {
	sentinel := errors.New("provider rejected operation")
	tests := []struct {
		name      string
		operation VariableProviderOperation
		configure func(*variableProviderTestProvider)
		code      string
	}{
		{
			name: "global factory", operation: VariableProviderCreateGlobal, code: `return 1;`,
			configure: func(provider *variableProviderTestProvider) { provider.factoryErr[VariableContainerGlobal] = sentinel },
		},
		{
			name: "global nil container", operation: VariableProviderCreateGlobal, code: `return 1;`,
			configure: func(provider *variableProviderTestProvider) { provider.nilFactory[VariableContainerGlobal] = true },
		},
		{
			name: "internal factory", operation: VariableProviderCreateInternal, code: `return 1;`,
			configure: func(provider *variableProviderTestProvider) {
				provider.factoryErr[VariableContainerInternal] = sentinel
			},
		},
		{
			name: "local factory", operation: VariableProviderCreateLocal, code: `return 1;`,
			configure: func(provider *variableProviderTestProvider) { provider.factoryErr[VariableContainerLocal] = sentinel },
		},
		{
			name: "local typed nil container", operation: VariableProviderCreateLocal, code: `return 1;`,
			configure: func(provider *variableProviderTestProvider) { provider.typedNilFactory[VariableContainerLocal] = true },
		},
		{
			name: "exists", operation: VariableProviderExists, code: `return $boom;`,
			configure: func(provider *variableProviderTestProvider) {
				provider.operationErr[variableProviderTestErrorKey(VariableProviderExists, "$boom")] = sentinel
			},
		},
		{
			name: "get contract", operation: VariableProviderGet, code: `return $boom;`,
			configure: func(provider *variableProviderTestProvider) {
				provider.seed = func(request VariableContainerRequest) map[string]*Cell {
					if request.Kind == VariableContainerGlobal {
						return map[string]*Cell{"$boom": NewCell(String("value"))}
					}
					return nil
				}
				provider.nilGet["$boom"] = true
			},
		},
		{
			name: "put", operation: VariableProviderPut, code: `$boom = 1;`,
			configure: func(provider *variableProviderTestProvider) {
				provider.operationErr[variableProviderTestErrorKey(VariableProviderPut, "$boom")] = sentinel
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newVariableProviderTestProvider()
			test.configure(provider)
			runtimeInstance, err := New(WithVariableProvider(provider))
			if err != nil {
				t.Fatal(err)
			}
			defer runtimeInstance.Close(context.Background())
			program, err := CompileString("variable-provider-error.sl", test.code)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtimeInstance.Load(context.Background(), program)
			var providerErr *VariableProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error = %T %v, want VariableProviderError", err, err)
			}
			if providerErr.Operation != test.operation || providerErr.RuntimeID != runtimeInstance.ID() || providerErr.Script == 0 {
				t.Fatalf("provider error = %#v", providerErr)
			}
			if test.name != "get contract" && !strings.Contains(test.name, "nil container") && !errors.Is(err, sentinel) {
				t.Fatalf("error %v does not unwrap sentinel", err)
			}
			if test.operation != VariableProviderCreateGlobal && providerErr.Span.Source != "variable-provider-error.sl" {
				t.Fatalf("provider span = %s", providerErr.Span)
			}
		})
	}

	var typedNil *variableProviderTestProvider
	if _, err := New(WithVariableProvider(typedNil)); err == nil || !strings.Contains(err.Error(), "variable provider is nil") {
		t.Fatalf("typed nil provider error = %v", err)
	}
	var nilFunction VariableProviderFunc
	if _, err := New(WithVariableProvider(nilFunction)); err == nil || !strings.Contains(err.Error(), "variable provider is nil") {
		t.Fatalf("nil provider function error = %v", err)
	}
}

func TestVariableProviderCallbacksJoinFatalLatchBeforeProviderError(t *testing.T) {
	boom := errors.New("provider boom")
	span := Span{Source: "variable-provider-fatal-latch.sl"}
	tests := []struct {
		name      string
		operation VariableProviderOperation
		variable  string
		invoke    func(*testing.T, *Runtime, context.Context, *scope, *variableProviderFatalLatchContainer) error
	}{
		{
			name: "global factory", operation: VariableProviderCreateGlobal,
			invoke: func(t *testing.T, runtimeInstance *Runtime, ctx context.Context, _ *scope, _ *variableProviderFatalLatchContainer) error {
				created, err := runtimeInstance.createGlobalScope(ctx, ScriptID(88))
				if created != nil {
					t.Fatal("global scope was published after fatal provider callback")
				}
				return err
			},
		},
		{
			name: "local factory", operation: VariableProviderCreateLocal,
			invoke: func(t *testing.T, _ *Runtime, ctx context.Context, root *scope, _ *variableProviderFatalLatchContainer) error {
				child, err := root.localChildAt(ctx, span)
				if child != nil {
					t.Fatal("local scope was published after fatal provider callback")
				}
				return err
			},
		},
		{
			name: "internal child factory", operation: VariableProviderCreateInternal,
			invoke: func(t *testing.T, _ *Runtime, ctx context.Context, root *scope, _ *variableProviderFatalLatchContainer) error {
				child, err := root.internalChildAt(ctx, span)
				if child != nil {
					t.Fatal("internal child scope was published after fatal provider callback")
				}
				return err
			},
		},
		{
			name: "fork root factory", operation: VariableProviderCreateInternal,
			invoke: func(t *testing.T, runtimeInstance *Runtime, ctx context.Context, root *scope, _ *variableProviderFatalLatchContainer) error {
				forkRoot, err := root.forkRootAt(ctx, runtimeInstance.ID(), ScriptID(89), span)
				if forkRoot != nil {
					t.Fatal("fork root was published after fatal provider callback")
				}
				return err
			},
		},
		{
			name: "own-cell exists", operation: VariableProviderExists, variable: "$target",
			invoke: func(_ *testing.T, _ *Runtime, ctx context.Context, root *scope, _ *variableProviderFatalLatchContainer) error {
				_, _, err := root.ownCellAt(ctx, "$target", span)
				return err
			},
		},
		{
			name: "scalar exists", operation: VariableProviderExists, variable: "$target",
			invoke: func(_ *testing.T, _ *Runtime, ctx context.Context, root *scope, _ *variableProviderFatalLatchContainer) error {
				_, err := root.scalarExistsAt(ctx, "$target", span)
				return err
			},
		},
		{
			name: "get", operation: VariableProviderGet, variable: "$target",
			invoke: func(_ *testing.T, _ *Runtime, ctx context.Context, root *scope, container *variableProviderFatalLatchContainer) error {
				container.cells["$target"] = NewCell(String("provider"))
				_, _, err := root.ownCellAt(ctx, "$target", span)
				return err
			},
		},
		{
			name: "put", operation: VariableProviderPut, variable: "$target",
			invoke: func(_ *testing.T, _ *Runtime, ctx context.Context, root *scope, _ *variableProviderFatalLatchContainer) error {
				return root.putCellAt(ctx, "$target", NewCell(String("value")), span)
			},
		},
		{
			name: "remove", operation: VariableProviderRemove, variable: "$target",
			invoke: func(_ *testing.T, _ *Runtime, ctx context.Context, root *scope, container *variableProviderFatalLatchContainer) error {
				container.cells["$target"] = NewCell(String("value"))
				return root.removeOwnAt(ctx, "$target", span)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeInstance, ctx, root, container := newVariableProviderFatalLatchFixture(t, test.operation, boom)
			err := test.invoke(t, runtimeInstance, ctx, root, container)
			assertVariableProviderFatalJoin(t, err, test.operation, test.variable, boom)
		})
	}
}

func TestVariableProviderFatalLatchStopsKnownMetadataMutation(t *testing.T) {
	span := Span{Source: "variable-provider-fatal-metadata.sl"}

	t.Run("exists does not forget known cell", func(t *testing.T) {
		_, ctx, root, _ := newVariableProviderFatalLatchFixture(t, VariableProviderExists, nil)
		known := NewCell(String("known"))
		root.known["$target"] = known
		_, _, err := root.ownCellAt(ctx, "$target", span)
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want fatal resource limit", err)
		}
		if root.known["$target"] != known {
			t.Fatal("ScalarExists fatal latch mutated known metadata")
		}
	})

	t.Run("get does not replace known cell", func(t *testing.T) {
		_, ctx, root, container := newVariableProviderFatalLatchFixture(t, VariableProviderGet, nil)
		known := NewCell(String("known"))
		providerCell := NewCell(String("provider"))
		root.known["$target"] = known
		container.cells["$target"] = providerCell
		_, _, err := root.ownCellAt(ctx, "$target", span)
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want fatal resource limit", err)
		}
		if root.known["$target"] != known {
			t.Fatal("GetScalar fatal latch replaced known metadata")
		}
	})

	t.Run("put does not add known cell", func(t *testing.T) {
		_, ctx, root, container := newVariableProviderFatalLatchFixture(t, VariableProviderPut, nil)
		cell := NewCell(String("provider commit"))
		err := root.putCellAt(ctx, "$target", cell, span)
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want fatal resource limit", err)
		}
		if container.cells["$target"] != cell {
			t.Fatal("provider PutScalar commit was not retained")
		}
		if _, exists := root.known["$target"]; exists {
			t.Fatal("PutScalar fatal latch added known metadata")
		}
	})

	t.Run("remove does not delete known cell", func(t *testing.T) {
		_, ctx, root, container := newVariableProviderFatalLatchFixture(t, VariableProviderRemove, nil)
		cell := NewCell(String("provider commit"))
		root.known["$target"] = cell
		container.cells["$target"] = cell
		err := root.removeOwnAt(ctx, "$target", span)
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("error = %v, want fatal resource limit", err)
		}
		if container.cells["$target"] != nil {
			t.Fatal("provider RemoveScalar commit was not retained")
		}
		if root.known["$target"] != cell {
			t.Fatal("RemoveScalar fatal latch deleted known metadata")
		}
	})
}

func TestVariableProviderForkUsesParentInternalFactoryAsDetachedRoot(t *testing.T) {
	provider := newVariableProviderTestProvider()
	runtimeInstance, err := New(WithVariableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	program, err := CompileString("variable-provider-fork.sl", `
$parentOnly = 99;
$handle = fork({ return $parentOnly; });
return wait($handle);
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if !script.Result().IsNull() {
		t.Fatalf("fork inherited parent global: %s", script.Result().Describe())
	}
	factories, events := provider.snapshot()
	globalRequests := 0
	forkScript := ScriptID(0)
	rootID := 0
	for _, factory := range factories {
		if factory.request.Kind == VariableContainerGlobal {
			globalRequests++
			rootID = factory.id
		}
		if factory.request.Kind == VariableContainerInternal && factory.parent == rootID && factory.request.Script != script.ID() {
			forkScript = factory.request.Script
		}
	}
	if globalRequests != 1 || forkScript == 0 {
		t.Fatalf("fork factories = %#v", factories)
	}
	haveForkSource := false
	for _, event := range events {
		if event.operation == VariableProviderPut && event.access.Name == "$source" && event.access.Script == forkScript && event.access.RuntimeID == runtimeInstance.ID() {
			haveForkSource = true
		}
	}
	if !haveForkSource {
		t.Fatalf("fork root access provenance missing for script %d", forkScript)
	}
}

func TestVariableProviderForeachKeepsResolvedContainerAndElementCellIdentity(t *testing.T) {
	provider := newVariableProviderTestProvider()
	runtimeInstance, err := New(WithVariableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	program, err := CompileString("variable-provider-foreach.sl", `
@items = @("one", "two");
$destination = "before";
foreach $destination (@items) {
    $destination = uc($destination);
}
return $destination;
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	root := provider.root(script.ID())
	itemsCell := root.cell("@items")
	items, ok := itemsCell.Get().Array()
	if !ok {
		t.Fatalf("@items = %s, want array", itemsCell.Get().Describe())
	}
	first, firstOK := items.Cell(0)
	second, secondOK := items.Cell(1)
	if !firstOK || !secondOK {
		t.Fatal("@items lost an element during foreach")
	}
	if first.Get().String() != "ONE" || second.Get().String() != "TWO" {
		t.Fatalf("foreach aliases did not mutate array cells: %s, %s", first.Get().Describe(), second.Get().Describe())
	}
	if got := root.cell("$destination"); got != second {
		t.Fatalf("final destination cell = %p, want final array cell %p", got, second)
	}

	_, events := provider.snapshot()
	destinationPuts := make([]variableProviderTestEvent, 0, 3)
	for _, event := range events {
		if event.operation == VariableProviderPut && event.access.Name == "$destination" {
			destinationPuts = append(destinationPuts, event)
		}
	}
	if len(destinationPuts) != 3 {
		t.Fatalf("destination puts = %#v, want declaration and two iterations", destinationPuts)
	}
	if destinationPuts[1].container != root.id || destinationPuts[1].cell != first ||
		destinationPuts[2].container != root.id || destinationPuts[2].cell != second {
		t.Fatalf("foreach put identities = %#v, want root container cells %p and %p", destinationPuts, first, second)
	}
}

func TestVariableProviderScriptLoaderInheritanceAndHostIndependence(t *testing.T) {
	provider := newVariableProviderTestProvider()
	provider.seed = func(request VariableContainerRequest) map[string]*Cell {
		if request.Kind == VariableContainerGlobal {
			return map[string]*Cell{"$runtimeMarker": NewCell(String(fmt.Sprintf("%d", request.RuntimeID)))}
		}
		return nil
	}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithVariableProvider(provider),
		WithFunction("unrelated", func(context.Context, Invocation) (Value, error) {
			return String("native"), nil
		}),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("host"), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeInstance.Close(context.Background())
	program, err := CompileString("variable-provider-loader.sl", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "provider-child.sl", 'return $runtimeMarker;', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if result.String() == fmt.Sprintf("%d", runtimeInstance.ID()) || result.String() == "" {
		t.Fatalf("child runtime marker = %q, parent runtime %d", result.String(), runtimeInstance.ID())
	}
	factories, _ := provider.snapshot()
	runtimeIDs := make(map[RuntimeID]struct{})
	for _, factory := range factories {
		if factory.request.Kind == VariableContainerGlobal {
			runtimeIDs[factory.request.RuntimeID] = struct{}{}
		}
	}
	if len(runtimeIDs) != 2 {
		t.Fatalf("global factory RuntimeIDs = %#v, want parent and ScriptLoader child", runtimeIDs)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("variable access fell through Host %d times", hostCalls.Load())
	}
}
