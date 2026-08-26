# Query, state, and service providers

[Aggressor provider index](README.md) | [Shared provider contract](provider-contract.md)

This group contains 55 native wrappers across seven provider interfaces. It
covers read-only session and data-model queries, application data records,
preferences, Team Server profile and VPN state, and connected-client services.
OPFOR validates the script-facing call and supplies provenance; the embedding
application owns the records, persistence, ordering, and external effects.

Install only the providers the application supports. `WithFunction` is an
exact-name override and wins over these wrappers. When a provider is absent, a
valid call is forwarded to `WithHost` exactly once with its original
reference-bearing `Invocation`. Once a typed provider is installed, its result
or error is authoritative and OPFOR never retries the call through `Host`.

## Provider inventory

### Session queries: 12 functions

Install `AggressorSessionQueryProvider` with
`WithAggressorSessionQueryProvider`; the function adapter is
`AggressorSessionQueryProviderFunc`. Its method is:

```go
QueryAggressorSession(context.Context, opfor.AggressorSessionQuery) (opfor.Value, error)
```

| Functions | Arity | Request and typed result |
| --- | ---: | --- |
| `beacons`, `beacon_ids` | 0 | `Kind` distinguishes the two inventories. `SessionID` and `Key` are `$null`; the provider value passes through. |
| `bdata`, `beacon_data` | 1 | Both use `AggressorSessionQueryBeaconData`; `Name` preserves the alias and `SessionID` is populated. The provider value passes through. |
| `binfo`, `beacon_info` | 2 | Both use `AggressorSessionQueryBeaconInfo`; `SessionID` and `Key` are populated. The provider value passes through. |
| `barch` | 1 | Uses `AggressorSessionQueryBeaconArchitecture`. A null or empty provider result becomes `"x86"`; every other value passes through. |
| `-is64`, `-isactive`, `-isadmin`, `-isbeacon`, `-isssh` | 1 | `SessionID` is populated and the provider value is normalized through Sleep truthiness to the canonical predicate result. |

`AggressorSessionQuery` also carries `Name`, `RuntimeID`, `Script`, and `Span`.
The documented array/hash/string shapes are not enforced after provider
dispatch. Return a detached array or hash when script mutation must not update
the application's live session model.

### Data-model queries: 3 functions

Install `AggressorDataModelQueryProvider` with
`WithAggressorDataModelQueryProvider`; the adapter is
`AggressorDataModelQueryProviderFunc` and the method is:

```go
QueryAggressorDataModel(context.Context, opfor.AggressorDataModelQuery) (opfor.Value, error)
```

| Functions | Arity | Request and typed result |
| --- | ---: | --- |
| `data_keys`, `pivots` | 0 | `Kind` distinguishes the query and `Key` is `$null`. The provider value passes through unchanged. |
| `data_query` | 1 | `Key` is resolved once and transferred without coercion. The provider value passes through unchanged. |

The request also contains `Name`, `RuntimeID`, `Script`, and `Span`. OPFOR does
not sort, clone, validate, or define unknown-key behavior for these results.

### Application data stores: 17 functions

Install `AggressorDataStoreProvider` with
`WithAggressorDataStoreProvider`; the adapter is
`AggressorDataStoreProviderFunc` and the method is:

```go
HandleAggressorDataStore(context.Context, opfor.AggressorDataStoreRequest) (opfor.Value, error)
```

| Functions | Arity | Positional request shape and typed result |
| --- | ---: | --- |
| `credential_add` | 2-7 | Username, secret, then optional realm, source, host, secret type, and notes. The provider value passes through. |
| `credentials`, `applications`, `archives`, `downloads`, `keystrokes`, `screenshots`, `services`, `targets`, `hosts`, `resetData` | 0 | No arguments. The provider value passes through. |
| `tokenToEmail` | 1 | Token. The provider value passes through. |
| `highlight` | 3 | Model, rows array, accent. The provider value passes through. |
| `host_info` | 1-2 | Host and optional key. The provider value passes through. |
| `host_update` | 4-5 | Host, DNS name, OS, version, optional note. The provider value passes through. |
| `host_delete` | 1 | One host or an array of hosts. The provider value passes through. |
| `redactobject` | 1 | Post-exploitation object ID. The provider result is discarded and the script receives `$null`. |

`AggressorDataStoreRequest` carries `Operation`, exact `Name`, provenance, and
a detached top-level `Arguments` slice. Use `Arg(index)` for a null-on-missing
lookup and `HasArgument(index)` when omission must remain distinct from an
explicit `$null`. Values inside the slice retain compound and object identity.
Except for `redactobject`, OPFOR intentionally preserves the provider's result
because the public reference does not define every mutation return convention.

### Preferences: 4 functions

Install `AggressorPreferenceProvider` with
`WithAggressorPreferenceProvider`; the adapter is
`AggressorPreferenceProviderFunc` and the method is:

```go
HandleAggressorPreference(context.Context, opfor.AggressorPreferenceRequest) (opfor.Value, error)
```

| Function | Arity | Request fields and typed result |
| --- | ---: | --- |
| `pref_get` | 2 | `PreferenceName`, `DefaultValue`; provider value passes through. |
| `pref_get_list` | 1 | `PreferenceName`; provider value passes through. |
| `pref_set` | 2 | `PreferenceName`, `PreferenceValue`; provider result is discarded and the script receives `$null`. |
| `pref_set_list` | 2 | `PreferenceName`, array `PreferenceValue`; provider result is discarded and the script receives `$null`. |

The request has `Operation`, `RuntimeID`, `Script`, and `Span`; it does not have
a separate `Name` field because every operation has one spelling. OPFOR
requires the second `pref_set_list` argument to be an array. It does not clone
that array or coerce names and values.

### Profile and Team Server configuration: 3 functions

Install `AggressorProfileProvider` with `WithAggressorProfileProvider`; the
adapter is `AggressorProfileProviderFunc` and the method is:

```go
HandleAggressorProfile(context.Context, opfor.AggressorProfileRequest) (opfor.Value, error)
```

| Function | Arity | Request fields and typed result |
| --- | ---: | --- |
| `killdate` | 0 | Only operation, name, and provenance are populated. |
| `setup_strings` | 1 | `Payload` is populated. |
| `setup_transformations` | 2 | `Payload` and `Architecture` are populated. |

All three successful provider values pass through without coercion or cloning.
The request also carries exact `Name`, `RuntimeID`, `Script`, and `Span`.

### Covert VPN state: 4 functions

Install `AggressorVPNProvider` with `WithAggressorVPNProvider`; the adapter is
`AggressorVPNProviderFunc` and the method is:

```go
HandleAggressorVPN(context.Context, opfor.AggressorVPNRequest) (opfor.Value, error)
```

| Function | Arity | Request fields and typed result |
| --- | ---: | --- |
| `vpn_interfaces` | 0 | Query; provider value passes through. |
| `vpn_interface_info` | 1-2 | `Interface`, optional `Key`, and `HasKey`. The provider value passes through. |
| `vpn_tap_create` | 5 | `Interface`, `MACAddress`, `Reserved`, `Port`, `Channel`. Provider result is discarded and the script receives `$null`. |
| `vpn_tap_delete` | 1 | `Interface`. Provider result is discarded and the script receives `$null`. |

The request also contains `Operation`, exact `Name`, and provenance. `HasKey`
is the only reliable way to distinguish an omitted key from explicit `$null`.

### Connected-client services: 12 functions

Install `AggressorClientServiceProvider` with
`WithAggressorClientServiceProvider`; the adapter is
`AggressorClientServiceProviderFunc` and the method is:

```go
HandleAggressorClientService(context.Context, opfor.AggressorClientServiceRequest) (opfor.Value, error)
```

| Functions | Arity | Request and typed result |
| --- | ---: | --- |
| `getAggressorClient`, `get_cs_version`, `mynick`, `users` | 0 | Query values pass through. An object returned by `getAggressorClient` retains identity and remains subject to `ObjectHost`. |
| `action`, `elog`, `say` | 1 | Message in `Arguments`; provider result is discarded. |
| `privmsg` | 2 | Recipient and message; provider result is discarded. |
| `custom_event` | 2 | Topic and data; provider result is discarded. |
| `custom_event_private` | 3 | Recipient, topic, data; provider result is discarded. |
| `closeClient` | 0 | Side effect; provider result is discarded. |
| `sync_download` | 2-3 | Remote path and local destination remain in `Arguments`. The optional third position becomes `CallbackState` and `Callback` and is removed from `Arguments`; provider result is discarded. |

`AggressorClientServiceRequest` carries `Operation`, exact `Name`, provenance,
and generation-bound `Bindings`. For `sync_download`, switch on
`AggressorCallbackOmitted`, `AggressorCallbackNull`, or
`AggressorCallbackCallable`; do not infer presence from `Callback == nil`.
The callable is retained, multi-shot, and must be invoked with a new
caller-owned context. Conventionally its first argument is the synchronized
local path. It rejects calls after generation retirement, script unload, or
runtime close.

## Compilable implementation skeleton

This in-memory adapter demonstrates all seven interfaces. A production adapter
would replace its placeholder records with synchronized client or Team Server
state while preserving the same operation switches and result policies.

```go
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/sliverarmory/opfor"
)

type queryStateServices struct {
	mu          sync.RWMutex
	preferences map[string]opfor.Value
}

func (p *queryStateServices) QueryAggressorSession(
	ctx context.Context,
	query opfor.AggressorSessionQuery,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch query.Kind {
	case opfor.AggressorSessionQueryBeacons, opfor.AggressorSessionQueryBeaconIDs:
		return opfor.ArrayValue(opfor.NewArray(opfor.String("demo"))), nil
	case opfor.AggressorSessionQueryBeaconData:
		row := opfor.NewHash()
		row.Set("id", query.SessionID)
		return opfor.HashValue(row), nil
	case opfor.AggressorSessionQueryBeaconInfo:
		return opfor.String("demo-value"), nil
	case opfor.AggressorSessionQueryBeaconArchitecture:
		return opfor.String("x64"), nil
	case opfor.AggressorSessionQueryIs64,
		opfor.AggressorSessionQueryIsActive,
		opfor.AggressorSessionQueryIsAdmin,
		opfor.AggressorSessionQueryIsBeacon,
		opfor.AggressorSessionQueryIsSSH:
		return opfor.Bool(true), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported session query %q", query.Name)
	}
}

func (p *queryStateServices) QueryAggressorDataModel(
	ctx context.Context,
	query opfor.AggressorDataModelQuery,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch query.Kind {
	case opfor.AggressorDataModelQueryKeys:
		return opfor.ArrayValue(opfor.NewArray(opfor.String("demo"))), nil
	case opfor.AggressorDataModelQueryValue:
		return opfor.String("value for " + query.Key.String()), nil
	case opfor.AggressorDataModelQueryPivots:
		return opfor.ArrayValue(opfor.NewArray()), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported data-model query %q", query.Name)
	}
}

func (p *queryStateServices) HandleAggressorDataStore(
	ctx context.Context,
	request opfor.AggressorDataStoreRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorDataStoreCredentials,
		opfor.AggressorDataStoreApplications,
		opfor.AggressorDataStoreArchives,
		opfor.AggressorDataStoreDownloads,
		opfor.AggressorDataStoreKeystrokes,
		opfor.AggressorDataStoreScreenshots,
		opfor.AggressorDataStoreServices,
		opfor.AggressorDataStoreTargets,
		opfor.AggressorDataStoreHosts:
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorDataStoreTokenToEmail:
		return opfor.String("unknown"), nil
	case opfor.AggressorDataStoreHostInfo:
		return opfor.HashValue(opfor.NewHash()), nil
	case opfor.AggressorDataStoreCredentialAdd,
		opfor.AggressorDataStoreHighlight,
		opfor.AggressorDataStoreHostUpdate,
		opfor.AggressorDataStoreHostDelete,
		opfor.AggressorDataStoreResetData,
		opfor.AggressorDataStoreRedactObject:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported data-store operation %q", request.Name)
	}
}

func (p *queryStateServices) HandleAggressorPreference(
	ctx context.Context,
	request opfor.AggressorPreferenceRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	key := request.PreferenceName.String()
	switch request.Operation {
	case opfor.AggressorPreferenceGet, opfor.AggressorPreferenceGetList:
		p.mu.RLock()
		value, ok := p.preferences[key]
		p.mu.RUnlock()
		if ok {
			return value, nil
		}
		if request.Operation == opfor.AggressorPreferenceGet {
			return request.DefaultValue, nil
		}
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorPreferenceSet, opfor.AggressorPreferenceSetList:
		p.mu.Lock()
		p.preferences[key] = request.PreferenceValue
		p.mu.Unlock()
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported preference operation %q", request.Operation)
	}
}

func (p *queryStateServices) HandleAggressorProfile(
	ctx context.Context,
	request opfor.AggressorProfileRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorProfileKillDate:
		return opfor.String(""), nil
	case opfor.AggressorProfileSetupStrings,
		opfor.AggressorProfileSetupTransformations:
		return request.Payload, nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported profile operation %q", request.Name)
	}
}

func (p *queryStateServices) HandleAggressorVPN(
	ctx context.Context,
	request opfor.AggressorVPNRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorVPNInterfaces:
		return opfor.ArrayValue(opfor.NewArray()), nil
	case opfor.AggressorVPNInterfaceInfo:
		return opfor.HashValue(opfor.NewHash()), nil
	case opfor.AggressorVPNTAPCreate, opfor.AggressorVPNTAPDelete:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported VPN operation %q", request.Name)
	}
}

func (p *queryStateServices) HandleAggressorClientService(
	ctx context.Context,
	request opfor.AggressorClientServiceRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	switch request.Operation {
	case opfor.AggressorClientServiceGetAggressorClient:
		return opfor.ObjectValue(p), nil
	case opfor.AggressorClientServiceGetCSVersion:
		return opfor.String("demo"), nil
	case opfor.AggressorClientServiceMyNick:
		return opfor.String("operator"), nil
	case opfor.AggressorClientServiceUsers:
		return opfor.ArrayValue(opfor.NewArray(opfor.String("operator"))), nil
	case opfor.AggressorClientServiceSyncDownload:
		if request.CallbackState == opfor.AggressorCallbackCallable {
			callbackCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			_, err := request.Callback.Invoke(callbackCtx, request.Arguments[1])
			return opfor.Null(), err
		}
		return opfor.Null(), nil
	case opfor.AggressorClientServiceAction,
		opfor.AggressorClientServiceEventLog,
		opfor.AggressorClientServiceSay,
		opfor.AggressorClientServicePrivateMessage,
		opfor.AggressorClientServiceCustomEvent,
		opfor.AggressorClientServiceCustomEventPrivate,
		opfor.AggressorClientServiceCloseClient:
		return opfor.Null(), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported client-service operation %q", request.Name)
	}
}

func main() {
	providers := &queryStateServices{preferences: make(map[string]opfor.Value)}
	runtime, err := opfor.New(
		opfor.WithAggressorSessionQueryProvider(providers),
		opfor.WithAggressorDataModelQueryProvider(providers),
		opfor.WithAggressorDataStoreProvider(providers),
		opfor.WithAggressorPreferenceProvider(providers),
		opfor.WithAggressorProfileProvider(providers),
		opfor.WithAggressorVPNProvider(providers),
		opfor.WithAggressorClientServiceProvider(providers),
	)
	if err != nil {
		panic(err)
	}
	defer runtime.Close(context.Background())
}
```

## Lifecycle, inheritance, and ownership

The [shared provider contract](provider-contract.md) applies in full. In
particular:

- methods are synchronous, may overlap for independent executions, must
  observe `ctx`, and must not retain it;
- source arguments are resolved once into detached top-level requests, but
  arrays, hashes, functions, objects, and nested compound values keep reference
  identity;
- return a detached graph whenever script mutation must not affect application
  state;
- provider errors reject the call and are never retried through `Host`; and
- `RuntimeID` and `Script` form the provenance identity; `ScriptID` alone is
  only runtime-local.

Portable `ScriptLoader` children inherit the exact provider instances. The
child is a fresh runtime with a different `RuntimeID`, fresh registrations and
generation state, and child-source `Span` values. The shared provider must
therefore be concurrency-safe and must key runtime-local Script IDs together
with `RuntimeID`. A retained `sync_download` callback or `Bindings` capability
belongs to the child generation that produced it and is revoked independently
of the parent.

## Tests and drift checks

Run the focused white-box tests while implementing this group:

```sh
go test ./internal/opfor -run 'TestAggressor(SessionQuery|DataModelQuery|DataStore|Preference|Profile|VPN|ClientService|SyncDownload)'
go test ./internal/opfor -run 'Test(RuntimeIDsAndPortableScriptLoaderQueryProviderInheritance|PortableScriptLoaderInheritsAggressor(DataModelQuery|DataStore|Preference|Profile|VPN|ClientService)Provider)'
```

For adapter tests, cover every operation constant, minimum/maximum arity,
optional-position state, value-versus-null result policy, provider error,
pre-canceled and canceled-during-call contexts, `WithFunction` precedence,
unset-provider Host fallback, concurrent calls, and ScriptLoader provenance.

To detect API drift, filter `opfor.DefaultAggressorFunctionContracts()` by
`TypedProvider` and assert the exact sorted names and counts documented here:
`AggressorSessionQueryProvider` 12, `AggressorDataModelQueryProvider` 3,
`AggressorDataStoreProvider` 17, `AggressorPreferenceProvider` 4,
`AggressorProfileProvider` 3, `AggressorVPNProvider` 4, and
`AggressorClientServiceProvider` 12. The inventory also supplies the enforced
arity, typed result, callback positions, authoritative-error rule, and Host
fallback flag; treat it as the machine-readable contract rather than copying
those policies into application code.
