# Aggressor Script implementation guides

These guides organize OPFOR's importer-facing Aggressor Script callbacks by
the application subsystem that must implement them. You do not need to
implement every group. Install only the providers your application owns and
use `WithHost` for intentional compatibility fallback.

Start with the [shared provider and callback contract](provider-contract.md).
It defines precedence, context and concurrency rules, value ownership,
provenance, retained-callback lifetime, errors, Host fallback, and
`ScriptLoader` inheritance once for every group.

## Guide map

| Guide | Provider and callback families | Native wrapper inventory |
| --- | --- | ---: |
| [Query, state, and services](query-state-services.md) | Session/data-model/data-store queries, preferences, profile, VPN, and client services | 55 |
| [Generation, binary, and storage](generation-binary-storage.md) | Artifacts, payloads, listeners, payload store, sites, PE transforms, code transforms, process injection, BOF extraction, and `bof_pack` encoding | 63 plus the encoder seam |
| [Beacon tasking, execution, RPC, and transcript](beacon-tasking-execution.md) | Beacon actions, low-level execution, Team Server `call`, and transcript publication | 128 plus `call` and 8 transcript functions |
| [Client UI and execution control](client-ui-execution-control.md) | Client UI, dialogs, prompts, `brk`, and `dispatch_event` | 109 plus two execution-control seams |
| [Catalogs, bindings, and dispatch](catalogs-bindings-dispatch.md) | Command/technique catalogs, registrations, observers, `AggressorBindings`, and host-driven entry | metadata and registration APIs |

At this revision,
`opfor.DefaultAggressorFunctionContracts()` contains 355 runtime-enforced
wrappers across 21 typed provider interfaces. Several important integrations
are outside that registry because they are not ordinary value-returning typed
providers: Team Server RPC `call`, Beacon transcript output, `brk`,
`dispatch_event`, `bof_pack` string encoding, command/technique catalogs, and
binding observation.

## Pick the narrowest boundary

Use the narrowest interface that owns the effect:

1. Use a typed `WithAggressor...Provider` option for a complete known function
   family. Typed requests have validated shapes, provenance, explicit result
   policy, and guarded callbacks.
2. Use `WithFunction` for an exact-name override or application-specific
   implementation.
3. Use `WithHost` for unresolved compatibility functions and predicates.
4. Use `WithObjectHost` for importer-owned Java-style objects rather than
   representing object methods as unrelated functions.

A configured typed provider is authoritative for its family. Returning an
error, including `UnsupportedError`, does not retry through `Host`, because an
external effect may already have occurred.

## Minimal provider

Every typed interface has a `Func` adapter, so a small implementation can stay
local to runtime construction:

```go
sessionQueries := opfor.AggressorSessionQueryProviderFunc(func(
	ctx context.Context,
	query opfor.AggressorSessionQuery,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}

	switch query.Kind {
	case opfor.AggressorSessionQueryBeacons:
		return opfor.ArrayValue(snapshotBeacons()), nil
	case opfor.AggressorSessionQueryBeaconIDs:
		return opfor.ArrayValue(snapshotBeaconIDs()), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported session query %q", query.Kind)
	}
})

runtime, err := opfor.New(
	opfor.WithAggressorSessionQueryProvider(sessionQueries),
)
```

The provider is called synchronously, but several script executions may call
it concurrently. The returned arrays in this example must be detached if
script mutation must not affect the application's backing state.

## Machine-readable coverage

Do not maintain a second private function inventory in an adapter. Build drift
checks from:

```go
contracts := opfor.DefaultAggressorFunctionContracts()
catalog := aggressor.Catalog()
```

`AggressorFunctionContract` records the exact function name, inclusive arity,
callback positions, argument constraints, typed provider, script-visible result
policy, fallback policy, and deprecation state. `aggressor.Catalog` adds the
portable, generic-host, event, hook, popup-hook, and evidence classifications.

Each group guide shows how to filter the contract inventory for its provider
names and how to test typed success, absent-provider fallback, authoritative
provider errors, callback revocation, and concurrent calls.

## Functions with no importer callback

The default runtime also implements portable Aggressor helpers and registration
functions that do not need an application provider. Their presence is visible
through `opfor.DefaultFunctionNames()` and `aggressor.Catalog()`.

Examples include client-independent encoding/compression utilities, command
and technique metadata registries, events/aliases/hooks, raw-offset PE helpers,
and default headless presentation for transcript records and `brk`. The group
guides call out any function whose portable implementation can be replaced by
an optional provider.

## Related documentation

- [Generic OPFOR embedding](../README.md)
- [Stable Aggressor extension landing page](../aggressor-script-extensions.md)
- [Shared provider and callback contract](provider-contract.md)
- [`opfor` public facade](../../api.go)
- [`aggressor` catalog implementation](../../aggressor/catalog.go)
- [Reusable conformance kit](../../conformance)
