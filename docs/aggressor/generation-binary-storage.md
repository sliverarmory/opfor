# Generation, binary, and storage providers

[Aggressor provider index](README.md) | [Shared provider contract](provider-contract.md)

This group contains 63 typed native wrappers across nine provider or extractor
interfaces, plus the `BeaconStringEncoder` seam used by `bof_pack` format `z`.
It covers Cobalt-owned artifact and payload generation, listener and Payload
Store state, hosted content, PE and code transforms, process-injection
configuration, and extracted-BOF bytes. OPFOR owns call validation, routing,
provenance, and result policy; the embedding application owns every unpublished
generation algorithm and external store.

`WithFunction` remains the highest-precedence exact-name override. With no
corresponding typed provider, a valid wrapper call reaches `WithHost` once with
the original reference-bearing `Invocation`. A configured provider is
authoritative: after it is called, neither its error nor an
`UnsupportedError` is retried through `Host`. `BeaconStringEncoder` is the one
different seam: it augments the already-portable `bof_pack` implementation and
defaults to UTF-8 rather than falling back to `Host`.

## Provider inventory

### Artifact generation: 2 functions

Install `AggressorArtifactProvider` with `WithAggressorArtifactProvider`; its
function adapter is `AggressorArtifactProviderFunc` and its method is:

```go
GenerateAggressorArtifact(context.Context, opfor.AggressorArtifactRequest) (opfor.Value, error)
```

| Function | Arity | Request fields and typed result |
| --- | ---: | --- |
| `artifact_payload` | 5-9 | `Listener`, `ArtifactType`, `Architecture`, `ExitMethod`, and `SystemCallMethod`; optional `HTTPLibrary`, `DNSCommMode`, `MalleableProfileOverride`, and `PayloadStoreInfo` each have a matching `Has...` flag. The provider value passes through. |
| `artifact_stageless` | 5 | `Listener`, `ArtifactType`, `Architecture`, `ProxyConfiguration`, and a required retained `Callback`. The provider result is discarded and the script receives `$null`; generated content is delivered by invoking `Callback` with one argument. |

The request also contains `Kind`, exact `Name`, provenance, and
generation-bound `Bindings`. The callback may be retained and invoked more
than once with a new caller-owned context, and rejects calls after its source
generation retires. Do not use a zero `Value` to infer optional payload-field
presence; inspect the associated `Has...` flag.

### Payload, stager, and legacy artifact generation: 14 functions

Install `AggressorPayloadProvider` with `WithAggressorPayloadProvider`; the
adapter is `AggressorPayloadProviderFunc` and the method is:

```go
HandleAggressorPayload(context.Context, opfor.AggressorPayloadRequest) (opfor.Value, error)
```

| Function | Arity | Positional request shape |
| --- | ---: | --- |
| `-hasbootstraphint` | 1 | Payload bytes. The provider result is normalized through Sleep truthiness. |
| `all_payloads` | 3-6 | Destination folder, sign flag, system-call method, optional HTTP library, DNS communication mode, profile override. |
| `artifact` | 2-4 | Listener, artifact type, optional deprecated value, architecture. |
| `artifact_general` | 3 | Shellcode, artifact type, architecture. |
| `artifact_sign` | 1 | Artifact bytes. |
| `artifact_stager` | 3-4 | Listener, artifact type, architecture, optional Payload Store information hash. |
| `payload` | 4-7 | Listener, architecture, exit method, system-call method, optional HTTP library, DNS communication mode, profile override. |
| `payload_bootstrap_hint` | 2 | Payload bytes, function name. |
| `payload_local` | 5-6 | Parent Beacon ID, listener, architecture, exit method, system-call method, optional HTTP library. |
| `powershell` | 2-3 | Listener, local-host flag, optional architecture. |
| `shellcode` | 3 | Listener, remote-target flag, architecture. |
| `stager` | 2 | Listener, architecture. |
| `stager_bind_pipe` | 1 | Listener. |
| `stager_bind_tcp` | 3 | Listener, architecture, port. |

Every successful result other than the predicate passes through. The request
carries `Operation`, exact `Name`, provenance, generation-bound `Bindings`, and
detached top-level `Arguments`; use `Arg` and `HasArgument` to preserve
omission. The typed route requires `artifact_stager`'s fourth argument to be a
hash. It also enforces these exact `all_payloads` enumerations after Sleep
string coercion while still passing the original Values to the provider:

- position 3: `None`, `Direct`, or `Indirect`;
- position 4: `wininet`, `winhttp`, `$null`, or a blank string; and
- position 5: `dns`, `dns_over_https`, `$null`, or a blank string.

Those enum constraints apply only to the typed-provider route; a missing
provider preserves the raw Host invocation.

### Listener state: 10 functions

Install `AggressorListenerProvider` with `WithAggressorListenerProvider`; the
adapter is `AggressorListenerProviderFunc` and the method is:

```go
HandleAggressorListener(context.Context, opfor.AggressorListenerRequest) (opfor.Value, error)
```

| Functions | Arity | Positional request and typed result |
| --- | ---: | --- |
| `listener_create` | 4-5 | Name, payload, host, port, optional Beacon hosts. Effect-only; provider result is discarded. |
| `listener_create_ext` | 3 | Name, payload, options hash. Effect-only; provider result is discarded. |
| `listener_delete`, `listener_restart` | 1 | Listener name. Effect-only; provider result is discarded. |
| `listener_describe`, `listener_info` | 1-2 | Listener name and optional target/key. Provider value passes through. |
| `listener_pivot_create` | 5 | Beacon ID, name, payload, host, port. Effect-only; provider result is discarded. |
| `listeners`, `listeners_local`, `listeners_stageless` | 0 | Provider value passes through. |

The request also includes `Operation`, exact `Name`, provenance, `Bindings`,
and `Arguments` with `Arg`/`HasArgument`. The typed route validates the
`listener_create_ext` options hash. Record schemas, ordering, missing-listener
behavior, and remote resolution remain importer-owned.

### Payload Store: 5 functions

Install `AggressorPayloadStoreProvider` with
`WithAggressorPayloadStoreProvider`; the adapter is
`AggressorPayloadStoreProviderFunc` and the method is:

```go
HandleAggressorPayloadStore(context.Context, opfor.AggressorPayloadStoreRequest) (opfor.Value, error)
```

| Function | Arity | Positional request and typed result |
| --- | ---: | --- |
| `payloadstore_add` | 5-6 | Name, payload type, artifact type, architecture, payload bytes, optional information hash. Provider value passes through. |
| `payloadstore_fetch` | 1 | ID or name. Provider value passes through. |
| `payloadstore_list` | 0 | Provider value passes through. |
| `payloadstore_metadata` | 1 | ID or name. Provider value passes through. |
| `payloadstore_remove` | 1 | ID or name. Provider result is discarded and the script receives `$null`. |

The request carries `Operation`, exact `Name`, provenance, and `Arguments` with
`Arg`/`HasArgument`; unlike payload requests, it has no `Bindings` field. The
typed route requires the optional sixth `payloadstore_add` argument to be a
hash. Entry schemas, persistence, duplicate names, missing entries, ordering,
and freshness are provider policy.

### Site delivery: 4 functions

Install `AggressorSiteProvider` with `WithAggressorSiteProvider`; the adapter
is `AggressorSiteProviderFunc` and the method is:

```go
HandleAggressorSite(context.Context, opfor.AggressorSiteRequest) (opfor.Value, error)
```

| Function | Arity | Request fields and typed result |
| --- | ---: | --- |
| `localip` | 0 | Query; provider value passes through. |
| `sites` | 0 | Query; provider value passes through. |
| `site_host` | 6-7 | `Host`, `Port`, `URI`, `Content`, `MIMEType`, `Description`, optional `SSL`. Provider value passes through. |
| `site_kill` | 2 | `Port` and `URI`. Provider result is discarded and the script receives `$null`. |

The request also contains `Kind`, exact `Name`, provenance, and `Bindings`.
For `site_host`, inspect `HasSSL` to distinguish omission from explicit
`$null`; `SSLTruth` records the supplied value's Sleep truthiness without
replacing the exact `SSL` value.

### PE operations: 8 functions

Install `AggressorPEProvider` with `WithAggressorPEProvider`; the adapter is
`AggressorPEProviderFunc` and the method is:

```go
HandleAggressorPE(context.Context, opfor.AggressorPERequest) (opfor.Value, error)
```

| Function | Arity | Positional request shape |
| --- | ---: | --- |
| `pe_insert_rich_header` | 2 | Content, Rich header. |
| `pe_mask_section` | 3 | Content, section name, key. |
| `pe_patch_code` | 3 | Content, find bytes, replacement bytes. |
| `pe_remove_rich_header` | 1 | Content. |
| `pe_set_compile_time_with_string` | 2 | Content, timestamp string. |
| `pe_set_export_name` | 1-2 | Content, optional export name. |
| `pe_set_value_at` | 3 | Content, field name, value. |
| `pedump` | 1 | Content. |

All successful provider values pass through. `AggressorPERequest` carries
`Operation`, exact `Name`, provenance, and `Arguments` with `Arg` and
`HasArgument`; it has no `Bindings`. OPFOR does not parse or modify PE content
on this route. The one-or-two-argument `pe_set_export_name` form is an explicit
evidence union: the published argument table and executable examples disagree,
so the provider must use `HasArgument(1)` rather than inventing a name.

### Code transformations: 3 functions

Install `AggressorCodeTransformProvider` with
`WithAggressorCodeTransformProvider`; the adapter is
`AggressorCodeTransformProviderFunc` and the method is:

```go
HandleAggressorCodeTransform(context.Context, opfor.AggressorCodeTransformRequest) (opfor.Value, error)
```

| Function | Arity | Positional request shape |
| --- | ---: | --- |
| `encode` | 3 | Position-independent code, encoder name, architecture. |
| `powershell_compress` | 1 | PowerShell script. |
| `transform_vbs` | 2 | Shellcode, maximum plaintext-run length. |

Every provider result passes through. The request carries `Operation`, exact
`Name`, provenance, and `Arguments` with `Arg`/`HasArgument`; it has no
`Bindings`. The newest active `POWERSHELL_COMPRESS` script hook runs before the
provider or Host path. A selected hook is authoritative: it does not fall
through after executing or failing.

### Process-injection configuration: 16 functions

Install `AggressorProcessInjectionProvider` with
`WithAggressorProcessInjectionProvider`; the adapter is
`AggressorProcessInjectionProviderFunc` and the method is:

```go
HandleAggressorProcessInjection(context.Context, opfor.AggressorProcessInjectionRequest) (opfor.Value, error)
```

| Functions | Arity | Request and typed result |
| --- | ---: | --- |
| `pi_explicit_get`, `pi_explicit_info`, `pi_spawn_get`, `pi_spawn_info` | 0 | Built-in selection or inventory query; provider value passes through. |
| `pi_explicit_set`, `pi_spawn_set` | 1 | `SelectionName`; provider result is discarded. |
| `pi_user_explicit_get`, `pi_user_explicit_get_map`, `pi_user_explicit_get_names`, `pi_user_spawn_get`, `pi_user_spawn_get_map`, `pi_user_spawn_get_names` | 0 | User-defined selection/inventory query; provider value passes through. |
| `pi_user_explicit_set`, `pi_user_spawn_set` | 1 | `SelectionName`; provider result is discarded. |
| `pi_user_explicit_clear`, `pi_user_spawn_clear` | 0 | Effect-only; provider result is discarded. |

The request also contains `Operation`, exact `Name`, provenance, and
generation-bound `Bindings`. `SelectionName` is populated only for the four
setter operations and is transferred without coercion.

### Extracted BOF bytes: 1 function

Install `AggressorBOFExtractor` with `WithAggressorBOFExtractor`; its function
adapter is `AggressorBOFExtractorFunc` and its method is:

```go
ExtractAggressorBOF(context.Context, opfor.AggressorBOFExtractionRequest) ([]byte, error)
```

`bof_extract` accepts one or two string arguments: object data and an optional
entry point. On the typed route, `Data` is a detached low-byte snapshot which
preserves embedded NULs, and an omitted entry point becomes
`AggressorBOFDefaultEntryPoint` (`"sleep_mask"`). Returned bytes are copied
into a binary-provenance Sleep string; a nil or empty slice is a successful
empty string, not `$null`. The request also carries exact `Name` and
provenance. With no extractor, only arity is checked before the raw invocation
falls back to `Host`; string validation belongs to the typed route.

OPFOR deliberately does not parse, link, relocate, execute, or guess the
unpublished extracted-BOF envelope.

### `bof_pack` narrow-string encoding

Install `BeaconStringEncoder` with `WithBeaconStringEncoder`; the adapter is
`BeaconStringEncoderFunc` and the method is:

```go
EncodeBeaconString(context.Context, beaconID opfor.Value, text opfor.Value) ([]byte, error)
```

This seam applies only to each `z` field of the portable `bof_pack` function.
It receives the target Beacon/session ID and original text `Value`, allowing a
per-session codec and binary-provenance policy. Return only encoded text bytes.
OPFOR stops at the first NUL, appends the canonical terminator, adds the field
length, and copies the bytes into the final binary string. The default is
UTF-8. `Z` remains UTF-16LE and never calls this encoder. An encoder error is
authoritative and does not route to `Host`.

## Compilable implementation skeleton

The following adapter wires every interface in this group. Its deterministic
outputs are intentionally small; replace them with application-owned builders,
stores, and transforms. The operation switches remain useful as fail-closed
guards if OPFOR later adds a new enum value.

```go
package main

import (
	"context"
	"fmt"

	"github.com/sliverarmory/opfor"
)

type generationBinaryStorage struct{}

func (a *generationBinaryStorage) GenerateAggressorArtifact(
	ctx context.Context,
	request opfor.AggressorArtifactRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	artifact := opfor.BinaryString([]byte("artifact:" + request.Listener.String()))
	switch request.Kind {
	case opfor.AggressorArtifactPayload:
		return artifact, nil
	case opfor.AggressorArtifactStageless:
		callbackCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		_, err := request.Callback.Invoke(callbackCtx, artifact)
		return opfor.Null(), err
	default:
		return opfor.Null(), fmt.Errorf("unsupported artifact operation %q", request.Name)
	}
}

func (a *generationBinaryStorage) HandleAggressorPayload(
	ctx context.Context,
	request opfor.AggressorPayloadRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	if request.Operation == opfor.AggressorPayloadHasBootstrapHint {
		return opfor.Bool(true), nil
	}
	return opfor.BinaryString([]byte("payload:" + request.Name)), nil
}

func (a *generationBinaryStorage) HandleAggressorListener(
	ctx context.Context,
	request opfor.AggressorListenerRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorListenerDescribe, opfor.AggressorListenerInfo:
		return opfor.HashValue(opfor.NewHash()), nil
	case opfor.AggressorListenerList,
		opfor.AggressorListenerListLocal,
		opfor.AggressorListenerListStageless:
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorListenerCreate,
		opfor.AggressorListenerCreateExtended,
		opfor.AggressorListenerDelete,
		opfor.AggressorListenerPivotCreate,
		opfor.AggressorListenerRestart:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported listener operation %q", request.Name)
	}
}

func (a *generationBinaryStorage) HandleAggressorPayloadStore(
	ctx context.Context,
	request opfor.AggressorPayloadStoreRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorPayloadStoreAdd:
		return opfor.String("demo-id"), nil
	case opfor.AggressorPayloadStoreFetch:
		return opfor.BinaryString([]byte("demo-payload")), nil
	case opfor.AggressorPayloadStoreList:
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorPayloadStoreMetadata:
		return opfor.HashValue(opfor.NewHash()), nil
	case opfor.AggressorPayloadStoreRemove:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported Payload Store operation %q", request.Name)
	}
}

func (a *generationBinaryStorage) HandleAggressorSite(
	ctx context.Context,
	request opfor.AggressorSiteRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Kind {
	case opfor.AggressorSiteLocalIP:
		return opfor.String("127.0.0.1"), nil
	case opfor.AggressorSiteList:
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorSiteHost:
		return opfor.String(fmt.Sprintf("http://%s:%s%s",
			request.Host.String(), request.Port.String(), request.URI.String())), nil
	case opfor.AggressorSiteKill:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported site operation %q", request.Name)
	}
}

func (a *generationBinaryStorage) HandleAggressorPE(
	ctx context.Context,
	request opfor.AggressorPERequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	if request.Operation == opfor.AggressorPEDump {
		return opfor.HashValue(opfor.NewHash()), nil
	}
	return request.Arg(0), nil
}

func (a *generationBinaryStorage) HandleAggressorCodeTransform(
	ctx context.Context,
	request opfor.AggressorCodeTransformRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	return request.Arg(0), nil
}

func (a *generationBinaryStorage) HandleAggressorProcessInjection(
	ctx context.Context,
	request opfor.AggressorProcessInjectionRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorProcessInjectionExplicitGet,
		opfor.AggressorProcessInjectionSpawnGet,
		opfor.AggressorProcessInjectionUserExplicitGet,
		opfor.AggressorProcessInjectionUserSpawnGet:
		return opfor.String("demo"), nil
	case opfor.AggressorProcessInjectionExplicitInfo,
		opfor.AggressorProcessInjectionSpawnInfo,
		opfor.AggressorProcessInjectionUserExplicitGetNames,
		opfor.AggressorProcessInjectionUserSpawnGetNames:
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorProcessInjectionUserExplicitGetMap,
		opfor.AggressorProcessInjectionUserSpawnGetMap:
		return opfor.HashValue(opfor.NewHash()), nil
	case opfor.AggressorProcessInjectionExplicitSet,
		opfor.AggressorProcessInjectionSpawnSet,
		opfor.AggressorProcessInjectionUserExplicitClear,
		opfor.AggressorProcessInjectionUserExplicitSet,
		opfor.AggressorProcessInjectionUserSpawnClear,
		opfor.AggressorProcessInjectionUserSpawnSet:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported process-injection operation %q", request.Name)
	}
}

func (a *generationBinaryStorage) ExtractAggressorBOF(
	ctx context.Context,
	request opfor.AggressorBOFExtractionRequest,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte("BOF:"+request.EntryPoint+":"), request.Data...), nil
}

func (a *generationBinaryStorage) EncodeBeaconString(
	ctx context.Context,
	beaconID opfor.Value,
	text opfor.Value,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = beaconID // Select a per-session codec here.
	return []byte(text.String()), nil
}

func main() {
	adapter := &generationBinaryStorage{}
	runtime, err := opfor.New(
		opfor.WithAggressorArtifactProvider(adapter),
		opfor.WithAggressorPayloadProvider(adapter),
		opfor.WithAggressorListenerProvider(adapter),
		opfor.WithAggressorPayloadStoreProvider(adapter),
		opfor.WithAggressorSiteProvider(adapter),
		opfor.WithAggressorPEProvider(adapter),
		opfor.WithAggressorCodeTransformProvider(adapter),
		opfor.WithAggressorProcessInjectionProvider(adapter),
		opfor.WithAggressorBOFExtractor(adapter),
		opfor.WithBeaconStringEncoder(adapter),
	)
	if err != nil {
		panic(err)
	}
	defer runtime.Close(context.Background())
}
```

## Lifecycle, inheritance, and ownership

The [shared provider contract](provider-contract.md) applies to every method.
Calls are synchronous and may overlap; implementations must synchronize shared
builders and stores, observe `ctx`, and never retain it. A callback retained
from `artifact_stageless` accepts a new context for each invocation.

Request slices and BOF `Data` are detached at the top level, but ordinary
`Value` rules still apply: arrays, hashes, functions, objects, nested graphs,
and binary provenance retain identity. Successful compound provider results
are handed directly to script code. Return detached results when scripts must
not mutate application-owned state. The BOF extractor and string encoder are
the exceptions for raw byte ownership: OPFOR copies their returned slices.

Portable `ScriptLoader` children inherit the exact provider, extractor, and
encoder instances. Each child is a new runtime with a distinct `RuntimeID`,
fresh registrations, and its own execution generation. Shared implementations
must therefore be concurrency-safe and correlate `Script` with `RuntimeID`.
`Bindings` and retained callbacks are stamped with the child generation that
created them and are revoked without silently targeting a later parent or
child generation.

## Tests and drift checks

Run the focused wrapper and inheritance tests while implementing this group:

```sh
go test ./internal/opfor -run 'TestAggressor(Artifact|Payload|AllPayloads|Listener|PayloadStore|Site|PEProvider|CodeTransform|PowerShellCompress|ProcessInjection|BOFExtract|BOFPack)'
go test ./internal/opfor -run 'Test(PortableScriptLoaderInheritsAggressor(Artifact|Site|PE|CodeTransform|ProcessInjection)Provider|PortableScriptLoaderInheritsAggressor(BOFExtractor|EncodingAndDispatchBoundaries)|PortableScriptLoaderInheritsPayloadListenerAndPayloadStoreProviders|ScriptLoaderPreservesActiveRuntimeExtensionProfiles)'
```

Adapter tests should cover every operation constant; both ends of each arity
range; every `Has...`/`HasArgument` state; callback invocation and revocation;
value, predicate, and null result policies; typed-only hash/string/enum
validation; `WithFunction` and `POWERSHELL_COMPRESS` precedence; raw Host
fallback; provider errors; cancellation; overlapping calls; child provenance;
binary NULs; and returned-slice copying.

For drift detection, filter `opfor.DefaultAggressorFunctionContracts()` by
`TypedProvider` and assert these exact names and counts:
`AggressorArtifactProvider` 2, `AggressorPayloadProvider` 14,
`AggressorListenerProvider` 10, `AggressorPayloadStoreProvider` 5,
`AggressorSiteProvider` 4, `AggressorPEProvider` 8,
`AggressorCodeTransformProvider` 3,
`AggressorProcessInjectionProvider` 16, and `AggressorBOFExtractor` 1. The
inventory totals 63 and also carries the enforced arity, callbacks, argument
constraints, result policy, authoritative-error rule, deprecation, and Host
fallback metadata. `BeaconStringEncoder` is intentionally not a separate
function contract; pin its behavior with focused `bof_pack` tests for `z`, `Z`,
embedded NULs, cancellation, per-session selection, and error propagation.
