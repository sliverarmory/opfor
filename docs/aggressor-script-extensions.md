# Aggressor Script extension and provider API

OPFOR implements the Sleep language, Aggressor Script syntax, registration
lifecycle, callback guards, and the portable parts of the standard function
surface. An embedding application supplies environment-specific data and
effects through the APIs in this document.

All integrations are in-process Go calls. OPFOR does not serialize provider
requests, define a transport, or require an external Aggressor Script host.
Applications install only the capabilities they support.

This reference covers APIs that add behavior, provide data, receive runtime
notifications, or invoke script-owned callbacks. General runtime settings such
as streams, clocks, limits, and debug flags remain in the
[embedding guide](README.md#embedding).

## Packages and inventories

The public API is split across two packages:

- `github.com/sliverarmory/opfor` contains the runtime, typed providers,
  request and response types, generic host interfaces, and runtime options.
- `github.com/sliverarmory/opfor/aggressor` contains a concurrency-safe generic
  host registry and the compatibility catalog.

Use these inventories when an adapter needs exact script-facing names rather
than a provider-family summary:

- `opfor.DefaultAggressorFunctionContracts()` returns the runtime-enforced
  function name, arity, callback positions, argument constraints, typed
  provider, result policy, fallback policy, and deprecation flag.
- `aggressor.Catalog()` returns the complete function, predicate, event, hook,
  and popup-hook catalog with support and routing classifications.
- `aggressor.Lookup`, `aggressor.Filter`, and
  `aggressor.FilterByBoundary` select entries from that catalog.
- [`aggressor/catalog.go`](../aggressor/catalog.go) defines the corresponding
  support, routing, and boundary classifications.

## Minimal typed provider

Every provider interface has a function adapter, so small integrations do not
need a dedicated type:

```go
package main

import (
	"context"
	"log"

	"github.com/sliverarmory/opfor"
)

func main() {
	actions := opfor.AggressorBeaconActionProviderFunc(func(
		ctx context.Context,
		action opfor.AggressorBeaconAction,
	) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Printf("action=%s target=%s", action.Name, action.Target.String())
		return nil
	})

	runtime, err := opfor.New(
		opfor.WithAggressorBeaconActionProvider(actions),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Close(context.Background())
}
```

Installing a provider makes it authoritative for its complete family. A
provider error is returned to the script call; OPFOR does not retry that call
through another provider or the generic host.

## Dispatch and precedence

Function resolution follows these layers:

1. `WithFunction` and runtime/script `RegisterFunction` entries are exact-name
   overrides and have highest precedence.
2. OPFOR runs a portable implementation when the function has one.
3. A function-specific wrapper validates and resolves a typed request, then
   invokes its configured provider.
4. For wrappers with generic fallback, an unconfigured provider sends the
   original `Invocation` to `WithHost`.
5. An unresolved call without a supporting host returns a typed
   `UnsupportedError` for Aggressor Script source.

Provider errors are authoritative because an external effect may already have
occurred. Returning `UnsupportedError` from a configured typed provider does
not ask OPFOR to retry through `Host`. If partial, per-name handling is needed,
use `WithFunction` for selected names or implement the family policy inside the
provider.

There are four important default-path exceptions:

- `WithAggressorBeaconTranscriptSink` replaces the synchronized stdout record
  presentation; it is not a `Host` fallback.
- `WithAggressorEventDispatcher` replaces the synchronous `dispatch_event`
  scheduler.
- `WithAggressorBreakpointProvider` replaces the headless `brk()` presentation.
- `WithBeaconStringEncoder` replaces the UTF-8 encoder used by `bof_pack`'s
  `z` format.

Catalog options seed metadata rather than handling calls. Their script-owned
layers and runtime invocation APIs are described under
[Catalog and registration APIs](#catalog-and-registration-apis).

## Shared provider contract

### Context and concurrency

Provider methods are synchronous, but independent script executions may call
the same provider concurrently. Implementations must provide their own
synchronization, observe `ctx`, and must not retain the supplied context after
the method returns. Retained callbacks accept a new caller-owned context for
each invocation.

### Values and request snapshots

Typed wrappers resolve each source argument once before invoking a provider.
Top-level argument slices are detached from `Invocation`, so no pass-by-name
`Cell` crosses a typed request boundary. Scalar `Value`s are immutable, but
arrays, hashes, functions, objects, binary provenance, and nested compound
identity are preserved. A provider that retains one of those values may also
retain capabilities reachable through it.

For a value-result contract, returned compound values are transferred directly
to script code. A null-result contract discards the provider value, while a
predicate-result contract converts it through Sleep truthiness. Return a
detached graph when script mutation must not affect application-owned state.

Optional positions use one of these representations:

- a `Has...` field paired with a `Value`;
- `HasArgument(index)` on request types with an `Arguments` slice; or
- `AggressorCallbackState`, whose values distinguish omitted, explicit
  `$null`, and callable arguments.

Do not infer omission from `Value.IsNull()` when a corresponding presence flag
exists.

### Provenance

Most typed requests contain:

- `Name`: the exact normalized function spelling used by the script;
- a canonical `Kind` or `Operation` discriminator;
- `RuntimeID`: a nonzero, process-local runtime identity;
- `Script`: the originating `ScriptID`; and
- `Span`: the source call site.

`RuntimeID` is suitable for correlation and does not expose a `*Runtime`.
`ScriptID` values are runtime-local, so correlate them with `RuntimeID` when a
provider serves multiple runtimes.

### Callback and binding capabilities

`Callable` is a synchronous interface:

```go
type Callable interface {
	Invoke(context.Context, ...Value) (Value, error)
}
```

`CallableFunc` adapts a Go function with that signature to `Callable`.

Callbacks placed in typed requests are already retained under the originating
script generation. They may be invoked after the provider returns, honor the
new invocation context, and reject calls after generation retirement, script
unload, or runtime close. The zero value is not callable.

Nine request families also include `AggressorBindings`, an opaque capability
for entering registrations in the exact originating runtime and generation:

```go
func (AggressorBindings) Valid() bool
func (AggressorBindings) RuntimeID() RuntimeID
func (AggressorBindings) Same(AggressorBindings) bool
func (AggressorBindings) DispatchEvent(context.Context, string, ...Value) ([]Value, error)
func (AggressorBindings) InvokeHook(context.Context, string, ...Value) (Value, error)
func (AggressorBindings) InvokePopupHook(context.Context, string, ...Value) (Value, error)
func (AggressorBindings) DispatchPopupHook(context.Context, string, ...Value) ([]Value, error)
```

`Valid` reports structural provenance, not current liveness. A retained
nonzero capability remains structurally valid after revocation, while its
invocation methods return `ErrScriptUnloaded` or `ErrRuntimeClosed`.

### Results and errors

The typed result visible to script code is one of:

- `AggressorContractResultValue`: transfer the provider's `Value`;
- `AggressorContractResultNull`: discard any provider value and return `$null`;
- `AggressorContractResultPredicate`: normalize through Sleep truthiness.

For each entry returned by `DefaultAggressorFunctionContracts`, the inventory
is authoritative for fields present in that record. Its detached
`AggressorFunctionContract` records contain `AggressorCallbackContract` and
`AggressorArgumentConstraint` entries, the `AggressorContractResult`,
typed-provider name, host-fallback flag, and deprecation state. It intentionally
does not describe the transcript sink, breakpoint, event dispatcher, host RPC,
string encoder, or catalog options, whose contracts are documented by their
dedicated types. Request-type documentation remains authoritative for retained
callback state when a contract record carries no callback metadata. All typed
provider errors currently use
`AggressorContractProviderErrorsAuthoritative`.

### Request discriminators

Typed requests use exported string discriminator types rather than asking an
adapter to switch on raw function names. Their constants and exact string
values are declared in [`api.go`](../api.go):

| Area | Discriminator types |
| --- | --- |
| Artifact and payload generation | `AggressorArtifactKind`, `AggressorPayloadOperation`, `AggressorPayloadStoreOperation` |
| Beacon actions and output | `AggressorBeaconActionKind`, `AggressorBeaconExecutionKind`, `AggressorBeaconTranscriptKind` |
| Catalogs | `AggressorCommandKind`, `AggressorBeaconTechniqueKind` |
| Client and UI | `AggressorClientServiceOperation`, `AggressorClientUIOperation`, `AggressorDialogRowKind`, `AggressorDialogButtonKind`, `AggressorPromptKind` |
| Data and state | `AggressorDataModelQueryKind`, `AggressorDataStoreOperation`, `AggressorPreferenceOperation`, `AggressorSessionQueryKind` |
| Binary and transformation | `AggressorCodeTransformOperation`, `AggressorPEOperation` |
| Runtime configuration | `AggressorListenerOperation`, `AggressorProcessInjectionOperation`, `AggressorProfileOperation`, `AggressorSiteKind`, `AggressorVPNOperation` |

Optional callbacks use `AggressorCallbackState` with
`AggressorCallbackOmitted`, `AggressorCallbackNull`, and
`AggressorCallbackCallable`. Provider code should switch on the discriminator
and use `Name` only when the exact alias spelling matters.

## Query, state, and service providers

Every interface below has the listed `Func` adapter with the same function
signature.

| Configuration | Interface and request | Script surface and result policy |
| --- | --- | --- |
| `WithAggressorSessionQueryProvider` | `AggressorSessionQueryProvider.QueryAggressorSession(context.Context, AggressorSessionQuery) (Value, error)`; adapter `AggressorSessionQueryProviderFunc`. `AggressorSessionQuery` carries `Kind`, `Name`, provenance, `SessionID`, and `Key`. | `beacons`, `beacon_ids`, `bdata`/`beacon_data`, `binfo`/`beacon_info`, `barch`, and the five session predicates. Values pass through; predicates are normalized and an empty `barch` uses the documented fallback. |
| `WithAggressorDataModelQueryProvider` | `AggressorDataModelQueryProvider.QueryAggressorDataModel(context.Context, AggressorDataModelQuery) (Value, error)`; adapter `AggressorDataModelQueryProviderFunc`. The query carries `Kind`, `Name`, provenance, and `Key`. | `data_keys`, `data_query`, and `pivots`. Results pass through without sorting, validation, coercion, or cloning. |
| `WithAggressorDataStoreProvider` | `AggressorDataStoreProvider.HandleAggressorDataStore(context.Context, AggressorDataStoreRequest) (Value, error)`; adapter `AggressorDataStoreProviderFunc`. The request carries `Operation`, `Name`, provenance, and resolved `Arguments`; use `Arg` and `HasArgument`. | Credential, application, archive, download, keystroke, screenshot, service, target, and host queries plus the documented mutation helpers. Results normally pass through; `redactobject` is effect-only and returns `$null`. |
| `WithAggressorPreferenceProvider` | `AggressorPreferenceProvider.HandleAggressorPreference(context.Context, AggressorPreferenceRequest) (Value, error)`; adapter `AggressorPreferenceProviderFunc`. Fields are `Operation`, provenance, `PreferenceName`, `DefaultValue`, and `PreferenceValue`. | `pref_get`, `pref_get_list`, `pref_set`, and `pref_set_list`. Gets return the provider value; successful setters return `$null`. |
| `WithAggressorProfileProvider` | `AggressorProfileProvider.HandleAggressorProfile(context.Context, AggressorProfileRequest) (Value, error)`; adapter `AggressorProfileProviderFunc`. Fields are `Operation`, `Name`, provenance, `Payload`, and `Architecture`. | `killdate`, `setup_strings`, and `setup_transformations`. Provider values pass through. |
| `WithAggressorVPNProvider` | `AggressorVPNProvider.HandleAggressorVPN(context.Context, AggressorVPNRequest) (Value, error)`; adapter `AggressorVPNProviderFunc`. The request carries `Operation`, `Name`, provenance, interface/key presence, MAC address, reserved value, port, and channel. | `vpn_interfaces`, `vpn_interface_info`, `vpn_tap_create`, and `vpn_tap_delete`. Queries return values; successful create/delete calls return `$null`. |
| `WithAggressorClientServiceProvider` | `AggressorClientServiceProvider.HandleAggressorClientService(context.Context, AggressorClientServiceRequest) (Value, error)`; adapter `AggressorClientServiceProviderFunc`. The request carries `Operation`, `Name`, provenance, `Bindings`, `Arguments`, and optional sync callback state. | Client identity, users, logging, messaging, custom events, close, and `sync_download`. Identity/query calls return values; effect-only calls return `$null`. The sync callback is retained and multi-shot. |

## Generation, binary, and storage providers

| Configuration | Interface and request | Script surface and result policy |
| --- | --- | --- |
| `WithAggressorArtifactProvider` | `AggressorArtifactProvider.GenerateAggressorArtifact(context.Context, AggressorArtifactRequest) (Value, error)`; adapter `AggressorArtifactProviderFunc`. The request carries listener, artifact type, architecture, exit/system-call methods, four optional payload positions with presence flags, proxy configuration, `Bindings`, and callback. | `artifact_payload` returns the provider value. Deprecated `artifact_stageless` delivers through its retained callback and returns `$null`. |
| `WithAggressorPayloadProvider` | `AggressorPayloadProvider.HandleAggressorPayload(context.Context, AggressorPayloadRequest) (Value, error)`; adapter `AggressorPayloadProviderFunc`. The request carries `Operation`, `Name`, provenance, `Bindings`, and resolved `Arguments`; use `Arg` and `HasArgument`. | The payload, stager, legacy artifact, signing, bootstrap, and bootstrap-hint family. Result policy is function-specific and available from `DefaultAggressorFunctionContracts`. |
| `WithAggressorListenerProvider` | `AggressorListenerProvider.HandleAggressorListener(context.Context, AggressorListenerRequest) (Value, error)`; adapter `AggressorListenerProviderFunc`. The request carries `Operation`, `Name`, provenance, `Bindings`, and resolved `Arguments`. | The ten listener query and mutation functions. Queries return provider values; effect-only mutations return `$null` according to the function contract. |
| `WithAggressorPayloadStoreProvider` | `AggressorPayloadStoreProvider.HandleAggressorPayloadStore(context.Context, AggressorPayloadStoreRequest) (Value, error)`; adapter `AggressorPayloadStoreProviderFunc`. The request carries `Operation`, `Name`, provenance, and resolved `Arguments`. | The five `payloadstore_*` operations. Use the machine-readable contract for each value/null result. |
| `WithAggressorSiteProvider` | `AggressorSiteProvider.HandleAggressorSite(context.Context, AggressorSiteRequest) (Value, error)`; adapter `AggressorSiteProviderFunc`. The request carries `Kind`, `Name`, provenance, `Bindings`, host/port/URI/content/MIME/description, and SSL value/presence/truth fields. | `localip`, `sites`, and `site_host` return provider values; `site_kill` returns `$null`. |
| `WithAggressorPEProvider` | `AggressorPEProvider.HandleAggressorPE(context.Context, AggressorPERequest) (Value, error)`; adapter `AggressorPEProviderFunc`. The request carries `Operation`, `Name`, provenance, and resolved `Arguments`; use `Arg` and `HasArgument`. | Seven structure-aware `pe_*` transformations plus `pedump`. Provider values pass through. Portable raw-offset PE helpers do not use this provider. |
| `WithAggressorCodeTransformProvider` | `AggressorCodeTransformProvider.HandleAggressorCodeTransform(context.Context, AggressorCodeTransformRequest) (Value, error)`; adapter `AggressorCodeTransformProviderFunc`. The request carries `Operation`, `Name`, provenance, and resolved `Arguments`. | `encode`, `powershell_compress`, and `transform_vbs`. Provider values pass through; the active compression hook runs before the provider. |
| `WithAggressorProcessInjectionProvider` | `AggressorProcessInjectionProvider.HandleAggressorProcessInjection(context.Context, AggressorProcessInjectionRequest) (Value, error)`; adapter `AggressorProcessInjectionProviderFunc`. The request carries `Operation`, `Name`, provenance, `Bindings`, and `SelectionName`. | The `pi_explicit_*`, `pi_spawn_*`, `pi_user_explicit_*`, and `pi_user_spawn_*` families. Queries return values; setters and clears return `$null`. |
| `WithAggressorBOFExtractor` | `AggressorBOFExtractor.ExtractAggressorBOF(context.Context, AggressorBOFExtractionRequest) ([]byte, error)`; adapter `AggressorBOFExtractorFunc`. The request contains `Name`, provenance, a detached low-byte `Data` copy, and `EntryPoint`. | `bof_extract`. Omitted entry points use `AggressorBOFDefaultEntryPoint`; returned bytes are copied into a binary-provenance Sleep string. |
| `WithBeaconStringEncoder` | `BeaconStringEncoder.EncodeBeaconString(context.Context, Value, Value) ([]byte, error)`; adapter `BeaconStringEncoderFunc`. Arguments are session ID and text. | Replaces only the `bof_pack` `z` string encoder. OPFOR copies returned bytes, applies the C-string terminator, and builds the final packed field. The default encoder is UTF-8. |

## Action, execution, RPC, and transcript APIs

| Configuration | Interface and request | Script surface and result policy |
| --- | --- | --- |
| `WithAggressorBeaconActionProvider` | `AggressorBeaconActionProvider.DispatchAggressorBeaconAction(context.Context, AggressorBeaconAction) error`; adapter `AggressorBeaconActionProviderFunc`. `AggressorBeaconAction` carries canonical `Kind`, exact `Name`, provenance, `Bindings`, `Target`, remaining `Arguments`, and callback/state. | The complete supported Beacon operational-action family. Calls are effect-only and return `$null`. Array-valued targets are preserved as one value; OPFOR does not fan them out. |
| `WithAggressorBeaconExecutionProvider` | `AggressorBeaconExecutionProvider.HandleAggressorBeaconExecution(context.Context, AggressorBeaconExecutionRequest) (Value, error)`; adapter `AggressorBeaconExecutionProviderFunc`. The request has fixed fields for ID, command, arguments, flags, PID, content, entry point, packed arguments, callback state, and optional message ID. | `beacon_execute_job`, `beacon_execute_postex_job`, both inline-execute forms, both host-script forms, and `get_postex_kit_callback_id`. Task effects return `$null`; query/helper results pass through. |
| `WithAggressorTeamServerRPCProvider` | `AggressorTeamServerRPCProvider.CallAggressorTeamServerRPC(context.Context, AggressorTeamServerRPCRequest) error`; adapter `AggressorTeamServerRPCProviderFunc`. The request carries command, payload arguments, provenance, and `AggressorTeamServerRPCCallback`. | `call(command, callback, ...)`. Success returns `$null`. A valid multi-shot callback exposes `Valid()` and `Respond(ctx, response)`; `Respond` invokes the script callback with the original command and response. An explicit `$null` callback is valid fire-and-forget input. |
| `WithAggressorBeaconTranscriptSink` | `AggressorBeaconTranscriptSink.PublishAggressorBeaconTranscript(context.Context, AggressorBeaconTranscriptRecord) error`; adapter `AggressorBeaconTranscriptSinkFunc`. The record carries kind, provenance, ID, text/task/job fields, and raw optional technique IDs. | `berror`, `blog`, `blog2`, `binput`, `btask`, `btaskcompleted`, `bjoblog`, and `bjoberror`. The sink replaces the default stdout presentation; calls return `$null`. |

`AggressorBeaconActionKind`, `AggressorBeaconExecutionKind`, and
`AggressorBeaconTranscriptKind` constants retain exact function spellings and
are the stable dispatch keys for adapters. Use
`DefaultAggressorFunctionContracts` for the exhaustive action list and exact
arity/callback metadata.

## UI and execution-control providers

| Configuration | Interface and request | Script surface and lifecycle |
| --- | --- | --- |
| `WithAggressorClientUIProvider` | `AggressorClientUIProvider.HandleAggressorClientUI(context.Context, AggressorClientUIRequest) (Value, error)`; adapter `AggressorClientUIProviderFunc`. The request carries `Operation`, `Name`, provenance, `Bindings`, `Arguments`, an optional retained callback, an optional `AggressorPopupComposer`, and optional `BindingInvocation` composition ancestry. | Tabs, visualizations, popup/menubar composition, navigation, clipboard, URLs, messages, documented `open*` functions, and browser/menu component helpers. Results are function-specific; effect-only functions and callback-delivered `openPayloadHelper` return `$null`. |
| `WithAggressorDialogProvider` | `AggressorDialogProvider.PresentAggressorDialog(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error`; adapter `AggressorDialogProviderFunc`. | The `dialog`, description/show, `dbutton_*`, and `drow_*` family. Presentations contain stable IDs, provenance, defaults, ordered rows, and buttons. The one-shot responder provides `Activate`, `Dismiss`, and `Done`. |
| `WithAggressorPromptProvider` | `AggressorPromptProvider.PresentAggressorPrompt(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error`; adapter `AggressorPromptProviderFunc`. | Confirm, text, directory-open, file-open, and file-save prompts. The presentation records kind, text/title, default presence, and multiple-selection presence/truth. The one-shot responder provides `Accept`, `Dismiss`, and `Done`. |
| `WithAggressorBreakpointProvider` | `AggressorBreakpointProvider.HandleAggressorBreakpoint(context.Context, AggressorBreakpointSnapshot) error`; adapter `AggressorBreakpointProviderFunc`. | `brk()`. The detached snapshot contains runtime/script provenance, source location, timestamp, locals, globals, closure variables, frames, call stack, and current function. `Clone` creates an independently mutable snapshot. A provider may block until execution should continue. |
| `WithAggressorEventDispatcher` | `AggressorEventDispatcher.DispatchAggressorEvent(context.Context, Callable) error`; adapter `AggressorEventDispatcherFunc`. | Replaces the `dispatch_event` scheduler. The dispatcher may invoke immediately or retain and queue the guarded callback. Queued calls use a new caller context and remain bound to the originating script generation. |

`AggressorPopupComposer.Compose(ctx)` enters the exact popup registrations
captured for one UI request. It never retargets newer registrations; clear or
unload makes it return `ErrAggressorPopupStale`.

The retained capability shapes are:

```go
type AggressorPopupComposer interface {
	Compose(context.Context) error
}

type AggressorDialogResponder interface {
	Activate(context.Context, AggressorDialogButtonID, ...AggressorDialogRowValue) (Value, error)
	Dismiss() error
	Done() <-chan struct{}
}

type AggressorPromptResponder interface {
	Accept(context.Context, ...Value) (Value, error)
	Dismiss() error
	Done() <-chan struct{}
}

func (AggressorTeamServerRPCCallback) Valid() bool
func (AggressorTeamServerRPCCallback) Respond(context.Context, Value) (Value, error)
```

Dialog and prompt responders are consumed by the first valid terminal action.
Their `Done` channel closes on response, dismissal, provider failure, lifecycle
revocation, or runtime close. A provider can respond synchronously or retain a
responder for asynchronous UI work.

The dialog constructor, description, row, and button functions build a
runtime-owned presentation; `dialog_show` is the operation that calls
`PresentAggressorDialog`. Each `prompt_*` function calls
`PresentAggressorPrompt` directly.

## Catalog and registration APIs

### Command catalogs

`WithAggressorCommandCatalog(kind, catalog)` seeds one immutable command-help
base. `AggressorCommandKind` supports:

- `AggressorCommandBeacon`
- `AggressorCommandSSH`

`AggressorCommandCatalog` contains ordered `Commands` and `Groups`.
`AggressorCommandMetadata` stores name, description, detail, and optional group
ID; `AggressorCommandGroup` stores ID, name, and description. OPFOR validates
names, uniqueness, and group references and defensively copies the input.

Script registrations layer over the base. Use
`Runtime.SnapshotAggressorCommandCatalog(kind)` for a detached effective view.

### Beacon technique catalogs

`WithAggressorBeaconTechniqueCatalog(kind, catalog)` seeds one immutable
metadata base for:

- `AggressorBeaconTechniqueElevator`
- `AggressorBeaconTechniqueExploit`
- `AggressorBeaconTechniqueRemoteExecMethod`
- `AggressorBeaconTechniqueRemoteExploit`

`AggressorBeaconTechniqueCatalog` contains ordered
`AggressorBeaconTechniqueMetadata` entries. Remote-exploit entries require an
`x86` or `x64` architecture; other kinds reject an architecture.

Use `Runtime.SnapshotAggressorBeaconTechniqueCatalog(kind)` for a detached
effective view. Use
`Runtime.InvokeAggressorBeaconTechnique(ctx, kind, name, arguments...)` to
invoke an effective script-owned callback. Importer-seeded entries contain
metadata only, so a base-only entry cannot be invoked.

### Binding observation and host-driven entry

`WithBindingObserver` installs a `BindingObserver`:

```go
type BindingObserver interface {
	Registered(context.Context, Binding) error
	Unregistered(context.Context, Binding) error
}
```

`Binding` describes subroutines, events, commands, aliases, hooks, popups,
menus, items, and key bindings. It includes `BindingKind`, `BindingLifetime`,
`EnvironmentKind`, selectors, filter/predicate metadata, parent composition,
source provenance, and a guarded callback.

`Binding.Callback` is a `Callable`; `Binding.Predicate` is a
`PredicateEvaluator`. `CallableFunc` and `PredicateEvaluatorFunc` adapt ordinary
Go functions to those two interfaces.

Applications can enter registered script behavior through:

- `Runtime.Dispatch` for any `BindingKind`
- `Runtime.DispatchEvent`
- `Runtime.InvokeBinding` and `Runtime.InvokeBindingByID`
- `Runtime.DispatchPopupHook`
- `Runtime.InvokeConsole`
- `Runtime.InvokeAggressorBeaconTechnique`

`Runtime.Bindings(kind, name)` returns matching registrations, while
`Script.Bindings()` returns that script's active registrations.
`Runtime.BindingByID` preserves an exact `(ScriptID, BindingID)` identity when
several scripts register the same name.

## Generic extension APIs

Typed providers are preferred for known function families because they carry
validated request shapes and explicit result policies. The following generic
extensions cover custom behavior and compatibility fallbacks.

| Configuration/API | Interface or callback | Purpose |
| --- | --- | --- |
| `WithFunction(name, NativeFunc)` | `type NativeFunc func(context.Context, Invocation) (Value, error)` | Installs an exact runtime-wide function override. `Runtime.RegisterFunction` adds one after construction; `Script.RegisterFunction` installs a generation-owned function that is revoked on unload. |
| `WithHost(Host)` | `Host.Call(context.Context, Invocation) (Value, error)`; adapter `HostFunc` | Receives otherwise unresolved functions and predicates. `Invocation` exposes live pass-by-name arguments, `Arg`, `Values`, `Callback`, `RetainCallback`, and `Bindings`. |
| `WithObjectHost(ObjectHost)` | `ObjectHost.Object(context.Context, ObjectInvocation) (Value, error)`; adapter `ObjectHostFunc` | Handles importer-owned construction, methods, properties, and type tests. Returning `UnsupportedError` declines to OPFOR's portable object fallback. |
| `Iterator` / `MutableIterator` | `Next(ctx) (Value, bool, error)` and optional `Remove(ctx) error`; adapter `IteratorFunc` | Lets opaque `ObjectValue`s participate in `foreach` and iterator-consuming stock functions. |
| `WithEnvironment(keyword, kind)` | `EnvironmentKind` is ordinary, filter, or predicate. `WithCompileEnvironment` adds the corresponding standalone compile option. | Registers custom environment syntax. Runtime compilation is required so the parser knows predicate environments before parsing. Bindings are delivered through the normal observer/dispatch APIs. |
| `WithInitialGlobals` | Map of variable names to `Value`s. | Installs eager values before lifecycle notification and top-level execution. Compound values preserve identity. |
| `WithVariableProvider(VariableProvider)` | `CreateGlobalVariableContainer(ctx, request)`; adapter `VariableProviderFunc`. The returned `VariableContainer` implements exists/get/put/remove plus local/internal container factories. | Makes application-owned variable storage authoritative. Exact `*Cell` identity crosses this boundary; failures are wrapped by `VariableProviderError` with operation and provenance. |
| `WithScriptLifecycleObserver` | `ScriptLifecycleObserver.ScriptLoaded` / `ScriptUnloaded`; adapter `ScriptLifecycleFuncs`. | Receives paired load/unload notifications for independently loaded scripts. `ScriptLoaded` may inspect or seed globals before top-level execution. |
| `WithSourceResolver` | `SourceResolver.ResolveSource(ctx, SourceRequest) (Source, error)`; adapter `SourceResolverFunc`. | Resolves `include` from files, archives, embedded assets, databases, or virtual modules. It is mutually exclusive with `WithSleepClasspath`, which configures the built-in `FileSourceResolver`. |
| `WithLoadableProvider` | `LoadableProvider.ResolveLoadable(ctx, LoadableRequest) (LoadableBridge, error)`; adapter `LoadableProviderFunc`. `LoadableBridge` has paired `ScriptLoaded`/`ScriptUnloaded` methods; adapter `LoadableBridgeFuncs`. | Maps Sleep `use()` identities to pure-Go, script-local bridges. Bridges can publish generation-owned functions with `Script.RegisterFunction`. `UnsupportedError` declines one identity to the next fallback. |
| `WithTaintMode` / `WithTaintFunction` / `WithTaintPolicy` | `NativeFunc` plus `TaintPolicy`; runtime forms are `RegisterTaintFunction` and `RegisterTaintPolicy`. `Runtime.TaintMode`, `Taint`, `TaintAll`, and `Untaint` expose bridge-level taint operations. | Enables taint tracking and adds or classifies custom functions. Policies are source, sanitizer, sensitive, sensitive-source, or permeable. |

### Invocation ownership

`Invocation.Arg` resolves one argument and `Invocation.Values` creates a
detached top-level value slice. `Argument.Set` mutates an ordinary bare-variable
argument or an explicit pass-by-name reference; expression temporaries are not
references. Raw `Invocation.Runtime`, `Argument.Reference`, `*Script`, and shared
compound values are trusted Go capabilities and are not lifecycle revoked.

Use `Invocation.Callback`, `Invocation.RetainCallback`, or
`Invocation.Bindings` when asynchronous work needs a guarded capability rather
than raw runtime authority. `ObjectInvocation` provides matching `Callback`
and `RetainCallback` methods.

## The `aggressor` host adapter

The `aggressor` package wraps `opfor.Host` with a concurrency-safe named
registry:

```go
host := aggressor.NewHost()
err := host.Register("operator_name", func(
	_ context.Context,
	_ aggressor.Request,
) (aggressor.Value, error) {
	return opfor.String("alice"), nil
})
if err != nil {
	panic(err)
}

runtime, err := opfor.New(opfor.WithHost(host))
if err != nil {
	panic(err)
}
defer runtime.Close(context.Background())
```

`aggressor.Host` supports:

- `Register(name, Callback)` and `Unregister(name)`;
- `SetFallback(Callback)`, where `nil` restores `UnsupportedError` behavior;
- `Names()` for a sorted snapshot; and
- `Call`, satisfying `opfor.Host`.

`Callback` has the exact shape
`func(context.Context, aggressor.Request) (aggressor.Value, error)`. The zero
value of `aggressor.Host` is ready for use; `NewHost` is the convenient
constructor.

`aggressor.Request` contains the normalized name, script ID, source location,
opaque `aggressor.Runtime`, and detached argument slice. `Request.Arg` and
`Request.Values` provide access helpers. `aggressor.Argument` provides
`Value`, `IsReference`, `Set`, and `Callback`. `aggressor.ScriptCallback` is a
retained lifecycle-bound callback with `Valid` and `Invoke`.

`Request.Location` is an `aggressor.Location` with source and start/end
`Position` values. `ErrNotReference` reports an `Argument.Set` on an expression
temporary; `ErrNoRuntime` reports use of a zero-value opaque `Runtime`.

The opaque `aggressor.Runtime` capability exposes `Valid`, `Same`,
`DispatchEvent`, `InvokeHook`, `InvokePopupHook`, and `DispatchPopupHook`
without exposing a raw `*opfor.Runtime`.

The catalog schema is also public. `CatalogSnapshot` contains `Counts`,
`SourceSnapshot`, and ordered `Entry` values. An `Entry` records `EntryKind`,
`Match`, `Support`, `Boundary`, `Evidence`, and a `Contract`; the contract
contains `ArityContract`, `CallbackContract`, `ArgumentConstraint`,
`VersionContract`, `ReturnShape`, `ContractAudit`, `ContractConfidence`,
`SoftErrorPolicy`, typed-provider name, error policy, and host-fallback state.
`Entry.Matches` applies exact or prefix matching.

| Catalog enum | Exported values |
| --- | --- |
| `EntryKind` | `KindFunction`, `KindEvent`, `KindHook`, `KindPopupHook` |
| `Match` | `MatchExact`, `MatchPrefix` |
| `Support` | `SupportPortableDefault`, `SupportHostRequired`, `SupportUnsupported` |
| `Boundary` | `BoundaryPortableRuntime`, `BoundaryNativeWrapper`, `BoundaryGenericHost`, `BoundaryBindingDispatch` |
| `ContractAudit` | `ContractAuditNameOnly`, `ContractAuditRuntimeEnforced` |
| `ContractConfidence` | `ContractConfidenceInventory`, `ContractConfidenceExecutable` |
| `ReturnShape` | `ReturnShapeUnknown`, `ReturnShapeValue`, `ReturnShapeNull`, `ReturnShapePredicate` |
| `SoftErrorPolicy` | `SoftErrorPolicyUnknown` |

`CatalogSchemaVersion` identifies the serialized schema. The immutable snapshot
metadata is published as `OfficialSnapshotDate`, `OfficialFunctionCount`,
`OfficialEventCount`, `OfficialHookCount`, `OfficialPopupHookCount`, and
`OfficialNamesSHA256`.

## Implementation checklist

For each adapter:

1. Install only provider families the application fully owns.
2. Switch on the canonical `Kind` or `Operation`, but log `Name` when exact
   alias spelling matters.
3. Treat `RuntimeID` plus `ScriptID` as the provenance key.
4. Check presence flags before interpreting optional values.
5. Decide whether retained compound values should be copied or coerced.
6. Observe context cancellation and make the provider concurrency-safe.
7. Retain guarded `Callable`, responder, popup, RPC, or `AggressorBindings`
   capabilities instead of raw interpreter state.
8. Return detached result graphs when script mutation must not reach backing
   application state.
9. Test success, provider error, missing-provider fallback, callback
   revocation, script unload, runtime close, and concurrent calls.
10. Use `DefaultAggressorFunctionContracts` and `aggressor.Catalog` in tests so
    provider coverage cannot silently drift from the runtime.

The public [`conformance`](../conformance) package provides inert host,
object-host, loadable-provider, callback, lifecycle, and authoritative-error
checks for reusable adapters.
