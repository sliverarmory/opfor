package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func TestLoadableProviderCachesPerScriptInstallsFunctionsAndCleansUp(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []LoadableRequest
	var loaded []ScriptID
	var unloaded []ScriptID
	var unloadActive []bool
	var registerAfterUnload []error
	bridge := LoadableBridgeFuncs{
		Loaded: func(_ context.Context, script *Script) error {
			mu.Lock()
			loaded = append(loaded, script.ID())
			mu.Unlock()
			return script.RegisterFunction("bridge_probe", func(_ context.Context, invocation Invocation) (Value, error) {
				if len(invocation.Arguments) != 1 {
					return Null(), fmt.Errorf("bridge_probe: received %d arguments", len(invocation.Arguments))
				}
				if !invocation.Arguments[0].Set(String("mutated")) {
					return Null(), errors.New("bridge_probe: argument was not a reference")
				}
				return String(fmt.Sprintf("%s/%d/%s", invocation.Name, invocation.Script, invocation.Span.Source)), nil
			})
		},
		Unloaded: func(_ context.Context, script *Script) error {
			mu.Lock()
			unloaded = append(unloaded, script.ID())
			unloadActive = append(unloadActive, script.Active())
			registerAfterUnload = append(registerAfterUnload, script.RegisterFunction("too_late", func(context.Context, Invocation) (Value, error) {
				return Null(), nil
			}))
			mu.Unlock()
			return nil
		},
	}
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		return bridge, nil
	})
	runtimeInstance, err := New(WithLoadableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loadable-provider.sl", `
use("example.Bridge");
use("example.Bridge");
$target = "original";
$metadata = bridge_probe($target);
sub install_again {
	use("example.Bridge");
	return 1;
}
return @($target, $metadata);
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	result := mustArrayValues(t, script.Result())
	if got, want := argvValueStrings(result), []string{"mutated", fmt.Sprintf("bridge_probe/%d/loadable-provider.sl", script.ID())}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provider function result = %q, want %q", got, want)
	}
	if value, err := script.Call(context.Background(), "install_again"); err != nil || value.Int32() != 1 {
		t.Fatalf("install_again = (%s, %v), want 1", value.Describe(), err)
	}

	mu.Lock()
	if len(requests) != 1 {
		mu.Unlock()
		t.Fatalf("ResolveLoadable calls = %d, want one cached resolution", len(requests))
	}
	request := requests[0]
	gotLoaded := append([]ScriptID(nil), loaded...)
	mu.Unlock()
	if request.RuntimeID != runtimeInstance.ID() || request.Script != script.ID() || request.ClassName != "example.Bridge" ||
		request.HasSource || request.ClassLiteral || request.Span.Source != "loadable-provider.sl" {
		t.Fatalf("LoadableRequest = %#v", request)
	}
	if want := []ScriptID{script.ID(), script.ID(), script.ID()}; !reflect.DeepEqual(gotLoaded, want) {
		t.Fatalf("ScriptLoaded calls = %v, want %v", gotLoaded, want)
	}

	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []ScriptID{script.ID(), script.ID(), script.ID()}; !reflect.DeepEqual(unloaded, want) {
		t.Fatalf("ScriptUnloaded calls = %v, want reverse paired calls %v", unloaded, want)
	}
	if !reflect.DeepEqual(unloadActive, []bool{false, false, false}) {
		t.Fatalf("ScriptUnloaded active states = %v, want all false", unloadActive)
	}
	for index, err := range registerAfterUnload {
		if !errors.Is(err, ErrScriptUnloaded) {
			t.Errorf("post-unload RegisterFunction[%d] error = %v, want ErrScriptUnloaded", index, err)
		}
	}
}

func TestLoadableProviderReceivesResolvedSourceAndSoftErrors(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	archive := filepath.Join(directory, "bridge.jar")
	if err := os.WriteFile(archive, []byte("provider-owned fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	loadErr := errors.New("provider load failed")
	unloadErr := errors.New("provider cleanup failed")
	var gotRequest LoadableRequest
	var unloads int
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		gotRequest = request
		return LoadableBridgeFuncs{
			Loaded: func(context.Context, *Script) error { return loadErr },
			Unloaded: func(context.Context, *Script) error {
				unloads++
				return unloadErr
			},
		}, nil
	})
	runtimeInstance, err := New(WithLoadableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loadable-source.sl", fmt.Sprintf(`
use(%s, "example.ArchiveBridge");
$problem = checkError();
return "$problem";
`, strconv.Quote(archive)))
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if got := script.Result().String(); got != loadErr.Error() {
		t.Fatalf("provider soft error = %q, want %q", got, loadErr)
	}
	if gotRequest.ClassName != "example.ArchiveBridge" || !gotRequest.HasSource || gotRequest.Source != archive ||
		gotRequest.ResolvedSource != archive || gotRequest.ClassLiteral {
		t.Fatalf("archive LoadableRequest = %#v", gotRequest)
	}
	if err := script.Unload(context.Background()); !errors.Is(err, unloadErr) {
		t.Fatalf("Unload error = %v, want %v", err, unloadErr)
	}
	if unloads != 1 {
		t.Fatalf("ScriptUnloaded calls = %d, want 1", unloads)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("Close repeated cleanup error: %v", err)
	}
}

func TestLoadableSourceSearchesSleepClasspathWithConcreteFileResolver(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	classPath := filepath.Join(root, "sleep-libs")
	if err := os.Mkdir(classPath, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(classPath, "bridge.jar")
	if err := os.WriteFile(archive, []byte("provider-owned fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewFileSourceResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	resolver.SetSleepClasspath("sleep-libs")
	var request LoadableRequest
	runtimeInstance, err := New(
		WithSourceResolver(resolver),
		WithLoadableProvider(LoadableProviderFunc(func(_ context.Context, got LoadableRequest) (LoadableBridge, error) {
			request = got
			return LoadableBridgeFuncs{}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if runtimeInstance.defaultFileResolver != nil || runtimeInstance.concreteFileSourceResolver() != resolver {
		t.Fatal("importer-supplied FileSourceResolver lost its distinct ownership or concrete lookup")
	}
	if _, err := runtimeInstance.Eval(context.Background(), "loadable-classpath.sl", `use("bridge.jar", "example.ClasspathBridge");`); err != nil {
		t.Fatal(err)
	}
	if !request.HasSource || request.Source != "bridge.jar" || request.ResolvedSource != archive || request.ClassName != "example.ClasspathBridge" {
		t.Fatalf("classpath LoadableRequest = %#v", request)
	}
}

func TestLoadableProviderClassLiteralAndScriptLoaderInheritance(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []LoadableRequest
	var unloads int
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		return LoadableBridgeFuncs{
			Loaded: func(_ context.Context, script *Script) error {
				return script.RegisterFunction("child_runtime_id", func(context.Context, Invocation) (Value, error) {
					return Long(int64(request.RuntimeID)), nil
				})
			},
			Unloaded: func(context.Context, *Script) error {
				mu.Lock()
				unloads++
				mu.Unlock()
				return nil
			},
		}, nil
	})
	directory := t.TempDir()
	childPath := filepath.Join(directory, "loadable-child.sl")
	if err := os.WriteFile(childPath, []byte(`
import example.*;
use(^ChildBridge);
return child_runtime_id();
`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := New(WithLoadableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loadable-parent.sl", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %s];
$result = [$child runScript];
[$loader unloadScript: $child];
return $result;
`, strconv.Quote(filepath.ToSlash(childPath))))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Execute(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	childRuntimeID := RuntimeID(result.Int64())
	if childRuntimeID == 0 || childRuntimeID == runtimeInstance.ID() {
		t.Fatalf("child runtime ID = %d, parent = %d", childRuntimeID, runtimeInstance.ID())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || requests[0].RuntimeID != childRuntimeID || !requests[0].ClassLiteral || requests[0].ClassName != "example.ChildBridge" {
		t.Fatalf("child Loadable requests = %#v", requests)
	}
	if unloads != 1 {
		t.Fatalf("child ScriptUnloaded calls = %d, want 1", unloads)
	}
}

func TestLoadableProviderSingleflightAndPerScriptCache(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	resolves := make(map[ScriptID]int)
	loads := make(map[ScriptID]int)
	provider := LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
		mu.Lock()
		resolves[request.Script]++
		mu.Unlock()
		once.Do(func() { close(entered) })
		<-release
		return LoadableBridgeFuncs{Loaded: func(_ context.Context, script *Script) error {
			mu.Lock()
			loads[script.ID()]++
			mu.Unlock()
			return nil
		}}, nil
	})
	runtimeInstance, err := New(WithLoadableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loadable-concurrent.sl", `sub install { use("example.Concurrent"); return 1; }`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	const calls = 8
	results := make(chan error, calls*2)
	for _, script := range []*Script{first, second} {
		for index := 0; index < calls; index++ {
			go func(script *Script) {
				value, err := script.Call(context.Background(), "install")
				if err == nil && value.Int32() != 1 {
					err = fmt.Errorf("install result = %s", value.Describe())
				}
				results <- err
			}(script)
		}
	}
	<-entered
	close(release)
	for index := 0; index < calls*2; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if resolves[first.ID()] != 1 || resolves[second.ID()] != 1 {
		t.Fatalf("per-script ResolveLoadable calls = %#v, want one each", resolves)
	}
	if loads[first.ID()] != calls || loads[second.ID()] != calls {
		t.Fatalf("per-script ScriptLoaded calls = %#v, want %d each", loads, calls)
	}
}

func TestLoadableRegisteredFunctionRebindsToForkScript(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var invocations []ScriptID
	var unloads int
	provider := LoadableProviderFunc(func(context.Context, LoadableRequest) (LoadableBridge, error) {
		return LoadableBridgeFuncs{
			Loaded: func(_ context.Context, script *Script) error {
				return script.RegisterFunction("bridge_script_id", func(_ context.Context, invocation Invocation) (Value, error) {
					mu.Lock()
					invocations = append(invocations, invocation.Script)
					mu.Unlock()
					return Long(int64(invocation.Script)), nil
				})
			},
			Unloaded: func(context.Context, *Script) error {
				mu.Lock()
				unloads++
				mu.Unlock()
				return nil
			},
		}, nil
	})
	runtimeInstance, err := New(WithLoadableProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("loadable-fork.sl", `
use("example.ForkBridge");
sub parent_bridge_id { return bridge_script_id(); }
return wait(fork({ return bridge_script_id(); }));
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	childID := ScriptID(script.Result().Int64())
	if childID == 0 || childID == script.ID() {
		t.Fatalf("fork bridge Script ID = %d, parent = %d", childID, script.ID())
	}
	parentValue, err := script.Call(context.Background(), "parent_bridge_id")
	if err != nil {
		t.Fatal(err)
	}
	if got := ScriptID(parentValue.Int64()); got != script.ID() {
		t.Fatalf("parent bridge Script ID = %d, want %d", got, script.ID())
	}
	mu.Lock()
	gotInvocations := append([]ScriptID(nil), invocations...)
	mu.Unlock()
	if want := []ScriptID{childID, script.ID()}; !reflect.DeepEqual(gotInvocations, want) {
		t.Fatalf("bridge invocation provenance = %v, want %v", gotInvocations, want)
	}
	if err := script.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if unloads != 1 {
		t.Fatalf("Loadable cleanup calls = %d, want parent use only", unloads)
	}
}

func TestLoadableProviderPrecedenceHostFallbackAndTypedNil(t *testing.T) {
	t.Parallel()

	t.Run("WithFunction override", func(t *testing.T) {
		called := false
		runtimeInstance, err := New(
			WithLoadableProvider(LoadableProviderFunc(func(context.Context, LoadableRequest) (LoadableBridge, error) {
				called = true
				return LoadableBridgeFuncs{}, nil
			})),
			WithFunction("use", func(context.Context, Invocation) (Value, error) {
				return String("override"), nil
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtimeInstance.Eval(context.Background(), "loadable-override.sl", `return use("example.Override");`)
		if err != nil || value.String() != "override" || called {
			t.Fatalf("override = (%s, %v), provider called = %v", value.Describe(), err, called)
		}
	})

	t.Run("Host fallback", func(t *testing.T) {
		var got Invocation
		runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			got = invocation
			return String("ignored by use"), nil
		})))
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtimeInstance.Eval(context.Background(), "loadable-host.sl", `return use("example.HostBridge");`)
		if err != nil || !value.IsNull() || got.Name != "use" || got.Arg(0).String() != "example.HostBridge" {
			t.Fatalf("Host fallback = (%s, %v), invocation = %#v", value.Describe(), err, got)
		}
	})

	t.Run("provider decline falls through to Host", func(t *testing.T) {
		var providerCalls int
		var hostCalls int
		runtimeInstance, err := New(
			WithLoadableProvider(LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
				providerCalls++
				return nil, &UnsupportedError{Operation: "Loadable class", Name: request.ClassName, Span: request.Span}
			})),
			WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				hostCalls++
				if invocation.Name != "use" || invocation.Arg(0).String() != "example.HostBridge" {
					return Null(), fmt.Errorf("unexpected Host invocation: %#v", invocation)
				}
				return String("ignored"), nil
			})),
		)
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtimeInstance.Eval(context.Background(), "loadable-decline.sl", `return use("example.HostBridge");`)
		if err != nil || !value.IsNull() || providerCalls != 1 || hostCalls != 1 {
			t.Fatalf("provider decline = (%s, %v), provider calls %d, Host calls %d", value.Describe(), err, providerCalls, hostCalls)
		}
	})

	t.Run("null is a class name value not a class literal", func(t *testing.T) {
		var got LoadableRequest
		runtimeInstance, err := New(WithLoadableProvider(LoadableProviderFunc(func(_ context.Context, request LoadableRequest) (LoadableBridge, error) {
			got = request
			return LoadableBridgeFuncs{}, nil
		})))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeInstance.Eval(context.Background(), "loadable-null.sl", `use($null);`); err != nil {
			t.Fatal(err)
		}
		if got.ClassLiteral || got.ClassName != "" {
			t.Fatalf("null LoadableRequest = %#v", got)
		}
	})

	t.Run("typed nil option", func(t *testing.T) {
		var provider *typedNilLoadableProvider
		if _, err := New(WithLoadableProvider(provider)); err == nil {
			t.Fatal("typed-nil Loadable provider unexpectedly accepted")
		}
	})

	t.Run("typed nil bridge", func(t *testing.T) {
		var bridge *typedNilLoadableBridge
		runtimeInstance, err := New(WithLoadableProvider(LoadableProviderFunc(func(context.Context, LoadableRequest) (LoadableBridge, error) {
			return bridge, nil
		})))
		if err != nil {
			t.Fatal(err)
		}
		value, err := runtimeInstance.Eval(context.Background(), "loadable-nil.sl", `
use("example.NilBridge");
$problem = checkError();
return "$problem";
`)
		if err != nil || value.String() != `opfor: Loadable provider returned a nil bridge for "example.NilBridge"` {
			t.Fatalf("typed-nil bridge error = (%q, %v)", value.String(), err)
		}
	})
}

type typedNilLoadableProvider struct{}

func (*typedNilLoadableProvider) ResolveLoadable(context.Context, LoadableRequest) (LoadableBridge, error) {
	return LoadableBridgeFuncs{}, nil
}

type typedNilLoadableBridge struct{}

func (*typedNilLoadableBridge) ScriptLoaded(context.Context, *Script) error   { return nil }
func (*typedNilLoadableBridge) ScriptUnloaded(context.Context, *Script) error { return nil }
