package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPortableScriptLoaderInheritsPayloadListenerAndPayloadStoreProviders(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-payload-listener.cna")
	if err := os.WriteFile(childPath, []byte(`
artifact_sign("child");
listener_info("child");
payloadstore_fetch("child");
`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-payload-listener.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
artifact_sign("parent");
listener_info("parent");
payloadstore_fetch("parent");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}

	payloadProvider := &recordingAggressorPayloadProvider{
		handle: func(_ context.Context, request AggressorPayloadRequest) (Value, error) {
			return request.Arg(0), nil
		},
	}
	listenerProvider := &recordingAggressorListenerProvider{
		handle: func(_ context.Context, request AggressorListenerRequest) (Value, error) {
			return request.Arg(0), nil
		},
	}
	storeProvider := &recordingAggressorPayloadStoreProvider{
		handle: func(_ context.Context, request AggressorPayloadStoreRequest) (Value, error) {
			return request.Arg(0), nil
		},
	}
	var hostCalls int
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls++
			return Null(), errors.New("provider was not inherited")
		})),
		WithAggressorPayloadProvider(payloadProvider),
		WithAggressorListenerProvider(listenerProvider),
		WithAggressorPayloadStoreProvider(storeProvider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls != 0 {
		t.Fatalf("inherited requests reached Host %d time(s)", hostCalls)
	}

	payloadRequests := payloadProvider.snapshot()
	listenerRequests := listenerProvider.snapshot()
	storeRequests := storeProvider.snapshot()
	if len(payloadRequests) != 2 || len(listenerRequests) != 2 || len(storeRequests) != 2 {
		t.Fatalf("parent/child request counts = payload %d listener %d store %d",
			len(payloadRequests), len(listenerRequests), len(storeRequests))
	}
	for _, pair := range [][]string{
		{payloadRequests[0].Arg(0).String(), payloadRequests[1].Arg(0).String()},
		{listenerRequests[0].Arg(0).String(), listenerRequests[1].Arg(0).String()},
		{storeRequests[0].Arg(0).String(), storeRequests[1].Arg(0).String()},
	} {
		if pair[0] != "parent" || pair[1] != "child" {
			t.Fatalf("parent/child arguments = %q", pair)
		}
	}
	parentID := runtimeInstance.ID()
	childID := payloadRequests[1].RuntimeID
	if parentID == 0 || childID == 0 || childID == parentID ||
		payloadRequests[0].RuntimeID != parentID || listenerRequests[0].RuntimeID != parentID || storeRequests[0].RuntimeID != parentID ||
		listenerRequests[1].RuntimeID != childID || storeRequests[1].RuntimeID != childID {
		t.Fatalf("parent/child RuntimeIDs = root %d, payload %d/%d, listener %d/%d, store %d/%d",
			parentID,
			payloadRequests[0].RuntimeID, payloadRequests[1].RuntimeID,
			listenerRequests[0].RuntimeID, listenerRequests[1].RuntimeID,
			storeRequests[0].RuntimeID, storeRequests[1].RuntimeID)
	}
	if payloadRequests[0].Script != 1 || listenerRequests[0].Script != 1 || storeRequests[0].Script != 1 ||
		payloadRequests[1].Script != 1 || listenerRequests[1].Script != 1 || storeRequests[1].Script != 1 ||
		payloadRequests[0].Span.Source != "parent-payload-listener.cna" || listenerRequests[0].Span.Source != "parent-payload-listener.cna" || storeRequests[0].Span.Source != "parent-payload-listener.cna" ||
		payloadRequests[1].Span.Source != filepath.ToSlash(childPath) || listenerRequests[1].Span.Source != filepath.ToSlash(childPath) || storeRequests[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = payload %#v, listener %#v, store %#v",
			payloadRequests, listenerRequests, storeRequests)
	}
}

func TestPayloadListenerAndPayloadStoreProvidersMayRunConcurrently(t *testing.T) {
	const workers = 8

	tests := []struct {
		name   string
		new    func(chan<- struct{}, <-chan struct{}) (*Runtime, error)
		invoke func(context.Context, *Runtime) (Value, error)
	}{
		{
			name: "payload",
			new: func(entered chan<- struct{}, release <-chan struct{}) (*Runtime, error) {
				return New(WithAggressorPayloadProvider(AggressorPayloadProviderFunc(func(ctx context.Context, _ AggressorPayloadRequest) (Value, error) {
					select {
					case entered <- struct{}{}:
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
					select {
					case <-release:
						return String("ok"), nil
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
				})))
			},
			invoke: func(ctx context.Context, runtimeInstance *Runtime) (Value, error) {
				return runtimeInstance.Invoke(ctx, "artifact_sign", String("bytes"))
			},
		},
		{
			name: "listener",
			new: func(entered chan<- struct{}, release <-chan struct{}) (*Runtime, error) {
				return New(WithAggressorListenerProvider(AggressorListenerProviderFunc(func(ctx context.Context, _ AggressorListenerRequest) (Value, error) {
					select {
					case entered <- struct{}{}:
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
					select {
					case <-release:
						return String("ok"), nil
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
				})))
			},
			invoke: func(ctx context.Context, runtimeInstance *Runtime) (Value, error) {
				return runtimeInstance.Invoke(ctx, "listener_info", String("listener"))
			},
		},
		{
			name: "payload-store",
			new: func(entered chan<- struct{}, release <-chan struct{}) (*Runtime, error) {
				return New(WithAggressorPayloadStoreProvider(AggressorPayloadStoreProviderFunc(func(ctx context.Context, _ AggressorPayloadStoreRequest) (Value, error) {
					select {
					case entered <- struct{}{}:
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
					select {
					case <-release:
						return String("ok"), nil
					case <-ctx.Done():
						return Null(), ctx.Err()
					}
				})))
			},
			invoke: func(ctx context.Context, runtimeInstance *Runtime) (Value, error) {
				return runtimeInstance.Invoke(ctx, "payloadstore_fetch", String("entry"))
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{}, workers)
			release := make(chan struct{})
			runtimeInstance, err := test.new(entered, release)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errorsChannel := make(chan error, workers)
			for index := 0; index < workers; index++ {
				go func() {
					result, invokeErr := test.invoke(ctx, runtimeInstance)
					if invokeErr == nil && result.String() != "ok" {
						invokeErr = fmt.Errorf("result = %s, want ok", result.Describe())
					}
					errorsChannel <- invokeErr
				}()
			}
			for index := 0; index < workers; index++ {
				select {
				case <-entered:
				case <-ctx.Done():
					close(release)
					t.Fatalf("only %d/%d callbacks entered concurrently: %v", index, workers, ctx.Err())
				}
			}
			close(release)
			for index := 0; index < workers; index++ {
				if invokeErr := <-errorsChannel; invokeErr != nil {
					t.Errorf("concurrent invocation: %v", invokeErr)
				}
			}
		})
	}
}
