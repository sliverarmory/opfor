package opfor

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestSleepRegexCacheReusesBoundsAndClearsPatterns(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}

	first, err := runtime.compileSleepRegexBridge(`a+`, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.compileSleepRegexBridge(`a+`, false)
	if err != nil {
		t.Fatal(err)
	}
	whole, err := runtime.compileSleepRegexBridge(`a+`, true)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first == whole {
		t.Fatalf("cache identity = (%p, %p, %p), want first == second != whole", first, second, whole)
	}

	for index := 0; index < sleepRegexCacheCapacity*2; index++ {
		if _, err := runtime.compileSleepRegexBridge(fmt.Sprintf(`item-%d`, index), false); err != nil {
			t.Fatal(err)
		}
	}
	if got := runtime.regexCache.len(); got != sleepRegexCacheCapacity {
		t.Fatalf("cache size = %d, want %d", got, sleepRegexCacheCapacity)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runtime.regexCache.len(); got != 0 {
		t.Fatalf("closed runtime cache size = %d, want 0", got)
	}
}

func TestSleepRegexCacheConcurrentPublication(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	const workers = 32
	results := make([]*sleepRegex, workers)
	errorsByWorker := make([]error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := range workers {
		go func() {
			defer wait.Done()
			results[index], errorsByWorker[index] = runtime.compileSleepRegexBridge(`(?<word>\\w+)-\\k<word>`, false)
		}()
	}
	wait.Wait()
	for index := range workers {
		if errorsByWorker[index] != nil {
			t.Fatalf("worker %d: %v", index, errorsByWorker[index])
		}
		if results[index] != results[0] {
			t.Fatalf("worker %d expression = %p, want published %p", index, results[index], results[0])
		}
	}
	if got := runtime.regexCache.len(); got != 1 {
		t.Fatalf("cache size = %d, want 1", got)
	}
}

func TestSleepRegexCacheDoesNotStoreFailures(t *testing.T) {
	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	if _, err := runtime.compileSleepRegexBridge("[", false); err == nil {
		t.Fatal("invalid pattern unexpectedly compiled")
	}
	if got := runtime.regexCache.len(); got != 0 {
		t.Fatalf("cache size after invalid pattern = %d, want 0", got)
	}
}
