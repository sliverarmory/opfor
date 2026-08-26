package opfor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultOutputCanBeOverridden(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runtime, err := New(WithStdout(&stdout), WithStderr(&stderr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := runtime.Invoke(context.Background(), "println", String("hello")); err != nil {
		t.Fatalf("println: %v", err)
	}
	if _, err := runtime.Invoke(context.Background(), "warn", String("careful")); err != nil {
		t.Fatalf("warn: %v", err)
	}
	if got, want := stdout.String(), "hello\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(), "Warning: careful\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestInitialDebugFlagsAreConfigurablePerRuntime(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		options []Option
		want    int32
	}{
		{name: "reference default", want: 1},
		{name: "configured", options: []Option{WithDebugFlags(3)}, want: 3},
		{name: "disabled", options: []Option{WithDebugFlags(0)}, want: 0},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtime, err := New(test.options...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			result, err := runtime.Eval(context.Background(), "debug-flags.sl", "return debug();")
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got := result.Int32(); got != test.want {
				t.Fatalf("debug flags = %d, want %d", got, test.want)
			}
		})
	}
}

func TestNativeFunctionAndHostFallback(t *testing.T) {
	t.Parallel()

	hostCalls := 0
	runtime, err := New(
		WithFunction("native", func(_ context.Context, invocation Invocation) (Value, error) {
			return Int(invocation.Arg(0).Int32() + 1), nil
		}),
		WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			hostCalls++
			return String(invocation.Name), nil
		})),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	value, err := runtime.Invoke(context.Background(), "&native", Int(41))
	if err != nil || value.Int32() != 42 {
		t.Fatalf("native = (%v, %v), want (42, nil)", value, err)
	}
	value, err = runtime.Invoke(context.Background(), "host_api")
	if err != nil || value.String() != "host_api" || hostCalls != 1 {
		t.Fatalf("host = (%v, %v, calls=%d)", value, err, hostCalls)
	}
}

func TestRuntimeInvokePanicReleasesExecutionLease(t *testing.T) {
	tests := []struct {
		name    string
		options func(any) []Option
		invoke  string
	}{
		{
			name: "Host",
			options: func(panicValue any) []Option {
				return []Option{WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					panic(panicValue)
				}))}
			},
			invoke: "host_panic",
		},
		{
			name: "native function",
			options: func(panicValue any) []Option {
				return []Option{WithFunction("native_panic", func(context.Context, Invocation) (Value, error) {
					panic(panicValue)
				})}
			},
			invoke: "native_panic",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			panicValue := &struct{ name string }{name: test.name}
			runtimeInstance, err := New(test.options(panicValue)...)
			if err != nil {
				t.Fatal(err)
			}

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _ = runtimeInstance.Invoke(context.Background(), test.invoke)
			}()
			if recovered != panicValue {
				t.Fatalf("Invoke panic = %#v, want original panic %#v", recovered, panicValue)
			}

			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := runtimeInstance.Close(closeCtx); err != nil {
				t.Fatalf("Close after recovered Invoke panic: %v", err)
			}
		})
	}
}

func TestImporterErrorAuthorityComposesThroughNestedNativeCalls(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		t.Run(boundaryErr.Error(), func(t *testing.T) {
			hostCalls := 0
			runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				hostCalls++
				if invocation.Name != "host_fail" {
					t.Fatalf("unexpected Host call %q", invocation.Name)
				}
				return String("discarded Host partial result"), boundaryErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			_, err = runtimeInstance.Eval(context.Background(), "nested-boundary-error.sl", `map({ host_fail(); }, @("value"));`)
			if !errors.Is(err, boundaryErr) {
				t.Fatalf("nested Host error = %v, want authoritative %v", err, boundaryErr)
			}
			if hostCalls != 1 {
				t.Fatalf("Host calls = %d, want one", hostCalls)
			}
		})
	}
}

func TestNativeBoundaryErrorAuthorityIsScopedToInvocationTree(t *testing.T) {
	for _, test := range []struct {
		name        string
		boundaryErr error
		warning     string
	}{
		{name: "unsafe array view", boundaryErr: ErrUnsafeArrayView, warning: unsafeArrayViewWarning},
		{name: "read-only array", boundaryErr: ErrReadOnlyArray, warning: readOnlyArrayWarning},
		{name: "read-only hash", boundaryErr: ErrReadOnlyHash, warning: readOnlyHashWarning},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var diagnostics bytes.Buffer
			var runtimeInstance *Runtime
			var retained error
			runtimeInstance, err := New(
				WithStderr(&diagnostics),
				WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
					if invocation.Name != "host_fail" {
						t.Fatalf("unexpected Host call %q", invocation.Name)
					}
					return String("discarded Host partial result"), test.boundaryErr
				})),
				WithFunction("capture_boundary_error", func(ctx context.Context, _ Invocation) (Value, error) {
					_, nestedErr := runtimeInstance.Invoke(ctx, "host_fail")
					if !errors.Is(nestedErr, test.boundaryErr) {
						return Null(), errors.New("nested Host error lost importer authority")
					}
					retained = nestedErr
					return Null(), nil
				}),
				WithFunction("replay_boundary_error", func(context.Context, Invocation) (Value, error) {
					return String("discarded retained partial result"), retained
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			result, err := runtimeInstance.Invoke(context.Background(), "capture_boundary_error")
			if err != nil || !result.IsNull() || retained == nil {
				t.Fatalf("capture = (%s, %v), retained=%v; want null/nil/non-nil", result.Describe(), err, retained)
			}
			if got := diagnostics.String(); got != "" {
				t.Fatalf("capture diagnostics = %q, want none", got)
			}

			// Reusing the wrapped error in a later, independent native call must
			// not make that call look like the original importer boundary. It is
			// a local native error again and follows Sleep's warning translation.
			_, err = runtimeInstance.Eval(context.Background(), "stale-boundary-error.sl", `replay_boundary_error();`)
			if err != nil {
				t.Fatalf("replayed stale boundary error = %v, want native warning translation", err)
			}
			if got := diagnostics.String(); !strings.Contains(got, test.warning) {
				t.Fatalf("replay diagnostics = %q, want warning %q", got, test.warning)
			}
		})
	}
}

func TestNativeBoundaryErrorAuthoritySearchesEveryJoinedBranch(t *testing.T) {
	var runtimeInstance *Runtime
	var stale error
	runtimeInstance, err := New(
		WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
			switch invocation.Name {
			case "stale_host_error":
				return Null(), ErrReadOnlyArray
			case "current_host_error":
				return Null(), ErrUnsafeArrayView
			default:
				t.Fatalf("unexpected Host call %q", invocation.Name)
				return Null(), nil
			}
		})),
		WithFunction("capture_stale_error", func(ctx context.Context, _ Invocation) (Value, error) {
			_, stale = runtimeInstance.Invoke(ctx, "stale_host_error")
			return Null(), nil
		}),
		WithFunction("join_current_error_second", func(ctx context.Context, _ Invocation) (Value, error) {
			_, current := runtimeInstance.Invoke(ctx, "current_host_error")
			return Null(), errors.Join(stale, current)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	if _, err := runtimeInstance.Invoke(context.Background(), "capture_stale_error"); err != nil {
		t.Fatalf("capture stale error: %v", err)
	}
	_, err = runtimeInstance.Invoke(context.Background(), "join_current_error_second")
	if !errors.Is(err, ErrReadOnlyArray) || !errors.Is(err, ErrUnsafeArrayView) {
		t.Fatalf("joined boundary error = %v, want stale read-only and authoritative current unsafe errors", err)
	}
}

func TestUnsupportedHostCallIsExplicit(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = runtime.Invoke(context.Background(), "bshell", String("B-unsupported"), String("whoami"))
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Name != "bshell" {
		t.Fatalf("Invoke error = %v, want UnsupportedError for bshell", err)
	}
}

func TestPassByNameArgument(t *testing.T) {
	t.Parallel()

	cell := NewCell(Int(1))
	argument := Argument{Reference: cell}
	if got := argument.Resolve().Int32(); got != 1 {
		t.Fatalf("Resolve() = %d, want 1", got)
	}
	if !argument.Set(Int(2)) || cell.Get().Int32() != 2 {
		t.Fatal("pass-by-name argument did not mutate its cell")
	}
}
