package opfor

import (
	"fmt"
	"testing"
)

var (
	benchmarkArrayLengthSink int
	benchmarkArrayValueSink  Value
	benchmarkArrayBoolSink   bool
	benchmarkArrayErrorSink  error
)

func TestArrayDirectAccessDoesNotAllocate(t *testing.T) {
	array := NewArray(Int(1), Int(2), Int(3))
	replacement := Int(4)

	tests := []struct {
		name string
		run  func()
	}{
		{
			name: "Len",
			run: func() {
				benchmarkArrayLengthSink = array.Len()
			},
		},
		{
			name: "Get",
			run: func() {
				benchmarkArrayValueSink, benchmarkArrayBoolSink = array.Get(1)
			},
		},
		{
			name: "Set",
			run: func() {
				benchmarkArrayErrorSink = array.Set(1, replacement)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if allocations := testing.AllocsPerRun(1000, test.run); allocations != 0 {
				t.Fatalf("%s allocations = %g, want 0", test.name, allocations)
			}
		})
	}
}

func TestArrayRootAppendFastPathPreservesViewSemantics(t *testing.T) {
	root := NewArray(Int(1))
	root.Append(Int(2), Int(3))

	storage, window := root.arrayStorage()
	storage.mu.RLock()
	if window != storage.root || len(storage.views) != 1 {
		storage.mu.RUnlock()
		t.Fatalf("root window setup = root %v, views %d; want root with one view", window == storage.root, len(storage.views))
	}
	if len(storage.items) == 0 || len(window.cached) == 0 || &storage.items[0] != &window.cached[0] {
		storage.mu.RUnlock()
		t.Fatal("root-only append did not retain the direct storage-backed cache")
	}
	storage.mu.RUnlock()

	view, err := root.sublist(0, 2)
	if err != nil {
		t.Fatalf("sublist: %v", err)
	}
	root.Append(Int(4))
	if err := view.viewError(); err != ErrUnsafeArrayView {
		t.Fatalf("view error after root append = %v, want %v", err, ErrUnsafeArrayView)
	}
	if got := root.Len(); got != 4 {
		t.Fatalf("root length after append = %d, want 4", got)
	}
}

func BenchmarkArrayDirectAccess(b *testing.B) {
	array := NewArray(Int(1), Int(2), Int(3), Int(4))
	replacement := Int(5)

	b.Run("Len", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			benchmarkArrayLengthSink = array.Len()
		}
	})
	b.Run("Get", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			benchmarkArrayValueSink, benchmarkArrayBoolSink = array.Get(2)
		}
	})
	b.Run("Set", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			benchmarkArrayErrorSink = array.Set(2, replacement)
		}
	})
}

func BenchmarkArrayRootAppend(b *testing.B) {
	for _, size := range []int{1000, 2000, 10000} {
		size := size
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				array := NewArray()
				for index := 0; index < size; index++ {
					array.Append(Int(int32(index)))
				}
				benchmarkArrayLengthSink = array.Len()
			}
			if benchmarkArrayLengthSink != size {
				b.Fatalf("array length = %d, want %d", benchmarkArrayLengthSink, size)
			}
		})
	}
}
