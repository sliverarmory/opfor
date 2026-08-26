# Shared Aggressor provider and callback contract

This contract applies to every implementation group in this directory. Group
guides document their request types and function surfaces; this page defines
how OPFOR calls them and what an importer may retain.

## Resolution and precedence

For a script function call, resolution is:

1. `WithFunction`, `Runtime.RegisterFunction`, or a script-owned exact-name
   function override;
2. an OPFOR portable implementation;
3. a native typed-provider wrapper;
4. `WithHost`, but only when the wrapper's typed provider is absent; and
5. `UnsupportedError` when no layer handles the call.

All 355 entries in `DefaultAggressorFunctionContracts()` use Host fallback
when their typed provider is absent. The fallback receives the original
`Invocation`, including live reference-capable arguments.

A configured typed provider is authoritative. OPFOR validates the typed route,
calls the provider once, and returns its error without retrying through Host.
This remains true when the provider returns `UnsupportedError`; an external
effect may already have happened.

These optional integrations replace an OPFOR-owned default rather than using
ordinary Host fallback:

- `WithAggressorBeaconTranscriptSink` replaces headless transcript output;
- `WithAggressorEventDispatcher` replaces synchronous `dispatch_event`;
- `WithAggressorBreakpointProvider` replaces headless `brk` presentation; and
- `WithBeaconStringEncoder` replaces UTF-8 for `bof_pack` format `z`.

Command and Beacon-technique catalog options seed metadata. They are not call
handlers and do not route errors to Host.

## Minimal implementation pattern

Every typed provider interface has a `Func` adapter:

```go
provider := opfor.AggressorPreferenceProviderFunc(func(
	ctx context.Context,
	request opfor.AggressorPreferenceRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}

	switch request.Operation {
	case opfor.AggressorPreferenceGet:
		return loadPreference(request.PreferenceName, request.DefaultValue)
	case opfor.AggressorPreferenceGetList:
		return opfor.ArrayValue(loadPreferenceList(request.PreferenceName)), nil
	case opfor.AggressorPreferenceSet:
		return opfor.Null(), storePreference(
			request.PreferenceName,
			request.PreferenceValue,
		)
	case opfor.AggressorPreferenceSetList:
		return opfor.Null(), storePreferenceList(
			request.PreferenceName,
			request.PreferenceValue,
		)
	default:
		return opfor.Null(), fmt.Errorf(
			"unsupported preference operation %q",
			request.Operation,
		)
	}
})

runtime, err := opfor.New(
	opfor.WithAggressorPreferenceProvider(provider),
)
```

Switch on the exported `Kind` or `Operation` discriminator. Use `Name` when an
alias spelling matters for logging or compatibility, not as the primary
dispatch key.

## Context and concurrency

Provider calls are synchronous: the script call waits for the provider to
return. Independent executions may call the same provider concurrently,
including when one provider instance is shared across runtimes or
`ScriptLoader` children.

An implementation must:

- make its own mutable state concurrency-safe;
- check or pass through `ctx` to cancellable work;
- return promptly after cancellation when practical; and
- never retain the supplied context after the method returns.

Retained callbacks, responders, popup composers, RPC callbacks, and
`AggressorBindings` methods accept a new caller-owned context for each later
invocation.

## Request snapshots and values

Typed wrappers resolve each source argument once. Their top-level argument
slices are detached from the raw `Invocation`; a pass-by-name `Cell` is not
exposed through an ordinary typed request.

`Value` ownership is intentionally shallow:

- scalar values are immutable;
- arrays, hashes, functions, and objects retain identity;
- nested compound graphs are not recursively cloned; and
- binary-string and taint provenance are preserved.

If a provider returns a compound value, script code may mutate or retain it.
Return a fresh graph when application-owned backing state must remain isolated.
If a provider retains a request value, it may also retain objects, callables,
or nested capabilities reachable through that value; coerce or copy it first
when that is not intended.

## Provenance

Most typed requests carry:

- `Name`, the exact normalized script-facing function spelling;
- `Kind` or `Operation`, the canonical discriminator;
- `RuntimeID`, a nonzero process-local runtime identity;
- `Script`, the originating runtime-local `ScriptID`; and
- `Span`, the source call site.

Use `(RuntimeID, ScriptID)` as the script provenance key when one provider
serves more than one runtime. Neither field exposes or retains a `*Runtime`.

## Omitted, null, and callback arguments

Do not infer omission from `Value.IsNull()` when the request supplies a
presence flag. Optional positions use one of:

- a `Has...` boolean paired with a `Value`;
- `HasArgument(index)` with an `Arguments` slice; or
- `AggressorCallbackState`.

`AggressorCallbackState` distinguishes:

- `AggressorCallbackOmitted`;
- `AggressorCallbackNull`; and
- `AggressorCallbackCallable`.

Only the callable state carries a valid retained `Callable`. Invalid non-null
callback values fail before typed provider dispatch.

## Retained callbacks

`Callable` is synchronous:

```go
type Callable interface {
	Invoke(context.Context, ...opfor.Value) (opfor.Value, error)
}
```

Callbacks in typed requests have already been retained under the exact
originating script generation. A provider may store them after returning and
invoke them more than once unless the group guide marks the capability
one-shot.

Use a fresh context on every invocation:

```go
value, err := request.Callback.Invoke(
	callbackContext,
	opfor.String("completed"),
)
```

After generation retirement, script unload, or runtime close, invocation
returns `ErrScriptUnloaded` or `ErrRuntimeClosed`. Never replace these guarded
callbacks with raw interpreter state.

Dialog and prompt responders are one-shot terminal capabilities. RPC callbacks
and ordinary retained `Callable`s are multi-shot unless their group guide says
otherwise. A popup composer pins one exact set of popup registrations and
returns `ErrAggressorPopupStale` rather than retargeting replacements.

## AggressorBindings

Nine typed request families carry `AggressorBindings`: artifact, Beacon action,
Beacon execution, payload, listener, process injection, client service, client
UI, and site requests.

The capability enters script-owned registrations in the exact originating
runtime and generation without exposing a `*Runtime`:

```text
func (opfor.AggressorBindings) Valid() bool
func (opfor.AggressorBindings) RuntimeID() opfor.RuntimeID
func (opfor.AggressorBindings) Same(opfor.AggressorBindings) bool
func (opfor.AggressorBindings) DispatchEvent(
	context.Context,
	string,
	...opfor.Value,
) ([]opfor.Value, error)
func (opfor.AggressorBindings) InvokeHook(
	context.Context,
	string,
	...opfor.Value,
) (opfor.Value, error)
func (opfor.AggressorBindings) InvokePopupHook(
	context.Context,
	string,
	...opfor.Value,
) (opfor.Value, error)
func (opfor.AggressorBindings) DispatchPopupHook(
	context.Context,
	string,
	...opfor.Value,
) ([]opfor.Value, error)
```

`Valid` reports structural provenance, not liveness. A retained capability can
remain valid while invocation correctly fails because its generation has
retired. The zero value returns `ErrAggressorBindingsUnavailable`.

## Results and errors

The machine-readable result policy is one of:

- `AggressorContractResultValue`: transfer the provider `Value` directly;
- `AggressorContractResultNull`: discard a successful provider value and return
  Sleep `$null`; or
- `AggressorContractResultPredicate`: normalize with Sleep truthiness.

Provider errors are authoritative and cross the native boundary once. Boundary
errors such as cancellation and lifecycle revocation retain their identity.
Group pages call out default fallbacks or result normalization that is specific
to one function.

## ScriptLoader children

Source-backed portable `ScriptLoader` children inherit the parent's active
extension profiles. For Aggressor integrations:

- provider, sink, encoder, and dispatcher identities are shared;
- the child has a new `RuntimeID` and its own script generations;
- dialog/prompt IDs, callback layers, bindings, and UI state are child-owned;
- command and technique catalogs inherit importer base metadata only, never a
  parent's script-owned layers; and
- an absent transcript sink stays absent so the child writes through its own
  console router.

Requests created by a child carry child provenance and child-bound callbacks,
even when the same Go provider instance also serves the parent.

## Generic Host fallback

For broad compatibility handling, the `aggressor` package provides a
concurrency-safe named Host registry:

```go
host := aggressor.NewHost()
if err := host.Register("operator_name", func(
	ctx context.Context,
	request aggressor.Request,
) (aggressor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	return opfor.String("alice"), nil
}); err != nil {
	return err
}

runtime, err := opfor.New(opfor.WithHost(host))
```

`aggressor.Host` also supports `Unregister`, `SetFallback`, and sorted `Names`.
Its `Request` retains raw Host semantics, including reference-capable arguments
and an opaque lifecycle-bound `aggressor.Runtime` capability. Prefer typed
providers for known families so validation and result policies remain explicit.

## Coverage and testing

Filter the runtime contract inventory rather than hand-maintaining provider
names:

```go
func providerContracts(provider string) []opfor.AggressorFunctionContract {
	var result []opfor.AggressorFunctionContract
	for _, contract := range opfor.DefaultAggressorFunctionContracts() {
		if contract.TypedProvider == provider {
			result = append(result, contract)
		}
	}
	return result
}
```

For every implemented provider family, test:

1. every contract name reaches the typed provider with its documented request;
2. each value/null/predicate result policy;
3. invalid arity, callback, and argument constraints stop before the provider;
4. an absent provider reaches Host with the raw `Invocation` exactly once;
5. a provider error never falls through to Host;
6. callback success and revocation after unload/close;
7. parent and `ScriptLoader` child provenance;
8. context cancellation and concurrent calls; and
9. mutation isolation for returned or retained compound values.

Use `aggressor.Catalog()` alongside the function contracts when the adapter
also handles generic Host functions, events, hooks, or popup hooks. The public
[`conformance`](../../conformance) package supplies reusable inert checks for
the generic Host/object/lifecycle portions of an adapter.

## Group guides

- [Query, state, and services](query-state-services.md)
- [Generation, binary, and storage](generation-binary-storage.md)
- [Beacon tasking, execution, RPC, and transcript](beacon-tasking-execution.md)
- [Client UI and execution control](client-ui-execution-control.md)
- [Catalogs, bindings, and dispatch](catalogs-bindings-dispatch.md)
