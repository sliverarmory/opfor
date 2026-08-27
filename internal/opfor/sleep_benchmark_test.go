package opfor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

var benchmarkSleepResult Value

func benchmarkLoadedSleepFunction(
	b *testing.B,
	name string,
	code string,
	want int32,
	options ...Option,
) {
	b.Helper()
	program, err := CompileString(name+".sl", code)
	if err != nil {
		b.Fatalf("compile %s: %v", name, err)
	}
	options = append(options, WithStdout(io.Discard), WithStderr(io.Discard))
	runtimeInstance, err := New(options...)
	if err != nil {
		b.Fatalf("new runtime for %s: %v", name, err)
	}
	b.Cleanup(func() {
		if err := runtimeInstance.Close(context.Background()); err != nil {
			b.Errorf("close runtime for %s: %v", name, err)
		}
	})
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		b.Fatalf("load %s: %v", name, err)
	}

	ctx := context.Background()
	var result Value
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		result, err = script.Call(ctx, "benchmark")
		if err != nil {
			b.Fatalf("call %s: %v", name, err)
		}
	}
	b.StopTimer()
	benchmarkSleepResult = result
	if result.Int32() != want {
		b.Fatalf("%s result = %s, want %d", name, result.Describe(), want)
	}
}

func BenchmarkSleepArithmeticLoop(b *testing.B) {
	const source = `
sub benchmark {
    $sum = 0;
    for ($index = 0; $index < 1000; $index++) {
        $sum = $sum + $index;
    }
    return $sum;
}
`
	b.Run("unmetered", func(b *testing.B) {
		benchmarkLoadedSleepFunction(b, "arithmetic-loop", source, 499500)
	})
	b.Run("metered", func(b *testing.B) {
		benchmarkLoadedSleepFunction(b, "arithmetic-loop", source, 499500, WithInstructionLimit(100_000))
	})
}

func BenchmarkSleepFunctionCalls(b *testing.B) {
	const source = `
sub increment { return $1 + 1; }
sub benchmark {
    $value = 0;
    for ($index = 0; $index < 1000; $index++) {
        $value = increment($value);
    }
    return $value;
}
`
	b.Run("taint-disabled", func(b *testing.B) {
		benchmarkLoadedSleepFunction(b, "function-calls", source, 1000)
	})
	b.Run("taint-enabled", func(b *testing.B) {
		benchmarkLoadedSleepFunction(b, "function-calls", source, 1000, WithTaintMode(true))
	})
}

func BenchmarkSleepNativeCalls(b *testing.B) {
	const source = `
sub benchmark {
    $value = 0;
    for ($index = 0; $index < 1000; $index++) {
        $value = bench_increment($value);
    }
    return $value;
}
`
	native := WithFunction("bench_increment", func(_ context.Context, invocation Invocation) (Value, error) {
		if len(invocation.Arguments) == 0 {
			return Int(1), nil
		}
		return Int(invocation.Arguments[0].Resolve().Int32() + 1), nil
	})
	b.Run("taint-disabled", func(b *testing.B) {
		benchmarkLoadedSleepFunction(b, "native-calls", source, 1000, native)
	})
	b.Run("taint-enabled", func(b *testing.B) {
		benchmarkLoadedSleepFunction(b, "native-calls", source, 1000, native, WithTaintMode(true))
	})
}

func BenchmarkSleepArrayIndexRead(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "array-index-read", `
sub benchmark {
    @items = @(0, 1, 2, 3, 4, 5, 6, 7, 8, 9);
    $sum = 0;
    for ($index = 0; $index < 1000; $index++) {
        $sum = $sum + @items[$index % 10];
    }
    return $sum;
}
`, 4500)
}

func BenchmarkSleepArrayAppend(b *testing.B) {
	for _, size := range []int{1000, 2000, 10000} {
		size := size
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			benchmarkLoadedSleepFunction(b, fmt.Sprintf("array-append-%d", size), fmt.Sprintf(`
sub benchmark {
    @items = @();
    for ($index = 0; $index < %d; $index++) {
        push(@items, $index);
    }
    return size(@items);
}
`, size), int32(size))
		})
	}
}

func BenchmarkSleepForeachArray(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "foreach-array", `
sub benchmark {
    @items = @();
    for ($index = 0; $index < 1000; $index++) {
        push(@items, $index);
    }
    $sum = 0;
    foreach $item (@items) {
        $sum = $sum + $item;
    }
    return $sum;
}
`, 499500)
}

func BenchmarkSleepStringASCII(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "string-ascii", `
sub benchmark {
    $result = "";
    for ($index = 0; $index < 250; $index++) {
        $result = $result . "opfor";
    }
    return strlen($result);
}
`, 1250)
}

func BenchmarkSleepRegexRepeatedPattern(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "regex-repeated-pattern", `
sub benchmark {
    $matches = 0;
    for ($index = 0; $index < 1000; $index++) {
        if ("operator42@example.com" ismatch '[a-z]+[0-9]+\@[a-z]+\.[a-z]+') {
            $matches++;
        }
    }
    return $matches;
}
`, 1000)
}

func BenchmarkSleepLiteralLoop(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "literal-loop", `
sub benchmark {
    $sum = 0;
    for ($index = 0; $index < 1000; $index++) {
        $number = 12345;
        $text = 'literal';
        $sum = $sum + $number + strlen($text);
    }
    return $sum;
}
`, 12352000)
}

func BenchmarkSleepClosureLiteralLoop(b *testing.B) {
	benchmarkLoadedSleepFunction(b, "closure-literal-loop", `
sub benchmark {
    $sum = 0;
    for ($index = 0; $index < 1000; $index++) {
        $closure = { return 7; };
        $sum = $sum + invoke($closure);
    }
    return $sum;
}
`, 7000)
}

func BenchmarkSleepRuntimeLoadUnload(b *testing.B) {
	program, err := CompileString("runtime-load-unload.sl", "return 7;")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	var result Value
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		runtimeInstance, newErr := New(WithStdout(io.Discard), WithStderr(io.Discard))
		if newErr != nil {
			b.Fatal(newErr)
		}
		result, err = runtimeInstance.Execute(ctx, program)
		if err == nil {
			err = runtimeInstance.Close(ctx)
		}
		if err != nil {
			b.Fatalf("runtime lifecycle: %v", err)
		}
	}
	b.StopTimer()
	benchmarkSleepResult = result
	if result.Int32() != 7 {
		b.Fatalf("runtime lifecycle result = %s, want 7", result.Describe())
	}
}

func BenchmarkSleepCompileCorpus(b *testing.B) {
	paths, err := filepath.Glob(filepath.Join("testdata", "upstream", "sleep-2.1", "programs", "*.sl"))
	if err != nil {
		b.Fatal(err)
	}
	if len(paths) == 0 {
		b.Fatal("upstream Sleep corpus is empty")
	}
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			b.Fatal(readErr)
		}
		sources = append(sources, NewSource(filepath.Base(path), data))
	}

	compiled := 0
	b.ReportAllocs()
	b.ReportMetric(float64(len(sources)), "files/iteration")
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for _, source := range sources {
			if _, compileErr := Compile(source); compileErr == nil {
				compiled++
			}
		}
	}
	b.StopTimer()
	if compiled == 0 {
		b.Fatal("no upstream Sleep corpus programs compiled")
	}
}
