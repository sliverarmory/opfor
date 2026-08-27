package opfor

import (
	"context"
	"sync"
	"testing"
)

func TestScriptLoaderCacheSharesInvalidatesAndSeparatesModes(t *testing.T) {
	cache := NewScriptLoaderCache()
	firstRuntime, err := New(WithScriptLoaderCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	secondRuntime, err := New(WithScriptLoaderCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = firstRuntime.Close(context.Background())
		_ = secondRuntime.Close(context.Background())
	})

	firstLoader := &portableScriptLoader{runtime: firstRuntime, globalCache: true}
	secondLoader := &portableScriptLoader{runtime: secondRuntime, globalCache: true}
	first, err := firstLoader.compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondLoader.compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("shared compile programs = (%p, %p), want identical", first, second)
	}

	secondLoader.charset, secondLoader.charsetSet = "UTF-16LE", true
	differentCharset, err := secondLoader.compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if differentCharset == first {
		t.Fatal("different charset reused cached Program")
	}
	secondLoader.charset, secondLoader.charsetSet = "", false
	environmentRuntime, err := New(
		WithScriptLoaderCache(cache),
		WithEnvironment("cache_environment", EnvironmentOrdinary),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = environmentRuntime.Close(context.Background()) })
	environmentProgram, err := (&portableScriptLoader{runtime: environmentRuntime, globalCache: true}).compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if environmentProgram == first {
		t.Fatal("different registered parser environments reused cached Program")
	}

	changed, err := secondLoader.compileString("shared.sl", `return 8;`)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("changed content reused cached Program")
	}

	cache.touch("shared.sl")
	afterTouch, err := secondLoader.compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if afterTouch == first {
		t.Fatal("touch did not invalidate cached Program")
	}

	secondLoader.globalCache = false
	disabled, err := secondLoader.compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if disabled == afterTouch {
		t.Fatal("cache-disabled compile reused cached Program")
	}

	if err := firstRuntime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondLoader.globalCache = true
	stillShared, err := secondLoader.compileString("shared.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	if stillShared != afterTouch {
		t.Fatal("closing one runtime invalidated the shared cache")
	}
}

func TestScriptLoaderGlobalCacheRequiresCapability(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	loader := &portableScriptLoader{runtime: runtime}

	_, handled, err := loader.invoke(context.Background(), ObjectInvocation{
		Runtime:   runtime,
		Op:        ObjectInvoke,
		Message:   "setGlobalCache",
		Arguments: []Argument{{Value: Int(1)}},
	})
	if !handled || err == nil {
		t.Fatalf("setGlobalCache without capability = (handled=%v, err=%v), want unsupported", handled, err)
	}
}

func TestScriptLoaderCacheSharesProgramsNotRuntimeState(t *testing.T) {
	cache := NewScriptLoaderCache()
	firstRuntime, err := New(WithScriptLoaderCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	secondRuntime, err := New(WithScriptLoaderCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = firstRuntime.Close(context.Background())
		_ = secondRuntime.Close(context.Background())
	})

	firstLoader := &portableScriptLoader{runtime: firstRuntime}
	secondLoader := &portableScriptLoader{runtime: secondRuntime}
	for _, loader := range []*portableScriptLoader{firstLoader, secondLoader} {
		_, handled, err := loader.invoke(context.Background(), ObjectInvocation{
			Runtime:   loader.runtime,
			Op:        ObjectInvoke,
			Message:   "setGlobalCache",
			Arguments: []Argument{{Value: Int(1)}},
		})
		if !handled || err != nil {
			t.Fatalf("enable shared cache = (handled=%v, err=%v)", handled, err)
		}
	}

	const source = `$counter++; sub counter { return $counter; }`
	firstProgram, err := firstLoader.compileString("state.sl", source)
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, err := secondLoader.compileString("state.sl", source)
	if err != nil {
		t.Fatal(err)
	}
	if firstProgram != secondProgram {
		t.Fatal("selected runtimes did not reuse the immutable Program")
	}
	firstScript, err := firstRuntime.Load(context.Background(), firstProgram)
	if err != nil {
		t.Fatal(err)
	}
	secondScript, err := secondRuntime.Load(context.Background(), secondProgram)
	if err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]*Script{"first": firstScript, "second": secondScript} {
		value, err := script.Call(context.Background(), "counter")
		if err != nil || value.Int32() != 1 {
			t.Fatalf("%s runtime counter = (%s, %v), want 1", name, value.Describe(), err)
		}
	}
}

func TestScriptLoaderCacheConcurrentCompilePublishesOnce(t *testing.T) {
	cache := NewScriptLoaderCache()
	runtime, err := New(WithScriptLoaderCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	loader := &portableScriptLoader{runtime: runtime, globalCache: true}

	const workers = 24
	programs := make([]*Program, workers)
	errorsByWorker := make([]error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := range workers {
		go func() {
			defer wait.Done()
			programs[index], errorsByWorker[index] = loader.compileString("concurrent.sl", `return 42;`)
		}()
	}
	wait.Wait()
	for index := range workers {
		if errorsByWorker[index] != nil {
			t.Fatalf("worker %d: %v", index, errorsByWorker[index])
		}
		if programs[index] != programs[0] {
			t.Fatalf("worker %d program = %p, want %p", index, programs[index], programs[0])
		}
	}
	if got := cache.len(); got != 1 {
		t.Fatalf("cache size = %d, want 1", got)
	}
}
