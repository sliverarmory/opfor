# Catalogs, bindings, and host-driven dispatch

This guide covers the Aggressor surfaces where scripts register metadata or
callbacks and an embedding application later inspects or invokes them. It
includes command-help catalogs, Beacon technique catalogs and actions,
function/declaration registrations, `BindingObserver`, `AggressorBindings`, and
the public host-driven entry APIs.

Read the [Aggressor extension index](README.md) first and apply the lifecycle,
ownership, precedence, and error rules in the
[shared provider contract](provider-contract.md). UI-specific popup composition
is described in [client UI and execution control](client-ui-execution-control.md).

There are two directions to keep distinct:

- **script to host:** a declaration is published through `BindingObserver`, or
  a catalog snapshot is read by the host;
- **host to script:** the application invokes a guarded `Callable`, a public
  `Runtime` dispatch method, or `AggressorBindings` carried by a typed request.

Catalog metadata is not an executable callback. In particular, importer-seeded
Beacon technique entries advertise names and descriptions only.

## Seed command-help catalogs

Beacon and SSH command help are independent namespaces selected by
`AggressorCommandBeacon` and `AggressorCommandSSH`.

```go
runtime, err := opfor.New(
	opfor.WithAggressorCommandCatalog(
		opfor.AggressorCommandBeacon,
		opfor.AggressorCommandCatalog{
			Groups: []opfor.AggressorCommandGroup{{
				ID: "core", Name: "Core", Description: "Core commands",
			}},
			Commands: []opfor.AggressorCommandMetadata{{
				Name: "status", Description: "show status",
				Detail: "Synopsis: status", GroupID: "core",
			}},
		},
	),
)
```

`WithAggressorCommandCatalog` validates and defensively copies its input while
`New` applies options. Names and group IDs must be non-empty and unique within
their kind; group IDs may not contain `,` or `@`; every command group reference
must resolve in the same base catalog. Repeating the option for one kind
replaces that kind's earlier base without changing the other kind.

### Script functions

The Beacon and SSH families have identical contracts:

| Functions | Arity | Result |
| --- | ---: | --- |
| `beacon_command_register`, `ssh_command_register` | 3–4 | register `(name, description, detail[, groupID])`; `$null` |
| `beacon_command_group`, `ssh_command_group` | 3 | register `(id, name, description)`; `$null` |
| `beacon_command_describe`, `ssh_command_describe` | 1 | description string or `$null` when missing |
| `beacon_command_detail`, `ssh_command_detail` | 1 | detail string or `$null` when missing |
| `beacon_commands`, `ssh_commands` | 0 | array of command names |

These functions manage help metadata only. Executable `command`, `alias`, and
`ssh_alias` registrations live in the binding registry described below.

Script registrations layer over the immutable importer base. The newest layer
for a name supplies metadata, while name enumeration retains deterministic
first-insertion order. A repeated registration by one script coalesces its
layer. Unload or failed-load rollback reveals the preceding script or base
layer. Effective snapshots omit a group until an active command references it,
so a group cannot remain visible as a dangling association. The optional
fourth argument to `*_command_register` associates the command only when that
group is active at registration time; creating the group later does not
retroactively associate the command.

Use a detached effective snapshot for host UI:

```go
catalog, err := runtime.SnapshotAggressorCommandCatalog(
	opfor.AggressorCommandBeacon,
)
if err != nil {
	return err
}
for _, command := range catalog.Commands {
	registerHelp(command.Name, command.Description, command.Detail)
}
```

Mutating the returned slices does not change runtime state.

## Seed Beacon technique catalogs

Four independent registries are selected by:

- `AggressorBeaconTechniqueElevator`;
- `AggressorBeaconTechniqueExploit`;
- `AggressorBeaconTechniqueRemoteExecMethod`; and
- `AggressorBeaconTechniqueRemoteExploit`.

```go
opfor.WithAggressorBeaconTechniqueCatalog(
	opfor.AggressorBeaconTechniqueRemoteExploit,
	opfor.AggressorBeaconTechniqueCatalog{
		Techniques: []opfor.AggressorBeaconTechniqueMetadata{{
			Name: "example",
			Description: "importer-advertised example",
			Architecture: "x64",
		}},
	},
)
```

Names are non-empty, unique, and case-sensitive. Remote-exploit entries require
exactly `x86` or `x64`; every other kind rejects a non-empty architecture.
Options and snapshots are defensively copied. As with command catalogs, later
script layers win without changing first-insertion order, and unload restores
the earlier layer.

### Script functions

| Registry | Register | Describe | Enumerate | Extra query |
| --- | --- | --- | --- | --- |
| elevator | `beacon_elevator_register(name, description, callback)` | `beacon_elevator_describe(name)` | `beacon_elevators()` | — |
| local exploit | `beacon_exploit_register(name, description, callback)` | `beacon_exploit_describe(name)` | `beacon_exploits()` | — |
| remote-exec method | `beacon_remote_exec_method_register(name, description, callback)` | `beacon_remote_exec_method_describe(name)` | `beacon_remote_exec_methods()` | — |
| remote exploit | `beacon_remote_exploit_register(name, architecture, description, callback)` | `beacon_remote_exploit_describe(name)` | `beacon_remote_exploits()` | `beacon_remote_exploit_arch(name)` |

Registration functions return `$null`; describe/architecture return a string
or `$null` when missing; enumerate returns an array. Script callbacks are
generation-owned and are deliberately absent from
`AggressorBeaconTechniqueCatalog`, making snapshots safe metadata views.

### Invoke a technique callback directly

`Runtime.InvokeAggressorBeaconTechnique` invokes an effective **script-owned**
callback. It never tasks a Beacon and never calls Host. Base-only or missing
entries therefore return a typed `UnsupportedError`.

```go
result, err := runtime.InvokeAggressorBeaconTechnique(
	ctx,
	opfor.AggressorBeaconTechniqueRemoteExecMethod,
	"wmiexec",
	opfor.String(beaconID),
	opfor.String(target),
	opfor.String(rawCommand),
)
```

Callback ABIs are exact:

| Kind | Callback values |
| --- | --- |
| elevator | `(Beacon ID, raw command)` |
| local exploit | `(Beacon ID, listener)` |
| remote-exec method | `(Beacon ID, target, raw command)` |
| remote exploit | `(Beacon ID, target, listener)` |

The call is lifecycle-guarded and instruction-metered. Retaining a catalog
snapshot does not retain the executable callback.

### Script-side technique actions

Four portable native functions bridge a script call to an effective technique
callback:

| Function | Arity | Registry and callback ABI |
| --- | ---: | --- |
| `belevate_command(ids, name, rawCommand)` | 3 | elevator `(id, rawCommand)` |
| `belevate(ids, name, listener)` | 3 | local exploit `(id, listener)` |
| `bremote_exec(ids, name, target, rawCommand)` | 4 | remote-exec method `(id, target, rawCommand)` |
| `bjump(ids, name, target, listener)` | 4 | remote exploit `(id, target, listener)` |

A scalar ID is invoked once. A top-level ID array is snapshotted and invoked
sequentially; nested arrays remain individual values and are not flattened.
Each callback receives its own argument slice, callback results are discarded,
and successful actions return `$null`.

When an effective script callback exists, it is authoritative: the first error
stops fan-out and never falls through to Host. If the name is missing or is
present only in the importer base, the original reference-bearing invocation
reaches Host exactly once; a successful Host value is discarded and the action
returns `$null`.

## Aggressor declaration registrations

Declarations compile to script-owned `Binding` records. Several source
keywords intentionally share a canonical `BindingKind`:

| Declaration | `BindingKind` | Lifetime and selection |
| --- | --- | --- |
| `on name { ... }` | `BindingEvent` | persistent; all exact layers, then wildcard layers |
| `when name { ... }` | `BindingEvent` | `BindingOnce`; atomically consumed before its first selected dispatch |
| `command name { ... }` | `BindingCommand` | persistent; newest layer for console invocation |
| `alias name { ... }` | `BindingAlias` | persistent; newest Beacon alias layer |
| `ssh_alias name { ... }` | `BindingSSHAlias` | persistent; newest SSH alias layer |
| `set name { ... }`, `hook name { ... }` | `BindingHook` | persistent; newest layer |
| `popup name { ... }` | `BindingPopup` | persistent additive root layers |
| `menu description { ... }`, `menubar description { ... }` | `BindingMenu` | persistent descendant of active composition |
| `item description { ... }` | `BindingItem` | persistent descendant of active composition |
| `bind shortcut { ... }` | `BindingKey` | persistent; newest exact shortcut layer |

`Binding.Keyword` preserves the source spelling while `Kind` provides the
canonical dispatch category. `Selectors` preserve source order, raw spelling,
evaluated values, and spans. `Parent` is a detached `BindingInvocation` chain
for popup/menu/item descendants. Re-composing a popup or menu retires its prior
ephemeral descendant tree before publishing a new one.

`sub`, `inline`, and `filter` also appear as bindings, but they are core Sleep
declarations rather than Aggressor host registrations.

### Function forms and local helpers

The stock function namespace shares the same registries for:

| Function | Effect |
| --- | --- |
| `on(name, callback)` | persistent event registration |
| `when(name, callback)` | one-shot event registration |
| `alias(name, callback)` | Beacon alias registration |
| `ssh_alias(name, callback)` | SSH alias registration |
| `bind(shortcut, callback)` | key-binding registration |
| `alias_clear(name)` | remove every exact Beacon-alias layer; does not touch SSH aliases or help metadata |
| `unbind(shortcut)` | remove every exact key-binding layer |
| `fireAlias(beaconID, name, rawArguments)` | invoke the newest Beacon alias and discard its result |
| `fireEvent(name, values...)`, `fire_event(name, values...)` | dispatch a runtime-local event and discard results |
| `insert_menu(popupName, values...)` | compose every exact popup layer beneath the active popup/menu invocation |

The `item` and `menu` **declaration** forms are supported. Same-shaped function
helpers exist internally for focused compatibility testing but are
evidence-gated and are not installed in the default function namespace.

`WithFunction` has highest precedence over every installed function form.
`WithEnvironment(keyword, kind)` registers an importer-defined ordinary,
filter, or predicate declaration. Compile through the configured runtime so
the parser sees it; standalone compilation requires the corresponding
`WithCompileEnvironment` option. Custom binding callbacks and predicates have
the same script-generation lifetime as built-ins.

## Observe registration lifecycle

Install `WithBindingObserver` to mirror active registrations into an
application-owned command, event, or UI registry:

The following key is sufficient only when one observer instance serves one
runtime and portable `ScriptLoader` children are not used. Both identifiers
are runtime-local, while `Binding` does not carry a `RuntimeID`.

```go
type bindingKey struct {
	script opfor.ScriptID
	id     uint64
}

type bindingRegistry struct {
	mu      sync.Mutex
	entries map[bindingKey]opfor.Binding
}

func (registry *bindingRegistry) Registered(
	ctx context.Context,
	binding opfor.Binding,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	registry.mu.Lock()
	if registry.entries == nil {
		registry.entries = make(map[bindingKey]opfor.Binding)
	}
	registry.entries[bindingKey{binding.Script, binding.ID}] = binding
	registry.mu.Unlock()
	return nil
}

func (registry *bindingRegistry) Unregistered(
	ctx context.Context,
	binding opfor.Binding,
) error {
	registry.mu.Lock()
	delete(registry.entries, bindingKey{binding.Script, binding.ID})
	registry.mu.Unlock()
	return ctx.Err()
}
```

Notifications are synchronous and may be concurrent for independent
executions. They run outside registry locks, so a careful observer may reenter
public runtime APIs, but it must not assume a retained binding is still active
after the callback returns.

`Registered` runs after publication. Returning an error removes the new binding
and rejects the declaration; OPFOR does not send a compensating
`Unregistered` for that rollback. `Unregistered` runs after authoritative
removal, and its error is reported but cannot restore the registration.
One-shot event retirement is observed before selected callbacks begin; an
observer error is joined with callback errors but does not resurrect or suppress
the consumed handler.

Binding slices and parent metadata are detached snapshots. Compound selector
values retain ordinary identity. `Callback` and `Predicate` are guarded
script-owned capabilities and reject invocation after generation retirement or
script unload. Within one runtime, key retained entries by
`(ScriptID, Binding.ID)`, not name alone, because names are layered.

Do not use that pair as a process-wide key. The same observer is inherited by
portable `ScriptLoader` children, but `ScriptID` and `Binding.ID` restart in
each child and observer notifications do not include the child's `RuntimeID`.
An application that needs one authoritative cross-runtime mirror must either
exclude ScriptLoader children from that mirror or add a separate,
application-owned runtime-provenance boundary; the observer alone cannot
distinguish colliding parent and child identities.

## Host-driven entry through `Runtime`

The raw runtime APIs are appropriate when the application deliberately owns
the runtime:

| API | Selection and ABI |
| --- | --- |
| `Bindings(kind, name)` | detached active registrations in load order; empty name selects all of a kind |
| `BindingByID(scriptID, bindingID)` | exact active identity |
| `Dispatch(kind, name, values...)` | every exact layer in load order; event wildcards after exact handlers |
| `DispatchEvent(name, values...)` | event specialization of `Dispatch` |
| `DispatchPopupHook(name, values...)` | every additive popup layer |
| `InvokeBinding(kind, name, values...)` | newest exact layer |
| `InvokeBindingByID(scriptID, bindingID, values...)` | one exact retained registration |
| `InvokeConsole(ConsoleInvocation)` | newest command, alias, or SSH alias with console ABI |
| `InvokeAggressorBeaconTechnique(...)` | effective script-owned technique callback |

Dispatch takes a stable registration snapshot. Persistent handlers remain;
every selected `when` is atomically removed before any callback runs, so
concurrent dispatch or unload has one winner. A callback registered during a
dispatch waits for a later dispatch. Callback failures are joined and do not
stop later event handlers.

Exact event handlers receive the concrete event name as Sleep's separate `$0`
and payload values in `$1...`/`@_`. Wildcard `on "*"` handlers run afterward;
they receive the concrete name in both `$0` and positional `$1`, followed by
the payload.

`InvokeConsole` separates ASCII whitespace and supports double-quoted groups.
For a `command`, `$0` is the unmodified raw line and parsed arguments begin at
`$1`. For `alias` and `ssh_alias`, `$0` is still the raw line, the supplied
session ID is `$1`, and parsed arguments begin at `$2`. `@_` contains only the
positional values, never `$0`.

Top-level host entry creates one fresh instruction budget; synchronous nested
reentry reuses the active budget. Calls honor cancellation and acquire runtime
and script-generation execution leases.

## Restricted entry through `AggressorBindings`

Nine typed request families carry `AggressorBindings`: artifact, Beacon action,
Beacon execution, client service, client UI, listener, payload,
process-injection, and site requests. Prefer this capability over retaining a
raw `*Runtime` from provider code.

```go
bindings := request.Bindings
if !bindings.Valid() {
	return opfor.Null(), opfor.ErrAggressorBindingsUnavailable
}

results, err := bindings.DispatchEvent(
	ctx,
	"custom_event_adapter_ready",
	opfor.String("ready"),
)
```

The exposed methods are:

```go
func (AggressorBindings) Valid() bool
func (AggressorBindings) RuntimeID() RuntimeID
func (AggressorBindings) Same(AggressorBindings) bool
func (AggressorBindings) DispatchEvent(context.Context, string, ...Value) ([]Value, error)
func (AggressorBindings) InvokeHook(context.Context, string, ...Value) (Value, error)
func (AggressorBindings) InvokePopupHook(context.Context, string, ...Value) (Value, error)
func (AggressorBindings) DispatchPopupHook(context.Context, string, ...Value) ([]Value, error)
```

The capability is bound to the exact runtime and, for script-originated
requests, the exact creating generation. Retaining it retains the runtime but
does not expose evaluator, scope, or registry pointers. `Valid` reports
structural presence, not liveness; it remains true after revocation, while
execution returns `ErrScriptUnloaded` or `ErrRuntimeClosed`. `Same` compares
runtime identity only, not generation or liveness. The zero value returns
`ErrAggressorBindingsUnavailable`.

Calls use the new caller-owned context, enforce limits, and never retarget a
later run of the same `Script`. Avoid invoking them while holding application
locks needed by a provider or observer reached during reentry.

## ScriptLoader children

A portable `ScriptLoader` child is a fresh runtime with its own nonzero
`RuntimeID`, script IDs, binding registries, command layers, and technique
callbacks.

It inherits:

- importer command and technique **base catalogs**;
- `WithBindingObserver` and custom environment registrations;
- generic Host and the Aggressor extension profile used by its parent; and
- the same rules again for nested ScriptLoader children.

It does not inherit parent script overlays, executable technique callbacks,
events, hooks, aliases, popup/menu trees, one-shot state, or other
script-lifecycle state. A child script sees its inherited base plus its own
registrations. `AggressorBindings` delivered by a child request targets the
child runtime and generation, never the parent.

## Tests and drift checks

An adapter should test:

- base validation, defensive copying, repeated options, and detached snapshots;
- duplicate-name ordering, same-owner coalescing, failed-load rollback, and
  unload restoration;
- callback ABI, revocation, first-error policy, array fan-out, and Host fallback
  for technique actions;
- observer rollback/error behavior and exactly-once unregistration;
- event exact/wildcard order and atomic one-shot consumption;
- newest-by-name versus exact-by-ID invocation and console `$0`/positional ABI;
- synchronous reentry, cancellation, instruction budgets, and concurrent
  dispatch/unload; and
- ScriptLoader base-only inheritance and child-local capabilities.

`DefaultAggressorFunctionContracts` does not by itself cover these portable
catalog and binding functions. Guard exact names and classifications with the
public catalog as well:

```go
for _, name := range expectedNames {
	entry, ok := aggressor.Lookup(aggressor.KindFunction, name)
	if !ok {
		t.Fatalf("Aggressor function %q disappeared from the catalog", name)
	}
	if entry.Support != aggressor.SupportPortableDefault {
		t.Fatalf("Aggressor function %q support = %s", name, entry.Support)
	}
}
```

The focused runtime regressions are in
[`aggressor_command_catalog_test.go`](../../internal/opfor/aggressor_command_catalog_test.go),
[`aggressor_beacon_technique_catalog_test.go`](../../internal/opfor/aggressor_beacon_technique_catalog_test.go),
[`builtins_aggressor_beacon_technique_actions_test.go`](../../internal/opfor/builtins_aggressor_beacon_technique_actions_test.go),
[`builtins_aggressor_bindings_test.go`](../../internal/opfor/builtins_aggressor_bindings_test.go),
[`builtins_aggressor_menu_bindings_test.go`](../../internal/opfor/builtins_aggressor_menu_bindings_test.go),
[`aggressor_callback_bindings_test.go`](../../internal/opfor/aggressor_callback_bindings_test.go),
and [`environment_spec_test.go`](../../internal/opfor/environment_spec_test.go).
