# Beacon tasking, execution, RPC, and transcript integrations

This guide covers the importer boundaries used for Beacon operational actions,
low-level execution, Team Server RPC, and Beacon transcript output. Read the
[shared provider and callback contract](provider-contract.md) first; it defines precedence,
typed request ownership, retained capabilities, context handling, and generic
`Host` behavior for every guide in this directory.

The four boundaries have deliberately different fallback policies:

| Area | Configuration | Missing configuration |
| --- | --- | --- |
| Operational actions | `WithAggressorBeaconActionProvider` | The original unresolved `Invocation` is sent to `Host`. |
| Low-level execution | `WithAggressorBeaconExecutionProvider` | Six wrappers send the original unresolved `Invocation` to `Host`; `beacon_inline_execute` uses the specialized bridge described below. |
| Team Server RPC | `WithAggressorTeamServerRPCProvider` | The original unresolved `Invocation` is sent to `Host`. |
| Transcript output | `WithAggressorBeaconTranscriptSink` | OPFOR writes its stock synchronized presentation to stdout. `Host` is never called. |

For the first three rows, a configured provider is authoritative. A provider
error never asks OPFOR to retry through `Host`, even when the error is an
`UnsupportedError`, because the provider may already have issued a task or
external effect. Exact-name `WithFunction`, runtime, and script overrides still
take precedence over all native wrappers.

Without a typed execution provider, `beacon_inline_execute` still applies the
active `BEACON_INLINE_EXECUTE` hook, replaces the forwarded content with the
detached binary result, resolves and lifecycle-guards an optional callback in
source position 5, and then calls `Host`. Its script-visible result is always
`$null`, even when Host returns a value. Every other low-level execution
wrapper preserves the raw unresolved Host invocation and its Host result.

## Beacon operational actions

`WithAggressorBeaconActionProvider` installs one typed dispatcher for the
complete supported operational-action family:

```go
type AggressorBeaconActionProvider interface {
	DispatchAggressorBeaconAction(
		context.Context,
		opfor.AggressorBeaconAction,
	) error
}
```

`AggressorBeaconActionProviderFunc` adapts a function with the same signature.
The provider is called synchronously exactly once for each valid source call,
including when `Target` is an array. A nil error means the task was accepted;
the script-visible result is always `$null`.

### Request mapping

An `AggressorBeaconAction` contains:

| Field | Meaning |
| --- | --- |
| `Kind` | Stable canonical `AggressorBeaconActionKind`; its string value is the exact Aggressor function name. Prefer this for routing. |
| `Name` | Exact normalized function spelling used by the script. Use it for diagnostics when spelling itself matters. |
| `RuntimeID`, `Script`, `Span` | Runtime, script, and call-site provenance without exposing a `*Runtime`. Correlate `Script` with `RuntimeID`, because script IDs are runtime-local. |
| `Bindings` | Generation-bound `AggressorBindings` capability for calling registered events, hooks, and popup hooks in the originating runtime. |
| `Target` | The first source argument. An array remains one array `Value`; OPFOR does not fan it out. |
| `Arguments` | Remaining arguments in source order, excluding `Target` and any callback position. The top-level slice is detached. |
| `CallbackState` | `AggressorCallbackOmitted`, `AggressorCallbackNull`, or `AggressorCallbackCallable`. |
| `Callback` | Retained script-owned `Callable` only when `CallbackState` is callable; otherwise nil. |

Every source argument is resolved exactly once on the typed route. Scalar
values are immutable, but arrays, hashes, objects, functions, nested compound
graphs, and binary provenance retain their original identity. Snapshot or
detach data before retention when script mutation or lifecycle ownership is
undesirable.

Filesystem-looking arguments describe a remote task. OPFOR does not read a
`bupload` local path, interpret `bupload_raw` content, or perform local file
operations. The importer owns those effects.

### Authoritative 121-function inventory

Do not maintain a second hand-written routing inventory. The runtime-enforced
source of truth is `DefaultAggressorFunctionContracts`. Filter it by typed
provider when building or validating an adapter:

```go
func beaconActionContracts() []opfor.AggressorFunctionContract {
	all := opfor.DefaultAggressorFunctionContracts()
	result := make([]opfor.AggressorFunctionContract, 0, 121)
	for _, contract := range all {
		if contract.TypedProvider == "AggressorBeaconActionProvider" {
			result = append(result, contract)
		}
	}
	return result
}
```

The current inventory has 121 entries. The following exhaustive grouping is an
implementation aid, not another dispatch contract. Each name appears exactly
once; arity, callback metadata, deprecation state, result policy, and fallback
flags remain authoritative in the generated contract records.

| Suggested handler group | Count | Current names |
| --- | ---: | --- |
| Session control and configuration | 24 | `bargue_add`, `bargue_list`, `bargue_remove`, `bbeacon_config`, `bbeacon_gate`, `bbeacon_interpreter`, `bbeacon_interpreter_lint`, `bblockdlls`, `bcheckin`, `bclear`, `beacon_console_watermark`, `beacon_console_watermark_reset`, `beacon_job_hide_output`, `beacon_job_name`, `beacon_remove`, `bmode`, `bnote`, `bpause`, `bppid`, `bsetenv`, `bsleep`, `bsleepu`, `bsyscall_method`, `bexit` |
| Files, transfer, pipes, and Beacon data store | 17 | `bcancel`, `bcd`, `bcp`, `bdata_store_list`, `bdata_store_load`, `bdata_store_unload`, `bdownload`, `bdrives`, `bls`, `bmkdir`, `bmv`, `bpwd`, `bread_pipe`, `brm`, `btimestomp`, `bupload`, `bupload_raw` |
| Discovery, collection, and monitoring | 13 | `bclipboard`, `bdesktop`, `bgetprivs`, `bgetuid`, `bipconfig`, `bjobs`, `bkeylogger`, `bnet`, `bprintscreen`, `bps`, `breg_queryv`, `bscreenshot`, `bscreenwatch` |
| Execution, injection, jobs, PowerShell, and spawning | 26 | `bdllinject`, `bdllload`, `bdllspawn`, `bexecute`, `bexecute_assembly`, `binject`, `binline_execute`, `binline_execute_pe`, `bjob_send_data`, `bjobkill`, `bkill`, `bpowerpick`, `bpowershell`, `bpowershell_import`, `bpowershell_import_clear`, `bpsinject`, `brun`, `brunas`, `brunu`, `bshell`, `bshinject`, `bshspawn`, `bspawn`, `bspawnas`, `bspawnto`, `bspawnu` |
| Credentials, Kerberos, elevation, and tokens | 19 | `bdcsync`, `bgetsystem`, `bhashdump`, `bkerberos_ccache_use`, `bkerberos_ticket_purge`, `bkerberos_ticket_use`, `bloginuser`, `blogonpasswords`, `bmimikatz`, `bmimikatz_small`, `bpassthehash`, `brev2self`, `bsteal_token`, `btoken_store_remove`, `btoken_store_remove_all`, `btoken_store_show`, `btoken_store_steal`, `btoken_store_steal_and_use`, `btoken_store_use` |
| Networking, pivots, lateral movement, and tunnels | 22 | `bbrowserpivot`, `bbrowserpivot_stop`, `bconnect`, `bcovertvpn`, `blink`, `bportscan`, `bpsexec`, `bpsexec_command`, `brportfwd`, `brportfwd_local`, `brportfwd_stop`, `bsocks`, `bsocks_stop`, `bspunnel`, `bspunnel_local`, `bssh`, `bssh_key`, `bsudo`, `bunlink`, `beacon_link`, `beacon_stage_pipe`, `beacon_stage_tcp` |

### Callback-bearing action forms

Eighteen action forms have a callback-shaped argument. All callbacks are
retained and generation-bound. Only `bipconfig` requires a callable; the other
positions are optional and accept explicit `$null`. Their callback argument
arity is deliberately unspecified, so the importer must supply the exact
result values required by its Cobalt integration rather than relying on OPFOR
to manufacture an ABI.

| Function | One-based callback position | Required | Nullable |
| --- | ---: | --- | --- |
| `bbeacon_interpreter` | 4 | No | Yes |
| `bbeacon_interpreter_lint` | 3 | No | Yes |
| `bdllspawn` | 7 | No | Yes |
| `bexecute_assembly` | 5 | No | Yes |
| `bhashdump` | 4 | No | Yes |
| `binline_execute` | 4 | No | Yes |
| `binline_execute_pe` | 4 | No | Yes |
| `bipconfig` | 2 | Yes | No |
| `bls` | 3 | No | Yes |
| `bmimikatz` | 5 | No | Yes |
| `bmimikatz_small` | 5 | No | Yes |
| `bnet` | 7 | No | Yes |
| `bportscan` | 8 | No | Yes |
| `bpowerpick` | 5 | No | Yes |
| `bpowershell` | 4 | No | Yes |
| `bps` | 2 | No | Yes |
| `bpsinject` | 5 | No | Yes |
| `bread_pipe` | 8 | No | Yes |

An omitted callback and an explicit `$null` callback are observably different.
Always switch on `CallbackState`; do not infer state from `Callback == nil`.
A callable may be retained after the provider returns and invoked more than
once. Each later invocation needs a new caller-owned context and returns
`ErrScriptUnloaded` or `ErrRuntimeClosed` after lifecycle revocation.

### Implementing an action router

A table keyed by `AggressorBeaconActionKind` makes coverage testable and keeps
transport or client code out of the OPFOR callback itself:

```go
type BeaconActionHandler func(
	context.Context,
	opfor.AggressorBeaconAction,
) error

type BeaconActionAdapter struct {
	handlers map[opfor.AggressorBeaconActionKind]BeaconActionHandler
}

func (adapter *BeaconActionAdapter) DispatchAggressorBeaconAction(
	ctx context.Context,
	action opfor.AggressorBeaconAction,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handler := adapter.handlers[action.Kind]
	if handler == nil {
		return fmt.Errorf("Beacon action %q is not implemented", action.Kind)
	}
	return handler(ctx, action)
}

func (adapter *BeaconActionAdapter) Validate() error {
	for _, contract := range beaconActionContracts() {
		kind := opfor.AggressorBeaconActionKind(contract.Name)
		if adapter.handlers[kind] == nil {
			return fmt.Errorf("missing Beacon action handler for %q", kind)
		}
	}
	return nil
}
```

Install the adapter only after validation:

```go
actions := &BeaconActionAdapter{
	handlers: map[opfor.AggressorBeaconActionKind]BeaconActionHandler{
		opfor.AggressorBeaconActionChangeDirectory: taskChangeDirectory,
		// Register every other kind reported by beaconActionContracts.
	},
}
if err := actions.Validate(); err != nil {
	return err
}

runtime, err := opfor.New(
	opfor.WithAggressorBeaconActionProvider(actions),
)
```

A partial typed provider cannot decline one action into `Host`: its error is
authoritative. For deliberately partial name handling, leave the family
provider unset and use exact `WithFunction` overrides for the selected names,
or implement the complete compatibility policy in `Host`.

The adapter should normally pass `Target` through without expanding it. If the
external API itself requires per-session calls, that fan-out and its partial
failure policy belong to the importer, not OPFOR.

## Low-level Beacon execution

`WithAggressorBeaconExecutionProvider` installs:

```go
type AggressorBeaconExecutionProvider interface {
	HandleAggressorBeaconExecution(
		context.Context,
		opfor.AggressorBeaconExecutionRequest,
	) (opfor.Value, error)
}
```

`AggressorBeaconExecutionProviderFunc` adapts a function with that signature.
The request carries the same `Name`, `RuntimeID`, `Script`, `Span`, and
generation-bound `Bindings` provenance as Beacon actions.

### Function and field mapping

| Function | Source shape and populated request fields | Script-visible result |
| --- | --- | --- |
| `beacon_execute_job` | Four arguments: `BeaconID`, `Command`, `CommandArguments`, `Flags`. | Provider value is discarded; returns `$null`. |
| `beacon_execute_postex_job` | Three to six arguments: `BeaconID`, `PID`, `Content`, optional `PackedArguments`, optional callback, optional `MessageID`. `HasPackedArguments` and `HasMessageID` preserve omission. | Provider value is discarded; returns `$null`. |
| `beacon_inline_execute` | Four or five arguments: `BeaconID`, `Content`, `EntryPoint`, `PackedArguments`, optional callback. OPFOR applies the active `BEACON_INLINE_EXECUTE` hook to `Content` before either the typed provider or Host sees it. | Provider value is discarded; returns `$null`. |
| `beacon_inline_execute_pe` | Four or five arguments with the same fields as the inline form. No BOF hook is applied. | Provider value is discarded; returns `$null`. |
| `beacon_host_imported_script` | One argument populates `BeaconID`. | Provider value passes through unchanged as the hosted-script invocation string/value. |
| `beacon_host_script` | Two arguments populate `BeaconID` and `Content`. | Provider value passes through unchanged. |
| `get_postex_kit_callback_id` | No arguments. | Provider value passes through unchanged. |

`beacon_execute_postex_job`, `beacon_inline_execute`, and
`beacon_inline_execute_pe` use a callback in source position 5 when that
position is present. `CallbackState` distinguishes omission, explicit `$null`,
and a callable. A callable is retained, script-owned, and multi-shot. The
documented callback ABI is three values:

1. Beacon ID;
2. result;
3. information map.

OPFOR does not manufacture or validate those callback values; the execution
provider supplies them. The callback contract is described on
`AggressorBeaconExecutionRequest` even though these overloaded callback
positions are not currently duplicated in each machine-readable function
contract.

### Execution-provider skeleton

```go
execution := opfor.AggressorBeaconExecutionProviderFunc(func(
	ctx context.Context,
	request opfor.AggressorBeaconExecutionRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}

	switch request.Kind {
	case opfor.AggressorBeaconExecuteJob:
		return opfor.Null(), queueCommandJob(ctx, request)

	case opfor.AggressorBeaconExecutePostexJob,
		opfor.AggressorBeaconInlineExecute,
		opfor.AggressorBeaconInlineExecutePE:
		// Retain request.Callback if completion is asynchronous, but do not
		// retain ctx. Invoke the callback later with a worker-owned context.
		return opfor.Null(), queuePostex(ctx, request)

	case opfor.AggressorBeaconHostImportedScript,
		opfor.AggressorBeaconHostScript:
		return hostPowerShell(ctx, request)

	case opfor.AggressorBeaconPostexKitCallbackID:
		return opfor.Int(postexCallbackID), nil

	default:
		return opfor.Null(), fmt.Errorf(
			"Beacon execution kind %q is not implemented",
			request.Kind,
		)
	}
})

runtime, err := opfor.New(
	opfor.WithAggressorBeaconExecutionProvider(execution),
)
```

If the provider returns a non-null value for one of the four tasking forms,
OPFOR intentionally discards it. If no provider is configured, the six
non-inline wrappers send the exact raw `Invocation` to `Host`; argument
resolution and callback retention happen only on their typed route.
`beacon_inline_execute` instead applies its hook and guards its callback before
calling Host, as described above. A provider error is authoritative and never
retries.

## Team Server RPC

`WithAggressorTeamServerRPCProvider` handles the native `call` wrapper:

```go
type AggressorTeamServerRPCProvider interface {
	CallAggressorTeamServerRPC(
		context.Context,
		opfor.AggressorTeamServerRPCRequest,
	) error
}
```

`call` requires at least three source arguments. `Command` is the first,
`Callback` represents the second, and `Arguments` is a detached top-level copy
of the third and later values. Every value is resolved once on the typed route.
The callback position accepts either `$null` for fire-and-forget operation or a
callable. Other values are rejected.

`AggressorTeamServerRPCCallback` is a retained multi-shot value capability:

```go
func (callback AggressorTeamServerRPCCallback) Valid() bool
func (callback AggressorTeamServerRPCCallback) Respond(
	context.Context,
	opfor.Value,
) (opfor.Value, error)
```

`Respond(ctx, response)` invokes the source callback with exactly two values:
the original `Command` followed by `response`. The zero value is safe,
represents an explicit `$null` callback, and returns `ErrInvalidCallable` from
`Respond`. Valid callbacks may be retained and used concurrently, and reject
responses after generation retirement, script unload, or runtime close.

```go
rpc := opfor.AggressorTeamServerRPCProviderFunc(func(
	ctx context.Context,
	request opfor.AggressorTeamServerRPCRequest,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// enqueueRPC must copy any application-owned state it needs. The request
	// values and callback may be retained, but ctx must not be retained.
	return enqueueRPC(request)
})

runtime, err := opfor.New(
	opfor.WithAggressorTeamServerRPCProvider(rpc),
)

// Later, on a worker or response-loop context:
if pending.Callback.Valid() {
	_, err = pending.Callback.Respond(responseContext, response)
}
```

The provider is called once synchronously to accept or reject the request.
Success returns `$null` to the script. With no provider, Host receives the raw
`call` invocation once. A configured provider error is authoritative.

## Beacon transcript output

`WithAggressorBeaconTranscriptSink` replaces the stock stdout presentation for
eight functions:

```go
type AggressorBeaconTranscriptSink interface {
	PublishAggressorBeaconTranscript(
		context.Context,
		opfor.AggressorBeaconTranscriptRecord,
	) error
}
```

`AggressorBeaconTranscriptSinkFunc` adapts a function with the same signature.
The sink is called synchronously exactly once for every successful wrapper
invocation. Every function returns `$null`; a sink error rejects the call.

| Function | Populated record fields |
| --- | --- |
| `berror` | `Kind`, `BeaconID`, `Text` |
| `blog` | `Kind`, `BeaconID`, `Text` |
| `blog2` | `Kind`, `BeaconID`, `Text` |
| `binput` | `Kind`, `BeaconID`, `Text` |
| `btask` | `Kind`, `BeaconID`, `Text`; optional third argument becomes exact Sleep string coercion in `RawMITREIDs`, with `HasMITREIDs` preserving omission. OPFOR does not parse or expand the IDs. |
| `btaskcompleted` | `Kind`, `BeaconID`, `TaskID` |
| `bjoblog` | `Kind`, `BeaconID`, `JobID`, `Text` |
| `bjoberror` | `Kind`, `BeaconID`, `JobID`, `Text` |

Every record also contains `RuntimeID`, `Script`, and immutable call-site
`Span`. A Beacon ID array remains one value and is not fanned out. Source
references are resolved before publication, but compound values retain their
identity. A sink may retain a record subject to that value ownership, but must
not retain `ctx`.

```go
sink := opfor.AggressorBeaconTranscriptSinkFunc(func(
	ctx context.Context,
	record opfor.AggressorBeaconTranscriptRecord,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return transcriptStore.Append(TranscriptEntry{
		RuntimeID: record.RuntimeID,
		ScriptID:  record.Script,
		Kind:      record.Kind,
		BeaconID:  record.BeaconID,
		Text:      record.Text,
	})
})

runtime, err := opfor.New(
	opfor.WithAggressorBeaconTranscriptSink(sink),
)
```

This option is not a serializer, persistence engine, or Beacon tasking API. It
only replaces presentation. When no sink is configured, OPFOR writes its
atomic escaped record format to the runtime's stdout. It never routes these
eight calls to Host.

## Retention, context, and concurrency

All four importer methods are synchronous, but independent script executions
may call the same implementation concurrently. Providers and sinks must
synchronize their own mutable state.

The supplied `ctx` belongs only to the current provider call. Observe its
cancellation and deadline, but never store it for later work. Retained callback
capabilities accept a new context for each invocation. Work admitted before a
provider returns still must not assume that private evaluator state is exposed.

Retaining one of these objects has meaningful ownership consequences:

- a `Callable` or Team Server RPC callback retains script-generation authority;
- `AggressorBindings` retains opaque access to the originating runtime's active
  registration routes and rejects use after revocation;
- compound `Value` graphs retain their original object/function identities;
- `RuntimeID` plus `Script` is the stable correlation key when one adapter
  serves several runtimes.

Do not use `context.Background()` merely to evade cancellation on synchronous
work. Use a new application-owned context only when a request was successfully
accepted for intentionally asynchronous processing.

## ScriptLoader inheritance

Portable ScriptLoader children automatically inherit the configured Beacon
action provider, execution provider, Team Server RPC provider, and explicit
transcript sink through OPFOR's runtime extension profile. Nested children
inherit the same profile as their parent; adapters do not need to reinstall
these options manually.

Each child is still a fresh Runtime with a distinct `RuntimeID`. Local
`ScriptID` values may repeat across parent and child runtimes, so transcript
and provider records must use `(RuntimeID, ScriptID)` together. Callbacks and
`AggressorBindings` in a child request are bound to the child generation, not
to the parent runtime.

An explicitly configured transcript sink is inherited. Without an explicit
sink, each child retains its own stock console/stdout behavior rather than
copying a runtime-bound presentation callback from its parent.

## Coverage and drift checks

Treat [`DefaultAggressorFunctionContracts`](../../api.go) as the machine-readable
typed-wrapper inventory. A production adapter should fail its own startup or
tests when a new action is added without a handler:

```go
func TestBeaconActionAdapterCoversRuntimeInventory(t *testing.T) {
	contracts := beaconActionContracts()
	if got, want := len(contracts), 121; got != want {
		t.Fatalf("Beacon action contract count = %d, want %d", got, want)
	}
	for _, contract := range contracts {
		if !contract.HostFallback {
			t.Errorf("%s unexpectedly lost Host fallback", contract.Name)
		}
		kind := opfor.AggressorBeaconActionKind(contract.Name)
		if adapter.handlers[kind] == nil {
			t.Errorf("missing handler for %s", contract.Name)
		}
	}
}
```

For each boundary, tests should cover:

1. exact request field and value-identity mapping;
2. absent-provider Host routing with the raw invocation exactly once;
3. provider error authority with no Host retry;
4. exact-name override precedence in both option orders;
5. callback omitted/null/callable states and post-unload/close rejection;
6. context cancellation and concurrent calls;
7. ScriptLoader child inheritance and child `RuntimeID` provenance;
8. transcript no-Host behavior and atomic concurrent output.

The repository's focused regression suite can be run with:

```sh
go test ./internal/opfor -run 'TestAggressorBeacon(Action|Execution|Transcript)|TestAggressorTeamServerRPC|TestPortableScriptLoader(InheritsAggressorBeacon|InheritsAggressorTeamServerRPC|Transcript)'
go test -race ./internal/opfor -run 'TestAggressorBeaconActionProviderSupportsConcurrentCalls|TestAggressorTeamServerRPCCallbackConcurrentResponses|TestAggressorBeaconTranscriptStdoutConcurrentCalls'
```

Run `go test ./...` after integration changes. The runtime source of truth for
this guide is the provider request/interface files, the corresponding
`builtins_aggressor_*` specification maps, and
[`aggressor_function_contract.go`](../../internal/opfor/aggressor_function_contract.go).
