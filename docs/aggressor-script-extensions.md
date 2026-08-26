# Aggressor Script extension and provider API

OPFOR implements Aggressor Script syntax, portable behavior, registration
lifecycle, callback guards, and typed request validation. An embedding
application supplies Cobalt-owned data and effects through the interfaces
linked below.

This file remains the stable landing page for existing links. Detailed
implementation material is split into the [grouped Aggressor guides](aggressor/README.md).
Generic streams, limits, initial globals, compilation, loading, and Host/object
configuration remain in the [OPFOR embedding guide](README.md).

## Implementation guide map

| Group | Implement this when your application owns | Guide |
| --- | --- | --- |
| Shared contract | Provider precedence, context, values, provenance, callbacks, errors, fallback, and testing | [Provider and callback contract](aggressor/provider-contract.md) |
| Query, state, and services | Sessions, data model/store, preferences, profile, VPN, and connected-client services | [Query, state, and services](aggressor/query-state-services.md) |
| Generation, binary, and storage | Artifacts, payloads, listeners, payload store, sites, PE/code transforms, process injection, BOF extraction/packing | [Generation, binary, and storage](aggressor/generation-binary-storage.md) |
| Beacon tasking and output | Beacon actions, low-level execution, Team Server RPC, and transcript publication | [Beacon tasking, execution, RPC, and transcript](aggressor/beacon-tasking-execution.md) |
| Client interaction | Client UI, dialogs, prompts, `brk`, and `dispatch_event` | [Client UI and execution control](aggressor/client-ui-execution-control.md) |
| Registries and script entry | Command/technique catalogs, bindings, observers, and host-driven dispatch | [Catalogs, bindings, and dispatch](aggressor/catalogs-bindings-dispatch.md) |

## Packages and inventories

The root `github.com/sliverarmory/opfor` package contains runtimes, options,
typed provider interfaces, request/result types, and callback capabilities.
The `github.com/sliverarmory/opfor/aggressor` package contains the generic Host
registry and compatibility catalog.

Use these inventories as the source of truth:

- `opfor.DefaultAggressorFunctionContracts()` returns 355 runtime-enforced
  typed-wrapper contracts at this revision;
- `opfor.DefaultFunctionNames()` returns the complete installed native
  namespace; and
- `aggressor.Catalog()` returns function, event, hook, popup-hook, evidence,
  support, and routing classifications.

See [machine-readable coverage](aggressor/README.md#machine-readable-coverage).

## Minimal typed provider

Every provider interface has a function adapter, so a small implementation can
be installed directly:

```go
provider := opfor.AggressorSessionQueryProviderFunc(func(
	ctx context.Context,
	query opfor.AggressorSessionQuery,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	return querySessionStore(query)
})

runtime, err := opfor.New(
	opfor.WithAggressorSessionQueryProvider(provider),
)
```

The complete implementation pattern is in the
[shared provider contract](aggressor/provider-contract.md#minimal-implementation-pattern).

## Dispatch and precedence

Exact-name `WithFunction` or registered functions win first. Portable
implementations run next. A typed wrapper then calls its configured provider;
only an absent provider falls back to `WithHost` when that contract allows it.
A configured provider error is authoritative and is never retried through
Host.

See [resolution and precedence](aggressor/provider-contract.md#resolution-and-precedence).

## Shared provider contract

The full cross-family contract is in
[provider-contract.md](aggressor/provider-contract.md).

### Context and concurrency

Providers are synchronous but may be called concurrently. Observe, but do not
retain, the supplied context. Retained capabilities receive a new context for
each later invocation. [Details](aggressor/provider-contract.md#context-and-concurrency)

### Values and request snapshots

Typed arguments are resolved once and top-level slices are detached. Compound
and object identity remains shared unless the provider copies it.
[Details](aggressor/provider-contract.md#request-snapshots-and-values)

### Provenance

Use `RuntimeID` plus `ScriptID` when correlating requests across runtimes.
`Span` identifies the script call site without exposing the evaluator.
[Details](aggressor/provider-contract.md#provenance)

### Callback and binding capabilities

Retained `Callable`, responder, popup, RPC, and `AggressorBindings`
capabilities are bound to the originating runtime and script generation.
[Details](aggressor/provider-contract.md#retained-callbacks)

### Results and errors

Typed contracts declare value, null, or predicate results. Provider errors are
authoritative. [Details](aggressor/provider-contract.md#results-and-errors)

### Request discriminators

Switch on exported `Kind` or `Operation` constants. Use `Name` only when exact
alias spelling matters. Each group guide maps its discriminator types and
request fields.

## Query, state, and service providers

Session queries, data-model queries, the client data store, preferences,
profile transformations, VPN state, and connected-client services are covered
in [Query, state, and services](aggressor/query-state-services.md).

## Generation, binary, and storage providers

Artifact/payload generation, listeners, payload storage, site delivery,
structure-aware PE operations, Cobalt-owned transforms, process-injection
selection, BOF extraction, and Beacon string encoding are covered in
[Generation, binary, and storage](aggressor/generation-binary-storage.md).

## Action, execution, RPC, and transcript APIs

The 121-function Beacon action family, seven low-level execution wrappers,
Team Server `call`, and eight transcript functions are covered in
[Beacon tasking, execution, RPC, and transcript](aggressor/beacon-tasking-execution.md).

## UI and execution-control providers

Client UI operations, dialog and prompt responders, breakpoint snapshots, and
event scheduling are covered in
[Client UI and execution control](aggressor/client-ui-execution-control.md).

## Catalog and registration APIs

Catalogs and registrations are metadata/lifecycle boundaries rather than
ordinary typed function providers. See
[Catalogs, bindings, and dispatch](aggressor/catalogs-bindings-dispatch.md).

### Command catalogs

`WithAggressorCommandCatalog` seeds immutable Beacon or SSH help metadata.
Script-owned command registrations layer over that base. The group guide
documents validation, snapshots, and unload restoration.

### Beacon technique catalogs

`WithAggressorBeaconTechniqueCatalog` seeds elevator, exploit,
remote-exec-method, or remote-exploit metadata. Only script registrations carry
executable callbacks. The group guide documents host fallback for technique
actions and exact callback invocation.

### Binding observation and host-driven entry

`WithBindingObserver`, `Runtime.Dispatch`, `Runtime.DispatchEvent`,
`Runtime.DispatchPopupHook`, `Runtime.InvokeBinding`,
`Runtime.InvokeBindingByID`, `Runtime.InvokeConsole`,
`Runtime.InvokeAggressorBeaconTechnique`, and `AggressorBindings` expose
controlled entry into script-owned events, hooks, popups, aliases, commands,
and key bindings. The group guide documents publication order, rollback,
one-shot `when`, and lifecycle revocation.

## Generic extension APIs

`WithFunction`, `WithHost`, `WithObjectHost`, custom environments, variable
providers, lifecycle observers, source/loadable providers, and taint policies
are generic OPFOR APIs. See the [embedding guide](README.md#generic-extension-points).

### Invocation ownership

Raw `Invocation` values retain reference-capable arguments and trusted
in-process capabilities. Prefer typed requests for known Aggressor families
and guarded callbacks for work that outlives the call. See
[request snapshots and values](aggressor/provider-contract.md#request-snapshots-and-values).

## The `aggressor` host adapter

`aggressor.NewHost` provides a concurrency-safe named fallback registry with
`Register`, `Unregister`, `SetFallback`, and sorted `Names`. Its requests retain
generic Host semantics. See
[Generic Host fallback](aggressor/provider-contract.md#generic-host-fallback).

## Implementation checklist

For each provider group:

1. cover every discriminator exposed by the machine-readable contracts;
2. enforce application-owned state and concurrency policy;
3. observe context cancellation;
4. handle presence flags and callback states explicitly;
5. return detached graphs where script mutation must not reach backing state;
6. retain only guarded callback/binding capabilities;
7. test typed success, absent-provider Host fallback, and provider-error
   no-retry behavior;
8. test unload/close revocation, parent/child provenance, and concurrent calls;
9. use `DefaultAggressorFunctionContracts()` and `aggressor.Catalog()` as drift
   checks.

The full reusable checklist and test matrix are in
[Coverage and testing](aggressor/provider-contract.md#coverage-and-testing).
