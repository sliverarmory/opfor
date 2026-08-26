package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSourceByteLimitChargesCompilationAndProgramAdmission(t *testing.T) {
	t.Parallel()

	const code = `return 7;`
	limit := uint64(len(code))

	runtimeCompiled, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeCompiled.Close(context.Background()) })
	program, err := runtimeCompiled.CompileString("runtime-compiled.sl", code)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtimeCompiled.Execute(context.Background(), program)
	if err != nil || value.Int32() != 7 {
		t.Fatalf("Execute runtime-compiled Program = (%s, %v)", value.Describe(), err)
	}
	if _, err := runtimeCompiled.CompileString("over-limit.sl", code); err == nil {
		t.Fatal("second Runtime.CompileString unexpectedly fit source budget")
	} else {
		assertRuntimeResourceLimit(t, err, resourceSourceBytes, limit)
	}

	standalone, err := CompileString("standalone.sl", code)
	if err != nil {
		t.Fatal(err)
	}
	admissionRuntime, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admissionRuntime.Close(context.Background()) })
	value, err = admissionRuntime.Execute(context.Background(), standalone)
	if err != nil || value.Int32() != 7 {
		t.Fatalf("first standalone Program admission = (%s, %v)", value.Describe(), err)
	}
	if _, err := admissionRuntime.Execute(context.Background(), standalone); err == nil {
		t.Fatal("repeated standalone Program admission unexpectedly fit source budget")
	} else {
		assertRuntimeResourceLimit(t, err, resourceSourceBytes, limit)
	}
}

func TestSourceByteLimitRejectsScriptEnvironmentWrappersBeforeConstruction(t *testing.T) {
	t.Parallel()

	for _, message := range []string{"evaluatePredicate", "evaluateExpression", "evaluateParsedLiteral"} {
		t.Run(message, func(t *testing.T) {
			runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: 1}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			loader := &portableScriptLoader{runtime: runtimeInstance}
			instance := &portableScriptInstance{loader: loader}
			environment := &portableScriptEnvironment{instance: instance}
			instance.env = environment
			value, handled, invokeErr := environment.invoke(context.Background(), ObjectInvocation{
				Op:      ObjectInvoke,
				Message: message,
				Arguments: []Argument{
					{Value: String("x")},
				},
			})
			if !handled || !value.IsNull() {
				t.Fatalf("%s = (%s, handled %t), want null/true", message, value.Describe(), handled)
			}
			assertRuntimeResourceLimit(t, invokeErr, resourceSourceBytes, 1)
			if got := runtimeInstance.resources.used(resourceSourceBytes); got != 0 {
				t.Fatalf("%s rejected wrapper charged %d source bytes, want 0", message, got)
			}

			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()
			value, handled, invokeErr = environment.invoke(canceledCtx, ObjectInvocation{
				Op:      ObjectInvoke,
				Message: message,
				Arguments: []Argument{
					{Value: String("x")},
				},
			})
			if !handled || !value.IsNull() || !errors.Is(invokeErr, context.Canceled) {
				t.Fatalf("canceled %s = (%s, handled %t, %v), want null/true/context.Canceled", message, value.Describe(), handled, invokeErr)
			}
			if got := runtimeInstance.resources.used(resourceSourceBytes); got != 0 {
				t.Fatalf("canceled %s charged %d source bytes, want 0", message, got)
			}
		})
	}
}

func TestClosedRuntimeRejectsProgramBeforeSourceAdmission(t *testing.T) {
	t.Parallel()

	program, err := CompileString("closed-admission.sl", `return 7;`)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Load after Close = %v, want ErrRuntimeClosed", err)
	}
	if got := runtimeInstance.resources.used(resourceSourceBytes); got != 0 {
		t.Fatalf("closed-runtime admission charged %d source bytes, want 0", got)
	}
}

func TestCanceledEvalRejectsSourceBeforeAdmission(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: 1}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtimeInstance.Eval(ctx, "canceled-eval.sl", `return 7;`); !errors.Is(err, context.Canceled) {
		t.Fatalf("Eval with canceled context = %v, want context.Canceled", err)
	}
	if got := runtimeInstance.resources.used(resourceSourceBytes); got != 0 {
		t.Fatalf("canceled Eval charged %d source bytes, want 0", got)
	}
}

func TestSourceByteLimitIsConcurrentAndFatalAcrossSoftBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("concurrent compilation", func(t *testing.T) {
		const code = `return 1;`
		limit := uint64(len(code))
		runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		var successes atomic.Uint64
		var wait sync.WaitGroup
		for index := 0; index < 2; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if _, compileErr := runtimeInstance.CompileString("concurrent.sl", code); compileErr == nil {
					successes.Add(1)
				} else {
					var limitErr *LimitError
					if !errors.Is(compileErr, ErrResourceLimit) || !errors.As(compileErr, &limitErr) ||
						limitErr.Resource != resourceSourceBytes || limitErr.Limit != limit {
						t.Errorf("concurrent compilation error = %v", compileErr)
					}
				}
			}()
		}
		wait.Wait()
		if got := successes.Load(); got != 1 {
			t.Fatalf("successful concurrent source compilations = %d, want 1", got)
		}
	})

	t.Run("include does not soften quota", func(t *testing.T) {
		const child = `return 2;`
		const root = `debug(34); include("child.sl"); return 1;`
		limit := uint64(len(root) + len(child) - 1)
		resolver := SourceResolverFunc(func(_ context.Context, request SourceRequest) (Source, error) {
			if request.Name != "child.sl" {
				return Source{}, fmt.Errorf("unexpected source %q", request.Name)
			}
			return NewSource(request.Name, []byte(child)), nil
		})
		runtimeInstance, err := New(
			WithLimits(Limits{MaxSourceBytesPerRuntime: limit}),
			WithSourceResolver(resolver),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		_, err = runtimeInstance.Eval(context.Background(), "root.sl", root)
		assertRuntimeResourceLimit(t, err, resourceSourceBytes, limit)
	})

	t.Run("ScriptLoader child shares family source account", func(t *testing.T) {
		const dynamic = `return 2;`
		child := fmt.Sprintf(`eval(%q);`, dynamic)
		root := fmt.Sprintf(`$loader = [new ScriptLoader]; $child = [$loader loadScript: "child", %q, $null]; [$child runScript];`, child)
		limit := uint64(len(root) + len(child) + len(dynamic) - 1)
		runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		_, err = runtimeInstance.Eval(context.Background(), "loader-root.sl", root)
		assertRuntimeResourceLimit(t, err, resourceSourceBytes, limit)
	})

	t.Run("ScriptLoader compile does not soften quota", func(t *testing.T) {
		const child = `return 1;`
		root := fmt.Sprintf(`debug(34); $loader = [new ScriptLoader]; [$loader loadScript: "child", %q, $null];`, child)
		limit := uint64(len(root) + len(child) - 1)
		runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		_, err = runtimeInstance.Eval(context.Background(), "loader-compile-root.sl", root)
		assertRuntimeResourceLimit(t, err, resourceSourceBytes, limit)
	})

	t.Run("ScriptEnvironment compile does not soften quota", func(t *testing.T) {
		const child = `return 1;`
		const statement = `return 2;`
		root := fmt.Sprintf(`$loader = [new ScriptLoader]; $child = [$loader loadScript: "child", %q, $null]; [$child runScript]; $environment = [$child getScriptEnvironment]; [$environment evaluateStatement: %q];`, child, statement)
		limit := uint64(len(root) + len(child) + len(statement) - 1)
		runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		_, err = runtimeInstance.Eval(context.Background(), "environment-compile-root.sl", root)
		assertRuntimeResourceLimit(t, err, resourceSourceBytes, limit)
	})
}

func TestSourceByteLimitBoundsFilesystemArchiveAndScriptLoaderStreams(t *testing.T) {
	t.Parallel()

	const child = `return 2;`
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "child.sl"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, "scripts.jar")
	writeTestSourceArchive(t, archive, "pkg/child.sl", child)

	tests := []struct {
		name string
		root string
	}{
		{name: "file", root: `include("child.sl"); return 1;`},
		{name: "archive", root: `include("scripts.jar", "pkg/child.sl"); return 1;`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, err := NewFileSourceResolver(directory)
			if err != nil {
				t.Fatal(err)
			}
			limit := uint64(len(test.root) + len(child))
			runtimeInstance, err := New(
				WithLimits(Limits{MaxSourceBytesPerRuntime: limit}),
				WithSourceResolver(resolver),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			program, err := runtimeInstance.CompileString(test.name+"-root.sl", test.root)
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtimeInstance.Execute(context.Background(), program)
			if err != nil || value.Int32() != 1 {
				t.Fatalf("Execute = (%s, %v)", value.Describe(), err)
			}
			if got := runtimeInstance.resources.used(resourceSourceBytes); got != limit {
				t.Fatalf("used source bytes = %d, want %d", got, limit)
			}
		})
	}

	t.Run("ScriptLoader stream normalization", func(t *testing.T) {
		const streamSource = `return 3;`
		// ScriptLoader prepends one newline, so the compiler source is one byte
		// larger than the raw stream and must reserve that expansion.
		limit := uint64(len(streamSource) + 1)
		runtimeInstance, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		loader := &portableScriptLoader{runtime: runtimeInstance}
		program, err := loader.compilePortableScriptStream(
			context.Background(),
			"stream.sl",
			io.NopCloser(bytes.NewReader([]byte(streamSource))),
		)
		if err != nil {
			t.Fatal(err)
		}
		if program == nil || program.sourceAccount != runtimeInstance.resources {
			t.Fatal("ScriptLoader stream Program did not retain source-account admission")
		}
		if got := runtimeInstance.resources.used(resourceSourceBytes); got != limit {
			t.Fatalf("used stream source bytes = %d, want %d", got, limit)
		}
	})
}

func TestScriptLoaderCharsetExpansionReservesBeforeDecodeAllocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		charset          string
		data             []byte
		decodedLength    uint64
		normalizedLength uint64
	}{
		{
			name:             "windows-1252",
			charset:          "windows-1252",
			data:             []byte{'#', 0x80, '\n'},
			decodedLength:    5, // "#€\n"
			normalizedLength: 5,
		},
		{
			name:             "malformed UTF-8",
			charset:          "UTF-8",
			data:             []byte{'#', 0xff, '\n'},
			decodedLength:    5, // "#\ufffd\n"
			normalizedLength: 5,
		},
		{
			name:             "malformed UTF-16",
			charset:          "UTF-16",
			data:             []byte{0x00, '#', 0xff},
			decodedLength:    4, // "#\ufffd"
			normalizedLength: 5,
		},
		{
			name:             "malformed UTF-16LE",
			charset:          "UTF-16LE",
			data:             []byte{'#', 0x00, 0xff},
			decodedLength:    4, // "#\ufffd"
			normalizedLength: 5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rawLength := uint64(len(test.data))
			if test.decodedLength <= rawLength {
				t.Fatalf("test does not expand during decode: raw %d, decoded %d", rawLength, test.decodedLength)
			}

			rejectedRuntime, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: test.decodedLength - 1}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = rejectedRuntime.Close(context.Background()) })
			rejectedLoader := &portableScriptLoader{
				runtime:    rejectedRuntime,
				charset:    test.charset,
				charsetSet: true,
			}
			_, err = rejectedLoader.compilePortableScriptStream(
				context.Background(),
				"rejected-"+test.name+".sl",
				io.NopCloser(bytes.NewReader(test.data)),
			)
			assertRuntimeResourceLimit(t, err, resourceSourceBytes, test.decodedLength-1)
			if got := rejectedRuntime.resources.used(resourceSourceBytes); got != rawLength {
				t.Fatalf("rejected expansion charged %d source bytes, want admitted raw length %d", got, rawLength)
			}

			exactRuntime, err := New(WithLimits(Limits{MaxSourceBytesPerRuntime: test.normalizedLength}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = exactRuntime.Close(context.Background()) })
			exactLoader := &portableScriptLoader{
				runtime:    exactRuntime,
				charset:    test.charset,
				charsetSet: true,
			}
			program, err := exactLoader.compilePortableScriptStream(
				context.Background(),
				"exact-"+test.name+".sl",
				io.NopCloser(bytes.NewReader(test.data)),
			)
			if err != nil {
				t.Fatal(err)
			}
			if program == nil || program.sourceAccount != exactRuntime.resources {
				t.Fatal("exact-limit compile did not retain its source admission")
			}
			if got := exactRuntime.resources.used(resourceSourceBytes); got != test.normalizedLength {
				t.Fatalf("exact-limit compile charged %d source bytes, want %d", got, test.normalizedLength)
			}
		})
	}
}

func TestUnsupportedScriptLoaderCharsetStopsAfterOutputQuota(t *testing.T) {
	t.Parallel()

	data := []byte{'#', 0xff, '\n'}
	runtimeInstance, err := New(WithLimits(Limits{
		MaxOutputBytesPerRuntime: 1,
		MaxSourceBytesPerRuntime: 100,
	}), WithStderr(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	loader := &portableScriptLoader{
		runtime:    runtimeInstance,
		charset:    "not-a-real-charset",
		charsetSet: true,
	}
	_, err = loader.compilePortableScriptStream(
		context.Background(),
		"unsupported-charset.sl",
		io.NopCloser(bytes.NewReader(data)),
	)
	assertOutputLimitError(t, err, 1)
	if got := runtimeInstance.resources.used(resourceSourceBytes); got != uint64(len(data)) {
		t.Fatalf("source usage after fatal charset diagnostic = %d, want raw %d", got, len(data))
	}
}

func TestGunzipDecompressedByteLimitIsSharedAndConcurrent(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("quota"), 200)
	compressed := gzipAggressorTestBytes(t, payload)
	limit := uint64(len(payload))

	t.Run("exact and cumulative", func(t *testing.T) {
		runtimeInstance, err := New(WithLimits(Limits{MaxDecompressedBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		value, err := runtimeInstance.Invoke(context.Background(), "gunzip", BinaryString(compressed))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := value.Bytes()
		if !ok || !value.IsBinaryString() || !bytes.Equal(got, payload) {
			t.Fatalf("gunzip result = %x/binary=%v", got, value.IsBinaryString())
		}
		if _, err := runtimeInstance.Invoke(context.Background(), "gunzip", BinaryString(compressed)); err == nil {
			t.Fatal("second gunzip unexpectedly fit decompression budget")
		} else {
			assertRuntimeResourceLimit(t, err, resourceDecompressedBytes, limit)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		runtimeInstance, err := New(WithLimits(Limits{MaxDecompressedBytesPerRuntime: limit}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		var successes atomic.Uint64
		var wait sync.WaitGroup
		for index := 0; index < 2; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if _, gunzipErr := runtimeInstance.Invoke(context.Background(), "gunzip", BinaryString(compressed)); gunzipErr == nil {
					successes.Add(1)
				} else {
					var limitErr *LimitError
					if !errors.Is(gunzipErr, ErrResourceLimit) || !errors.As(gunzipErr, &limitErr) ||
						limitErr.Resource != resourceDecompressedBytes || limitErr.Limit != limit {
						t.Errorf("concurrent gunzip error = %v", gunzipErr)
					}
				}
			}()
		}
		wait.Wait()
		if got := successes.Load(); got != 1 {
			t.Fatalf("successful concurrent gunzip calls = %d, want 1", got)
		}
	})

	t.Run("ScriptLoader child", func(t *testing.T) {
		runtimeInstance, err := New(
			WithLimits(Limits{MaxDecompressedBytesPerRuntime: limit}),
			WithFunction("compressed_payload", func(context.Context, Invocation) (Value, error) {
				return BinaryString(compressed), nil
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

		if _, err := runtimeInstance.Invoke(context.Background(), "gunzip", BinaryString(compressed)); err != nil {
			t.Fatal(err)
		}
		const root = `$loader = [new ScriptLoader]; $child = [$loader loadScript: "child", 'gunzip(compressed_payload());', $null]; [$child runScript];`
		_, err = runtimeInstance.Eval(context.Background(), "gunzip-loader.sl", root)
		assertRuntimeResourceLimit(t, err, resourceDecompressedBytes, limit)
	})
}

func gzipAggressorTestBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	value, err := builtinAggressorGzip(context.Background(), aggressorPortableInvocation("gzip", BinaryString(payload)))
	if err != nil {
		t.Fatal(err)
	}
	compressed, ok := value.Bytes()
	if !ok {
		t.Fatalf("gzip result = %s, want bytes", value.Describe())
	}
	return compressed
}

func assertRuntimeResourceLimit(t *testing.T, err error, resource string, limit uint64) {
	t.Helper()
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("error = %v, want ErrResourceLimit", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != resource || limitErr.Limit != limit {
		t.Fatalf("LimitError = %+v, want resource %q limit %d", limitErr, resource, limit)
	}
}
