# OPFOR

OPFOR is a pure-Go, embeddable implementation of the Sleep language and the
Cobalt Strike Aggressor Script (`.cna`) runtime. The same runtime is exposed as
a Go library and through a Cobra-based command-line interpreter named `opfor`.

The current source target is **`v0.1.0-alpha.1`**. Its acceptance suite compiles
and loads all 18 manifest-pinned `.cna` files from commit
`36d7514dbec82d53d23f25fe7f9e18f4af613be8` of the official
`Cobalt-Strike/aggressor_script_examples` repository and exercises at least one
representative behavior from each through deterministic inert importer
adapters. The acceptance inventory and upstream provenance are pinned in
[`testdata/corpus.json`](../testdata/corpus.json).

The project remains under active compatibility-first development. It does not
yet claim that arbitrary Aggressor scripts execute completely. The support
matrix tracks four separate promises so parse coverage is never confused with
working host behavior:

- Sleep syntax compatibility
- portable Sleep execution compatibility
- Aggressor API compatibility through Go callbacks
- Java/Cobalt object compatibility through importer adapters

OPFOR is not a JVM or a Java standard-library reimplementation. Sleep's
bracket-object syntax is an integration boundary: `ObjectHost` receives first
refusal, and `LoadableProvider` maps `use()` identities to importer code. The
built-in pure-Go Java-shaped helpers are a bounded convenience shim for
behavior directly exercised by Sleep and the approved Aggressor corpus. The
shim is centered on strings, collections, files, random values, and UUIDs;
anything beyond those areas needs direct Sleep/Aggressor evidence and is not a
roadmap item. Gaps in unrelated JDK APIs are not OPFOR compatibility blockers.

During the `v0.x` alpha series, exported Go APIs may change when compatibility
evidence requires a better contract. Such changes must be called out in the
release notes and should preserve old behavior when a safe compatibility shim
is practical. A stable `v1` API is not promised by this alpha.

Building the OPFOR library or `opfor` CLI requires Go 1.24 or newer.

Java serialization is not required to embed OPFOR, use the CLI, or execute
ordinary `.cna` callbacks. It is a compatibility feature only for scripts that
explicitly call `readObject`/`writeObject`, use binary pattern `o`, or persist a
suspended closure. OPFOR's interpreter state is otherwise native Go state, and
additional serialization-shape coverage is not a language/runtime release gate.

## Embedding

Console output defaults to standard output, warnings and diagnostics default to
standard error, and console input defaults to standard input. Importers can
replace all three streams and register native or fallback host functions:

OPFOR owns parsing, evaluation, Sleep/Aggressor call semantics, callback
lifecycle, provenance, and portable defaults. The importing application owns
Cobalt-specific state and effects: Team Server transport, Beacon tasking,
payload generation, client UI, data stores, and operator/session state. Known
families use typed provider interfaces; `WithFunction` supplies exact
per-function overrides, and `WithHost` is the generic compatibility fallback.
All three boundaries exchange in-process `Value`s and guarded callbacks; they
do not require serialization.

Importer adapters can validate the shared Host, object, `use()`, lifecycle,
callback-revocation, and error contracts with the public, Team-Server-free
[`conformance` test kit](../conformance). The kit creates inert reference
endpoints and reports stable versioned cases; it does not claim that an
adapter's Cobalt-owned effects are implemented.

```go
runtime, err := opfor.New(
	opfor.WithStdin(input),
	opfor.WithStdout(output),
	opfor.WithStderr(diagnostics),
	opfor.WithDebugFlags(1),
	opfor.WithLimits(opfor.Limits{
		MaxInstructionsPerExecution:    1_000_000,
		MaxCollectionEntriesPerRuntime: 250_000,
		MaxOutputBytesPerRuntime:       16 << 20,
		MaxInputBytesPerRuntime:        16 << 20,
		MaxDecompressedBytesPerRuntime: 64 << 20,
		MaxSourceBytesPerRuntime:       16 << 20,
	}),
	opfor.WithInitialGlobals(map[string]opfor.Value{
		"client":  opfor.ObjectValue(myClient),
		"@roles":  opfor.ArrayValue(roles),
		"%config": opfor.HashValue(config),
	}),
	opfor.WithVariableProvider(myVariableProvider),
	opfor.WithScriptLifecycleObserver(myLifecycleObserver),
	opfor.WithSourceResolver(mySourceResolver),
	opfor.WithLoadableProvider(myLoadableProvider),
	opfor.WithBeaconStringEncoder(myBeaconStringEncoder),
	opfor.WithAggressorEventDispatcher(myEventDispatcher),
	opfor.WithAggressorBeaconTranscriptSink(myBeaconTranscriptSink),
	opfor.WithAggressorSessionQueryProvider(mySessionProvider),
	opfor.WithAggressorDataModelQueryProvider(myDataModelProvider),
	opfor.WithAggressorDataStoreProvider(myDataStoreProvider),
	opfor.WithAggressorPEProvider(myPEProvider),
	opfor.WithAggressorPreferenceProvider(myPreferenceProvider),
	opfor.WithAggressorCodeTransformProvider(myCodeTransformProvider),
	opfor.WithAggressorProcessInjectionProvider(myProcessInjectionProvider),
	opfor.WithAggressorProfileProvider(myProfileProvider),
	opfor.WithAggressorVPNProvider(myVPNProvider),
	opfor.WithAggressorClientServiceProvider(myClientServiceProvider),
	opfor.WithAggressorClientUIProvider(myClientUIProvider),
	opfor.WithAggressorArtifactProvider(myArtifactProvider),
	opfor.WithAggressorPayloadProvider(myPayloadProvider),
	opfor.WithAggressorListenerProvider(myListenerProvider),
	opfor.WithAggressorPayloadStoreProvider(myPayloadStoreProvider),
	opfor.WithAggressorSiteProvider(mySiteProvider),
	opfor.WithAggressorTeamServerRPCProvider(myTeamServerRPCProvider),
	opfor.WithAggressorBeaconActionProvider(myBeaconActionProvider),
	opfor.WithAggressorBeaconExecutionProvider(myBeaconExecutionProvider),
	opfor.WithAggressorBOFExtractor(myBOFExtractor),
	opfor.WithAggressorDialogProvider(myDialogProvider),
	opfor.WithAggressorPromptProvider(myPromptProvider),
	opfor.WithAggressorBreakpointProvider(myBreakpointProvider),
	opfor.WithAggressorCommandCatalog(opfor.AggressorCommandBeacon, opfor.AggressorCommandCatalog{
		Groups: []opfor.AggressorCommandGroup{{ID: "core", Name: "Core"}},
		Commands: []opfor.AggressorCommandMetadata{{
			Name: "status", Description: "show status", Detail: "Synopsis: status", GroupID: "core",
		}},
	}),
	opfor.WithAggressorBeaconTechniqueCatalog(opfor.AggressorBeaconTechniqueRemoteExploit, opfor.AggressorBeaconTechniqueCatalog{
		Techniques: []opfor.AggressorBeaconTechniqueMetadata{{
			Name: "example", Description: "importer-advertised example", Architecture: "x64",
		}},
	}),
	opfor.WithFunction("hello", func(ctx context.Context, call opfor.Invocation) (opfor.Value, error) {
		return opfor.String("hello " + call.Arg(0).String()), nil
	}),
	opfor.WithHost(myAggressorHost),
)
```

`WithSleepClasspath` configures OPFOR's built-in filesystem resolver. It is an
alternative to `WithSourceResolver`, not an additional option. An importer
that needs both custom resolution and the built-in search rules can construct
a `FileSourceResolver`, call `SetSleepClasspath`, wrap or delegate to it from
its resolver, and pass only `WithSourceResolver`.

Zero-valued limits are unlimited. The instruction counter resets for each
top-level execution or callback; source, collection, input, output, and
decompression counters are monotonic and shared across the root runtime, forks, and
source-backed `ScriptLoader` children. `WithInstructionLimit` remains
compatibility shorthand for changing only the instruction field.

Importers install only the providers they support; implementing every interface
is not required. For example, `bshell` is validated and delivered as an
`AggressorBeaconAction`, but OPFOR never runs that command locally or tasks a
Beacon itself. An unconfigured typed family falls back to `WithHost`; if no
Host is configured, OPFOR returns an explicit unsupported-operation error.
This makes a small offline host, a test double, and a complete Team Server
adapter different implementations of the same interpreter boundary.

The stock function namespace is evidence-gated. Convenience spellings that are
not established by pinned Sleep source/JAR behavior, the official Aggressor
reference, or the approved example corpus are not installed by default, even
when OPFOR retains a small internal implementation for testing. Such calls flow
to an importer-installed `WithFunction` or `WithHost` callback.

Host callbacks receive an opaque `aggressor.Runtime` capability for sending
stimuli back into loaded scripts. `DispatchEvent` delivers exact and wildcard
event handlers, `InvokeHook` selects the newest `set` hook, and
`DispatchPopupHook` composes every matching popup layer in load order. The
older `InvokePopupHook` method remains available for callers that intentionally
want only the newest popup registration. The capability is bound to the
emitting Script generation: retaining it preserves provenance, but its
operations return `ErrScriptUnloaded` after logical generation retirement
rather than targeting a later run. `Valid` reports provenance, not liveness,
and `Same` compares the underlying runtime rather than generation identity.

The nine typed Cobalt-effect request families additionally carry
`opfor.AggressorBindings`. This opaque capability exposes the same
event/hook/popup operations without a
raw `*Runtime`, evaluator, registry, or scope. It is bound to the exact runtime
and execution generation which produced the request, so one importer provider
shared by a parent and a portable `ScriptLoader` child cannot accidentally
dispatch into the parent's registries or a later child run. Retained
capabilities honor caller cancellation and become non-executable when their
generation retires or the runtime closes; their zero value returns
`ErrAggressorBindingsUnavailable`. `Valid`, `RuntimeID`, and `Same` report
structural presence and runtime provenance without exposing pointers; they do
not promise that a retained generation is still executable. `Valid` remains
true for a retained nonzero capability, and `Same` compares runtime identity,
not generation identity or liveness.

The zero-argument `brk()` function is interpreter-owned because only the active
fiber can distinguish locals, closure captures, globals, and caller frames. It
returns the documented nine-key debug hash and either synchronously presents a
detached typed snapshot through `WithAggressorBreakpointProvider` or prints the
returned hash to the script console by default. A provider may block until an
operator continues execution and should observe context cancellation. `brk()`
never falls through to `WithHost`; `WithFunction("brk", ...)` still has highest
precedence. Focused regressions pin its timestamp, ordering, detachment, and
headless policies.

`opfor.String` constructs textual values and maps valid UTF-8 to Java UTF-16
code units. Binary-producing APIs return byte-provenance strings; importers
should use `opfor.BinaryString` for binary input, including octets that happen
to be valid UTF-8. `Value.Bytes` returns the reversible Go host spelling, and
`Value.IsBinaryString` reports retained byte provenance. Comparisons and hash
keys follow Java string identity and therefore observe UTF-16 units rather than
that provenance. `Hash.KeyValues`, `Hash.GetValue`, and `Hash.SetValue` preserve
exact key code units and host byte provenance; the older string-only `Keys`,
`Get`, and `Set` conveniences cannot represent every exotic key spelling.

The default runtime also implements ten documented client-independent
Aggressor utilities `format_size`, `gzip`, `gunzip`, `iprange`,
`powershell_command`, `str_chunk`, `str_encode`, `str_decode`, `str_xor`, and
the portable `hex`, `powershell-base64`, and `veil` selectors of `transform`.
GZIP and XOR consume the low eight
bits of each UTF-16 code unit and return binary-provenance values; `str_encode`
returns a binary value and `str_decode` returns text through the same finite
charset registry as portable text I/O. `str_chunk` counts UTF-16 units and
therefore may split a surrogate pair, matching the language's string indexing
model. `iprange` expands the documented IPv4 forms in deterministic order with
exclusive range endpoints and a 65,536-address safety limit. Fortra documents
`format_size(1024)` as `1kb`; OPFOR defines otherwise unspecified rounding as a
binary scale with at most two decimal places. These portable edge policies are
independently specified and are not claims about an unobserved licensed runtime.
The documented `script_resource` helper is also portable: it returns a clean
full path relative to the loaded source file, falling back to the runtime
working directory for stdin and synthetic eval sources. `WithFunction` may
override it for embedded or virtual source stores.

`transform(value, "powershell-base64")` encodes the input's Java UTF-16 units
as UTF-16LE and then Base64; the documented `2 + 2` expression produces
`MgAgACsAIAAyAA==`. `powershell_command(expression, remote)` first invokes the
newest active `POWERSHELL_COMMAND` hook. Without one it uses the two command
templates published in Fortra's hook reference. The `hex` and `veil`
transform selectors use lowercase digits and narrow each UTF-16 unit to its
low byte; those casing and edge choices are provisional because the public
reference specifies only the transformation family. The public output grammar
for `array`, `vba`, and `vbs` is incomplete, so OPFOR rejects those selectors
instead of inventing byte-for-byte compatibility.

The related Cobalt-owned `encode(code, encoder, architecture)`,
`powershell_compress(script)`, and `transform_vbs(shellcode, maxRun)` functions
use the typed `WithAggressorCodeTransformProvider` boundary. OPFOR enforces
their exact three-, one-, and two-argument forms, resolves each Value once, and
returns the provider Value unchanged, but does not claim a portable encoder,
PowerShell deflator template, or VBS output grammar. `powershell_compress`
first invokes the newest active `POWERSHELL_COMPRESS` script hook; a selected
hook is authoritative and shares the caller's lifecycle and instruction meter.
Without a hook or configured provider, the original reference-bearing
Invocation reaches `WithHost`. `WithFunction` remains the highest-precedence
override, and explicit providers follow portable `ScriptLoader` children.

The documented `dstamp(milliseconds)` and `tstamp(milliseconds)` helpers are
portable as well. Both display the supplied Unix instant in the location of
the runtime's configured clock; `dstamp` uses `yyyy-MM-dd HH:mm:ss`, while
`tstamp` uses `yyyy-MM-dd HH:mm`. Fortra's public reference specifies only that
the first includes seconds and the second does not, so these concrete numeric
layouts are an explicit, locale-independent OPFOR policy pending a licensed
runtime differential. `WithClock` makes the location deterministic, and
`WithFunction` may override either helper.

The documented `on(name, callback)` and `alias(name, callback)` function forms
also use portable defaults. The SSH guide permits `ssh_alias` as a function and
documents its callback ABI but does not separately list its arguments; OPFOR
infers the symmetric `ssh_alias(name, callback)` mapping. These functions share
the ordered, script-owned registries of their corresponding keyword
declarations, so callbacks are revoked when their portable ScriptLoader
generation retires or on terminal Script unload, and
duplicate-name behavior is consistent across both forms. `fireAlias` invokes the newest
matching Beacon alias through the same console ABI as `InvokeConsole`, with the
complete reconstructed command in `$0`, the supplied session ID in `$1`, and
quoted arguments tokenized into `$2` onward. These registration functions and
`fireAlias` return `$null`; importers may override any of them with
`WithFunction`.

`when(name, callback)` and `when name { ... }` are the portable one-shot event
forms. They share the `on` registry and callback ABI, but every matching
`when` is atomically removed before the first callback selected for that event
runs. Matching registrations run in FIFO order, a registration created by a
callback waits for a later dispatch, and a failing callback does not restore a
consumed registration. Exact handlers receive the concrete event name in `$0`
and the dispatched Values in `$1...`/`@_`. OPFOR additionally supports
`when("*")` as a wildcard extension: it runs after exact handlers and receives
the concrete name in both `$0` and `$1`, followed by the payload. External
applications emit events with `Runtime.DispatchEvent`; `fireEvent` and
`fire_event` are runtime-local script conveniences. Registration returns
`$null`, callbacks remain script-owned, and `WithFunction("when", ...)` can
replace the default.

The complete Beacon and SSH command-help families are portable:
`*_command_register`, `*_command_group`, `*_command_describe`,
`*_command_detail`, and `*_commands`. The two catalogs are independent and
contain metadata only; aliases remain separate executable registrations.
Importers can seed immutable defaults with `WithAggressorCommandCatalog` and
read a detached effective view with `SnapshotAggressorCommandCatalog` without
replacing the five script functions. Script registrations layer over that base,
the last duplicate supplies metadata while names retain first-insertion order,
and unload or failed-load rollback reveals an earlier layer. Portable
`ScriptLoader` children inherit only the importer base, never a parent's script
overlays. The reference does not define duplicate/order behavior, unknown
describe/detail results, strict arity rejection, or side-effect return values;
these are explicit OPFOR policies, with missing lookups and registration
functions returning `$null`. An optional group is accepted only when that group
exists at registration, and effective snapshots hide groups until an active
command references them. Cobalt Strike's own UI sorting is separate from
OPFOR's deterministic registry enumeration.

The thirteen documented Beacon technique-registry functions are portable too:
the elevator, local-exploit, remote-exec-method, and remote-exploit families
each provide registration, description, and enumeration, with remote exploits
also exposing architecture. `WithAggressorBeaconTechniqueCatalog` seeds a
validated, defensively copied metadata-only base;
`SnapshotAggressorBeaconTechniqueCatalog` returns a detached effective view.
Script registrations add owner-bound guarded callbacks in four independent,
case-sensitive layered namespaces. The newest layer wins without changing
first-insertion order, repeated registration by one script coalesces, and
unload admission or failed-load rollback restores the preceding layer.
Portable `ScriptLoader` children inherit importer base metadata only.

An importer can call `InvokeAggressorBeaconTechnique` to enter the effective
script callback with the documented ABI: elevator `(Beacon ID, raw command)`,
local exploit `(Beacon ID, listener)`, remote-exec method
`(Beacon ID, target, raw command)`, or remote exploit
`(Beacon ID, target, listener)`. Raw command values are passed through
unchanged. The entry is lifecycle-guarded and instruction-metered; base-only
and missing entries return a typed `UnsupportedError`. These registry APIs do
not call `Host` or perform Beacon tasking.

The corresponding `belevate_command`, `belevate`, `bremote_exec`, and `bjump`
functions are implemented as native dispatch boundaries. An effective local
script callback receives each scalar or top-level array Beacon ID sequentially
with the ABI above; callback results are discarded and the action returns
`$null`. The effective callback is selected once before OPFOR snapshots the ID
array and begins fan-out, so replacement during a call cannot retarget later
IDs. The ID array is snapshotted once and is not recursively flattened. A
callback error or lifecycle revocation stops fan-out without falling back to
the host. If no script callback exists, including a metadata-only importer base
entry, OPFOR forwards the original invocation exactly once to `WithHost` and
discards its synchronous result. These wrappers never perform local tasking:
payload selection and actual privilege escalation or lateral movement remain
explicit embedding/Cobalt boundaries. A null query name
returns `$null`; null/empty registration names, null callbacks, and null remote
architecture are rejected, while a null description becomes empty. These null
rules, strict arity, exact case, raw enumeration order, duplicate layering, and
registration `$null` results
are documented provisional OPFOR policies because the public reference does
not specify those edges.

Twelve documented Beacon/session metadata queries have a dedicated read-only
importer boundary: `beacons()`, `beacon_ids()`, `bdata(bid)` and its
`beacon_data` alias, `binfo(bid, key)` and its `beacon_info` alias,
`barch(bid)`, and the one-ID predicates `-is64`, `-isactive`, `-isadmin`,
`-isbeacon`, and `-isssh`. The official return shapes are arrays of metadata
dictionaries or IDs, a metadata dictionary, a metadata string, an
`x86`/`x64` Beacon-process architecture string, and predicate truth
respectively. `-is64` reports system bitness independently, so an x86 Beacon
on x64 Windows is a valid `barch == "x86"` / `-is64 == true` combination. OPFOR
enforces those exact arities and calls `WithAggressorSessionQueryProvider`
once with a canonical kind, the exact alias spelling, a nonzero process-local
`RuntimeID`, script/span provenance, and the ID/key Values resolved once. An
array-valued ID remains one Value and is not fanned out. `RuntimeID` itself is
non-retaining, but a provider which retains the complete query also retains any
function, object, or nested compound capabilities supplied in its arbitrary
`Value` fields; snapshot scalar coercions when that longer lifetime is unwanted.

Provider results pass directly to the script: the five predicates alone are
normalized to canonical Sleep booleans, and a null or empty `barch` result uses
the documented `x86` fallback. Returned arrays and hashes therefore become
script-owned mutable references; providers must create fresh detached graphs
when their backing state must remain isolated. A configured provider is
authoritative on both success and error; errors reject with `$null` and never
reach `Host`. The provider is inherited by portable `ScriptLoader` children.
Without one, the original `Invocation`, including
reference arguments, is forwarded to `WithHost` exactly once with its result
and error unchanged. `WithFunction` still has highest precedence. The catalog
keeps all twelve functions host-required because OPFOR supplies routing and
provenance, not Cobalt session data.

The related `data_keys()`, `data_query(key)`, and `pivots()` functions use their own typed
`WithAggressorDataModelQueryProvider` boundary because Cobalt's general data
model is heterogeneous and is not limited to Beacon metadata. OPFOR enforces
their documented zero-, one-, and zero-argument forms, resolves the query key once, and
passes the provider result through without sorting, deduplication, coercion,
shape validation, or copying. Provider order and compound identity therefore
remain visible to the script; return fresh detached arrays or hashes when
script mutation must not affect backing state. Provider success or error is
authoritative. Without a provider, the original reference-bearing invocation
reaches `WithHost` once, and `WithFunction` retains highest precedence. The
provider follows portable `ScriptLoader` children. Missing-key behavior, key
coercion and case rules, ordering, duplicates, freshness, and model-specific
schemas remain importer-owned because the public reference does not define
them.

The documented client data-store family has a separate typed
`WithAggressorDataStoreProvider` boundary. It covers credential, application,
archive, download, keystroke, screenshot, service, target, and host queries;
`credential_add`, `host_update`, `host_delete`, and `resetData` mutations;
`tokenToEmail`; `highlight(model, rows, accent)`; and
`redactobject(id)`. OPFOR validates each documented positional shape, resolves
every argument once, and transfers query and other mutation provider results
directly to the script. `redactobject` is effect-only on the typed route: its
successful provider result is discarded and the script receives `$null`. OPFOR
does not invent record schemas, ordering, freshness, identity lookup,
persistence, highlighting, or redaction behavior. Provider errors are
authoritative; without the provider, the original reference-bearing call
reaches `WithHost` once and retains that Host's result policy. No request or
result is serialized.

Eight additional Cobalt-owned PE operations use
`WithAggressorPEProvider`: `pe_insert_rich_header(content, richHeader)`,
`pe_mask_section(content, sectionName, key)`,
`pe_patch_code(content, findBytes, replacementBytes)`,
`pe_remove_rich_header(content)`,
`pe_set_compile_time_with_string(content, string)`,
`pe_set_export_name(content[, exportName])`,
`pe_set_value_at(content, fieldName, value)`, and `pedump(content)`. The first
seven return updated DLL content and `pedump` returns a map according to the
current reference; OPFOR preserves the provider's exact `Value` for every
operation and does not infer any PE parsing or mutation algorithm. Valid calls
fall back to the original `WithHost` invocation when no provider is installed,
and `WithFunction` retains precedence. The provider follows portable
`ScriptLoader` children and no request or result is serialized.

`pe_set_export_name` uses an explicit evidence-union policy: its current
argument table lists only DLL content, while both executable examples supply a
second export-name value. OPFOR accepts one or two arguments and preserves
omission through `AggressorPERequest.HasArgument(1)` so the importer can apply
its licensed-runtime policy. This range is not a claim that Cobalt Strike
accepts both forms.

The four documented preference functions use
`WithAggressorPreferenceProvider`. `pref_get(name, default)` and
`pref_get_list(name)` return the provider's exact `Value`; in particular, a
returned array keeps its identity. `pref_set(name, value)` and
`pref_set_list(name, array)` are side-effect-only and return `$null` after
provider success. OPFOR enforces exact arity and the documented array input for
`pref_set_list`, resolves each Value once, and otherwise leaves preference
coercion, missing keys, persistence, live-list policy, and concurrent ordering
to the importer. Provider errors are authoritative. Without one, the original
reference-bearing call reaches `WithHost` once; no preference operation is
serialized by OPFOR.

The 16 documented process-injection selection and inventory functions use
`WithAggressorProcessInjectionProvider`. The zero-argument `pi_explicit_*`,
`pi_spawn_*`, `pi_user_explicit_*`, and `pi_user_spawn_*` queries return the
provider's exact `Value`; their four setters accept one resolved selection
name, and the two user-selection clears accept no arguments. Successful
setters and clears return `$null`. OPFOR does not invent built-in methods,
user-defined hook inventories, path formats, selection validation, or persistence. Provider
errors are authoritative, an absent provider preserves the raw Host call, and
no process-injection request is serialized.

Team Server configuration and active Malleable C2 profile behavior use
`WithAggressorProfileProvider`. It covers the zero-argument `killdate` query,
`setup_strings(payload)`, and `setup_transformations(payload, architecture)`.
OPFOR resolves each argument Value once and transfers the provider result
unchanged; the importer owns the configured kill date and the actual profile
transformations. The four Covert VPN functions similarly use
`WithAggressorVPNProvider`: `vpn_interfaces()`,
`vpn_interface_info(interface[, key])`, `vpn_tap_create(...)`, and
`vpn_tap_delete(interface)`. Inventory and metadata return the provider Value;
create/delete are side-effect-only. Neither boundary connects to a Team Server
or serializes requests.

Client identity, chat, shared events, and connection lifecycle use
`WithAggressorClientServiceProvider`. The value-producing
`getAggressorClient`, `get_cs_version`, `mynick`, and `users` calls return the
provider's exact `Value`; an opaque client returned by `getAggressorClient`
remains usable through `ObjectHost`. `action`, `elog`, `say`, `privmsg`,
`custom_event`, `custom_event_private`, `closeClient`, and
`sync_download(remote, local[, callback])` are side-effect-only and return
`$null` after provider success. The optional download callback preserves
omitted, explicit `$null`, and retained callable states and is revoked with its
owning script. OPFOR implements call shape and routing, while the importer owns
the connected client and Team Server effect.

The two documented stageless-artifact functions use the typed
`WithAggressorArtifactProvider` boundary. The deprecated
`artifact_stageless(listener, artifactType, architecture, proxyConfiguration,
callback)` form accepts exactly five arguments. Its callback becomes a
retained, multi-shot, script-owned `Callable` which the provider may invoke
after returning; script unload or runtime close revokes it. On this provider
route OPFOR discards the provider's synchronous result and returns `$null`,
because the artifact is delivered through the callback.
`artifact_payload(listener, artifactType, architecture, exitMethod,
systemCallMethod[, httpLibrary[, dnsCommMode[, malleableProfileOverride[,
payloadStoreInfo]]]])` accepts five through nine arguments and returns the
provider's Value directly. `HasHTTPLibrary`, `HasDNSCommMode`,
`HasMalleableProfileOverride`, and `HasPayloadStoreInfo` distinguish each
omitted optional position from an explicitly supplied `$null`.

The broader listener-aware generation surface is split into three importer
boundaries. `WithAggressorPayloadProvider` covers 14 payload, stager, artifact,
signing, PowerShell-bootstrap, and bootstrap-hint functions;
`WithAggressorListenerProvider` covers
10 listener queries and mutations; and `WithAggressorPayloadStoreProvider`
covers the five Payload Store operations. Their wrappers enforce the
documentation-backed arities and optional-position distinctions, preserve
binary and compound Value identity, and distinguish query results from
side-effect-only calls. OPFOR deliberately does not generate or sign a payload,
create a listener, or persist Payload Store state; those behaviors belong to
the importer. The typed `all_payloads` route also enforces the documented
`None`/`Direct`/`Indirect` system-call method, `wininet`/`winhttp` optional HTTP
library, and `dns`/`dns_over_https` optional DNS mode; the last two accept an
actual `$null` or blank string. The raw no-provider Host route stays
importer-authoritative and receives the original Invocation without that enum
validation.

The separate `WithAggressorSiteProvider` boundary covers `localip()` and
`sites()` with zero arguments, `site_kill(port, uri)` with exactly two, and
`site_host(host, port, uri, content, mimeType, description[, ssl])` with the
legacy six-argument or current seven-argument form. `site_host` preserves the
exact optional SSL Value, sets `HasSSL` for any supplied seventh argument
(including `$null`), and records its Sleep truth in `SSLTruth`; omission
produces `$null`, false, and false respectively. Successful `localip`, `sites`,
and `site_host` provider Values pass directly to the script without coercion or
cloning. `site_kill` is effect-only on the typed provider route: OPFOR discards
any successful provider Value and returns `$null`.

For each valid invocation, the configured provider is called synchronously once
after its Values resolve once, may be shared by concurrent calls, and is
authoritative on error: an error returns `$null` and never retries through
`Host`. With no relevant provider,
each valid original reference-bearing `Invocation` reaches `WithHost` exactly
once and its result and error pass through unchanged. `WithFunction` has
higher precedence than these wrappers and their arity checks regardless of
option order. Explicit providers follow portable `ScriptLoader` children.
The compatibility catalog nevertheless keeps all six names host-required:
OPFOR supplies typed routing and lifecycle/provenance guards, not payload
generation or Team Server IP, hosting, removal, or enumeration effects.

The documented `call(command, callback, argument, ...)` function has a separate
typed `WithAggressorTeamServerRPCProvider` boundary. OPFOR requires at least
three arguments, preserves the exact command Value, and supplies the remaining
payload Values in order. A non-null callback becomes a lifecycle-guarded,
multi-shot `AggressorTeamServerRPCCallback`; each
`Respond(ctx, response)` invocation enters Sleep with the original command as
`$1` and the exact response Value as `$2`. The `$null` callback used by the
pinned `oneliner.cna` and `portfwd.cna` examples is accepted and represented by
`Callback.Valid() == false`, so fire-and-forget RPCs do not acquire a script
capability. The provider receives non-retaining `RuntimeID`, `Script`, and
`Span` provenance but no `Invocation` or Runtime pointer.

A provider success returns `$null`; an error is authoritative and is never
retried through `Host`. Without a provider, the original reference-bearing
invocation reaches `WithHost` exactly once, while `WithFunction("call", ...)`
has highest precedence. Explicit providers follow portable `ScriptLoader`
children and may be called concurrently. This API only transfers in-process
OPFOR Values and guarded callbacks: it performs no serialization and implements
no Team Server protocol, connection, authentication, or transport. The public
reference specifies the callback's `(command, response)` ABI and one-or-more
payload arguments, but not the native return value, null-callback behavior,
response multiplicity, invalid-call errors, cancellation ordering, or payload
schema; OPFOR's policies for those edges are explicit compatibility choices.

One hundred twenty-one documented Beacon action functions have a shared typed
write-side boundary. The inventory spans filesystem/process tasking, execution,
injection, token and Kerberos operations, pivots, SOCKS/port forwarding,
listener staging/linking, data-store controls, Beacon configuration, and client
transcript/model effects. It includes the original 29-function tranche, of
which nine names are observed in the approved corpus and twenty are covered by
documentation-grounded OPFOR-authored tests, plus 92 additional
documentation-audited operations.
`WithAggressorBeaconActionProvider` receives one resolved
`AggressorBeaconAction` synchronously for each valid call. `Kind` is the
canonical function, `Name` preserves the exact spelling, `RuntimeID`, `Script`,
and `Span` identify the origin, `Target` is the first argument, and `Arguments`
contains the remaining ordinary Values in source order. The callback slot is
excluded from `Arguments`. OPFOR resolves each Value once, copies the top-level
slice, preserves compound identity and provenance, and never expands an
array-valued `Target`; notably, the official `bread_pipe` example supplies an
array there deliberately.

For the seven filesystem additions, OPFOR treats paths and raw content as
opaque task arguments. It does not read a `bupload` path, normalize local or
remote paths, expand an array target, or issue a task. The importer decides how
to read local content, interpret remote paths, fan out targets, and report
partial failures.

The optional callback slots preserve three distinct states:
`AggressorCallbackOmitted`, `AggressorCallbackNull`, and
`AggressorCallbackCallable`. Only the last carries a retained, multi-shot
`Callable`; invalid non-null callback Values fail before provider dispatch. The
callback argument shapes are action-specific, so the provider owns that ABI
and OPFOR passes callback Values through unchanged. The capability accepts a
fresh caller context on each
invocation and rejects use after its script unloads or runtime closes.

A nil provider preserves compatibility by forwarding the original
reference-bearing `Invocation` to `WithHost` exactly once and returning its
result and error unchanged. A configured provider is called once, may be called
concurrently, must observe cancellation, and is authoritative: success returns
`$null`, while failure returns `$null` plus the error without retrying through
Host. `WithFunction` has higher precedence than every native wrapper regardless
of option order. Explicit providers follow portable `ScriptLoader` children,
whose distinct nonzero `RuntimeID` disambiguates runtime-local Script IDs.

Seven lower-level Cobalt-owned operations use the separate
`WithAggressorBeaconExecutionProvider` boundary: `beacon_execute_job` (four
arguments), `beacon_execute_postex_job` (three through six),
`beacon_inline_execute` and `beacon_inline_execute_pe` (four or five), and
`get_postex_kit_callback_id` (none), plus the value-returning
`beacon_host_imported_script(beaconID)` and
`beacon_host_script(beaconID, content)` helpers. The first four are tasking
effects whose provider result is discarded; the three queries/helpers transfer
the provider's Value directly to the script. The postex and inline callback slots use the same
omitted/null/guarded-callable states described above. For
`beacon_inline_execute`, OPFOR first applies the newest
`BEACON_INLINE_EXECUTE` hook and sends the resulting binary string to the
provider. The boundary is synchronous and authoritative, follows portable
`ScriptLoader` children, and exchanges ordinary in-process Values and guarded
capabilities only—there is no serialization or Team Server transport.

Without this provider, the original reference-bearing Invocation falls back to
`WithHost` once. `beacon_inline_execute` retains its older specialized Host
compatibility path: the hook result replaces argument two, a supplied callback
is guarded, any Host result is discarded, and the script receives `$null`.
`WithFunction` overrides retain precedence over both provider and Host paths.

General client UI operations use `WithAggressorClientUIProvider`: tabs,
visualizations, popup presentation/clearing, top-level menubars, menu
separators, tab navigation, clipboard, URL opening, message dialogs, and 61
currently documented `open*` window commands. Nine additional documented
browser/menu helpers (`bbrowser`, `colorMenu`, `file_browser`,
`insert_color_menu`, `insert_component`, `pgraph`, `process_browser`,
`sbrowser`, and `tbrowser`) bring this typed provider to 84 functions.
`show_popup` and `menubar` expose a restricted, script-lifetime
`AggressorPopupComposer` for the exact popup bindings selected at call time;
`popup_clear` or owner unload makes it stale instead of retargeting a newer
registration. The portable `insert_menu` function composes every exact layer
of another popup hook beneath the active popup/menu invocation.
The retained, opt-in `item(description, callback)` and
`menu(description, callback)` compatibility helpers publish into the same
script-owned descendant tree as their stock declaration forms, while
`bind(shortcut, callback)` and `unbind(shortcut)` share the ordinary layered
key-binding registry. The provider supplies actual UI widgets,
keyboard integration, and event-loop behavior. OPFOR exposes safe popup/menu
composition and separator placement, but does not claim a transactionally
immutable complete menu tree across a concurrent clear or unload.
`popup_clear(hook)` is effect-only on the typed provider route: OPFOR discards
the provider's synchronous Value, returns `$null`, and clears the matching
portable popup generation only after provider success. Provider errors are
authoritative and leave the local generation intact. Without a client-UI
provider, the original reference-bearing Host invocation and its result/error
remain unchanged, and a successful Host call precedes the local clear.
`openPayloadHelper` additionally supplies a retained, script-owned callback to
the provider; it may be invoked repeatedly until the creating script unloads or
the runtime closes. Its documented selected-listener output is delivered only
through that callback, so the typed provider's synchronous result is discarded
and a successful call returns `$null`; provider errors remain authoritative.
The current reference explicitly marks the legacy
`openBypassUACDialog`, `openWindowsDropperDialog`, `bbypassuac`, `bpsexec_psh`,
`brunasadmin`, `bstage`, `bwdigest`, `bwinrm`, and `bwmi` headings removed.
They remain generic Host compatibility names and OPFOR installs no native
wrapper for them. They are the only current generic-Host catalog entries;
active `pe_set_export_name` instead uses the typed PE provider with an explicit
one-or-two-argument evidence union. Deprecated `powershell` uses
`WithAggressorPayloadProvider`; OPFOR accepts the documented three-argument
table and its official two-argument example while preserving whether the
architecture was omitted.

Aggressor's complete documented custom-dialog and prompt family has two
independent typed UI boundaries. `WithAggressorDialogProvider` covers
`dialog`, description/show, both `dbutton_*` functions, and all fifteen
`drow_*` functions. OPFOR builds an opaque runtime-owned dialog object and
presents one ordered `AggressorDialogPresentation`; its one-shot responder
activates action buttons with the documented `(dialog, button label,
%rowValues)` callback ABI or dismisses without a callback. Help buttons expose
their URL to the provider and never enter the action callback. The deprecated
`drow_listener_smb` spelling remains a canonical listener-stage row, while
deprecated `drow_proxyserver` remains distinct. Row result Values, including
checkbox and client-owned selector representations, pass through unchanged.

`WithAggressorPromptProvider` separately covers confirm, text, directory-open,
file-open, and file-save prompts. OPFOR provisionally calls the confirm callback
with no arguments because the public reference does not specify that arity.
Every other prompt passes exactly one provider-produced Value through as
callback `$1` without coercion; a compatible multi-file/folder provider should
encode the documented comma-separated scalar itself. Both responder families
may outlive the native call but are revoked by script unload or runtime close,
close their `Done` channel at every terminal outcome, sanitize later callback
contexts, and are consumed by the first validated terminal response even if
its callback later fails. A response begun before its provider
returns is drained at that boundary and shares the presenting script's
instruction budget; a retained response invoked later is a detached UI event
with a fresh budget. Explicit providers follow portable `ScriptLoader`
children. A provider error is authoritative for the native call but cannot
roll back a callback or dismissal the provider already completed; providers
should normally return nil after a successful response. Without the relevant
provider, the original reference-bearing invocation reaches `WithHost` once.
`WithFunction` remains highest precedence. The stock CLI is headless and
therefore has no invented GUI default. Because Aggressor exposes no pre-show
dialog discard operation, an abandoned unshown dialog remains a script-owned
resource until that script unloads.

The native Beacon transcript adapters implement `berror(bid, text)`,
`blog(bid, text)`, `blog2(bid, text)`, `binput(bid, text)`,
`btask(bid, text[, rawMITREIDs])`, `btaskcompleted(bid, taskID)`,
`bjoblog(bid, jobID, text)`, and `bjoberror(bid, jobID, text)`. Each call
resolves its arguments once, emits one `AggressorBeaconTranscriptRecord`
synchronously through `WithAggressorBeaconTranscriptSink`, and returns
`$null`; sink errors and cancellation propagate. Record fields retain OPFOR
`Value` identity and binary/taint provenance. The nonzero process-local
`RuntimeID` disambiguates parent and `ScriptLoader` runtimes whose `Script` IDs
may collide without directly exposing or retaining a runtime, while `Script`
and `Span` identify the call site. Retaining a capability-bearing record Value
may independently retain its Script or Runtime graph. `HasMITREIDs` distinguishes an
omitted `btask` third argument from a supplied empty or null value, and
`RawMITREIDs` is preserved without parsing. A Beacon-ID array remains one
value and is never fanned out.
An importer `WithFunction` override still takes precedence over these native
wrappers. Because they are native functions, `WithHost` does not intercept
them; the typed sink is their importer boundary.

Without a sink, OPFOR writes one synchronized headless-display line to that
runtime's stdout. It starts with `opfor.aggressor.beacon_transcript`, then uses
fixed common field order (`kind`, `runtime_id`, `script`, source span, and typed
`beacon_id`), followed by the fields for that kind. Every string is encoded in
`strconv.Quote`-compatible Go syntax, so newlines, terminal control bytes, and
invalid UTF-8 cannot split or corrupt the record; a short write is an error.
Installing a sink replaces, rather than duplicates, this fallback. Explicit
sinks are inherited by portable `ScriptLoader` children; an unset sink remains
unset so each child uses its own redirectable console. This display path is
not Cobalt transcript persistence, operator attribution, task/report storage,
or Beacon tasking. `btask` only records the supplied description, and
`btaskcompleted` records only an explicit call—OPFOR never synthesizes task
completion.

`alias_clear(name)` removes every active Beacon-alias layer for the exact name,
not SSH aliases or command-help metadata, and emits one unregister notification
per binding. This realizes the documented restoration of default Beacon command
behavior by leaving importer console dispatch available. No `ssh_alias_clear`
function is documented or installed.

The default runtime also implements the documented `bof_pack` wire encoder in
pure Go. Formats `b`, `i`, `s`, `z`, and `Z` emit the BOF ABI's independent
big-endian length fields and integers, raw low-byte strings, target-encoded
NUL-terminated strings, and UTF-16LE NUL-terminated strings; the complete
buffer has no outer length prefix. Raw `b` fields preserve embedded zero bytes;
`z` and `Z` stop at the first NUL like the official public C-string packer.
`z` uses deterministic UTF-8 by default and
can be replaced with `WithBeaconStringEncoder` when an importer tracks a
Beacon-specific character set. The result is a binary-provenance Sleep string.

`bof_extract(data[, entryPoint])` is deliberately not mislabeled portable.
Current public material documents the default entry point `sleep_mask` and the
byte result consumed by `BEACON_SLEEP_MASK`, but not the returned relocatable
envelope. `WithAggressorBOFExtractor` therefore receives a detached low-byte
copy of the BOF, the resolved entry point, and runtime/script/span provenance;
it returns copied bytes as a binary-provenance Sleep string. A successful empty
result remains an empty string so the documented `strlen(result) <= 0` check is
observable. Extractor errors are authoritative, and OPFOR never parses, links,
relocates, or executes the object. Without an extractor, the exact original
Invocation reaches `WithHost`, allowing a Cobalt-aware adapter to supply the
format without baking an unverified protocol into OPFOR. This executable
boundary is published by `DefaultAggressorFunctionContracts` and cataloged as
`runtime-enforced`; the accepted one-argument form is an inference from the
documented `sleep_mask` default because the official example demonstrates only
the explicit two-argument form.

Nine documented PE mutation helpers are portable as well. Seven operate on raw
byte offsets without parsing their input:
`pe_mask(content, start, length, key)`,
`pe_mask_string(content, start, key)`,
`pe_set_long(content, location, value)`,
`pe_set_short(content, location, value)`,
`pe_set_string(content, location, value)`,
`pe_set_stringz(content, location, value)`, and
`pe_stomp(content, location)`. The two structure-aware helpers are
`pe_set_compile_time_with_long(content, milliseconds)`, which writes whole
Unix seconds to the PE COFF `TimeDateStamp`, and
`pe_update_checksum(content)`, which recomputes the complete-file PE image
checksum while treating its existing checksum field as zero. Both accept PE32
and PE32+ images, include overlay bytes in the checksum, and reject malformed
or truncated header envelopes. This is targeted field validation, not a claim
that sections, data directories, machine/magic pairings, or the whole image are
loadable. Run `pe_update_checksum` after other transformations; applying it
again to unchanged content is byte-for-byte idempotent.

Every helper returns a fresh byte string and
never changes the caller's input; each unit of a non-empty result retains
binary provenance (an empty string has no unit on which to carry that marker).
Masking is bytewise XOR, string scans include
the first NUL terminator, numeric setters write little-endian PE `DWORD`/`WORD`
values, `pe_set_stringz` adds one terminator, and `pe_stomp` zeroes through the
first terminator. OPFOR rejects negative or extending ranges and missing
terminators instead of panicking or growing the payload. The public reference
does not define those invalid-input rules, timestamp values outside the normal
unsigned COFF range, numeric narrowing, low-byte string conversion, or exact
return provenance, so those edges remain explicit provisional policy pending a
licensed-runtime differential. The seven raw-offset helpers do not validate a
PE image; the two structure-aware helpers parse only the header path to their
target field. None uses serialization. Context
cancellation is checked while converting source/replacement strings and during
long scans or mutations; the shared final `BinaryString` materialization is not
chunk-interruptible.

`getAggressorClientType()` returns `headless` for the stock offline runtime.
An importer exposing a UI or REST client can replace it with `WithFunction`.
`dispatch_event(callback)` runs synchronously by default because OPFOR has no
Swing event thread; `WithAggressorEventDispatcher` can queue its guarded
callback on an application event loop. The queued callback remains usable
after `dispatch_event` returns and is revoked when its owning script unloads.
Portable `ScriptLoader` child runtimes inherit the configured encoder and
dispatcher as well as an explicitly configured Beacon transcript sink.

`beacon_inline_execute` is an importer-required tasking wrapper, not an
in-process BOF loader. Its typed execution provider and compatibility Host path
are described above. OPFOR never maps or executes the supplied object bytes;
with neither a provider nor a supporting Host the call fails explicitly as
unsupported. None of these operations uses Java serialization.

The portable Java bridge implements `new java.lang.String()`,
`String(String)`, `String(byte-string, charset)`, and
`java.lang.String.getBytes(charset)`. Sleep exposes Java byte-array results as
its byte-string scalar, so these calls round-trip through `BinaryString` rather
than an opaque JVM object. They share the portable charset registry; the
no-argument `getBytes` form uses OPFOR's deterministic UTF-8 default, while an
unknown explicit name produces Java's `UnsupportedEncodingException` soft
error. The same bridge now covers UTF-16 char/code-point access and movement;
comparison, including `compareToIgnoreCase` and both `regionMatches`
overloads; String and integer/code-point `indexOf`/`lastIndexOf` families;
`contentEquals` and `contains` for strings and portable mutable-string objects;
`getChars` into portable `char[]`; concat, blank/empty checks, repeat, literal
replacement, strip variants, regex-backed `matches`, `replaceFirst`,
`replaceAll`, and `split`; a Sleep-compatible scalar `toCharArray` result; and
conservative `valueOf`/`copyValueOf` forms including portable `char[]`
subranges. Line-normalizing `indent`, incidental-whitespace `stripIndent`, and
Java escape decoding through `translateEscapes` are portable too. `hashCode`,
`subSequence`, value-preserving `intern`, static `join`, static `format`, and
instance `formatted` are portable as well. Regex
replacement strings follow Java `Matcher` syntax for numeric and named groups
and backslash escaping, including Java-style errors, while `split` implements
positive, zero, and negative limits. No-argument case conversion uses generated
Unicode 17 `Locale.ROOT` mappings, including full and contextual mappings.
Explicit overloads accept bounded immutable `java.util.Locale` values and
implement Turkish, Azeri, and Lithuanian conditional rules; OPFOR deliberately
does not inherit a machine JVM's default locale.

Regex slices and replacements retain exact UTF-16 units and raw-byte
provenance. `getChars` preflights and stages the complete copy before one
locked destination commit, so cancellation or instruction exhaustion cannot
partially mutate the portable `char[]`; chars intentionally carry code-unit,
not raw-byte, identity. Native scans observe cancellation and instruction
limits, and regex matching has a two-second bound. The rune-oriented regex
engine cannot begin between surrogate halves for an arbitrary non-empty
zero-width expression; the common empty regex is explicitly handled at every
UTF-16 boundary. `lines`, `chars`, and `codePoints` return bounded one-shot
portable `Stream`/`IntStream` objects with the documented terminal, iterator,
and base-stream subset. `transform` synchronously accepts a Sleep function or
portable `Function` proxy and preserves exact result identity. Importer-owned
`CharSequence` and arbitrary JVM `Function` values remain an `ObjectHost`
responsibility. Machine-default locale selection, the rest of the stream API,
date/time and uncommon Formatter behavior, and less-common overloads remain
open. The portable Formatter subset includes general,
character, integral, floating, percent, and newline conversions; argument
selection, common flags, width/precision, nulls, UTF-16 truncation, and
Java-style soft errors. Focused portable String tests pin the claimed and
excluded behavior.

Portable `java.lang.StringBuilder` and `java.lang.StringBuffer` objects provide
mutable, identity-preserving UTF-16 sequences. The bounded adapter implements
zero-argument, capacity, `String`, and supported `CharSequence` constructors;
append/insert/delete/replace/reverse operations; length, capacity, mutation,
slice, search, comparison, and conversion methods; Java capacity growth; and
the relevant `Appendable`, `CharSequence`, `Comparable`, `Serializable`, and
`Object` relationships. Mutation methods return the original receiver where
Java does, and reverse is surrogate-aware. `StringBuffer` calls are
synchronized; `StringBuilder` retains Java's unsynchronized script contract
while an internal mutex prevents Go/importer data races. Remaining overload,
stream, serialization, and resource-limit boundaries are explicitly excluded
from this bounded adapter.

Portable Java lists and sets now implement `addAll`, `containsAll`,
`removeAll`, and `retainAll`; indexed list insertion, ordered list equality and
hashing, and membership-based set equality and hashing are also supported.
`subList` returns a backed, nestable view whose mutations propagate through its
ancestors and whose stale accesses fail fast after an unrelated structural
change. List and nested-view iterators expose bidirectional traversal, indices,
`add`, `set`, and `remove`, with OpenJDK mutation state and fail-fast behavior;
their unchecked cursor predicates also preserve the reference
`cursor != size` edge. Portable maps implement `containsValue`, `putAll`,
equality, entry-hash summation, `getOrDefault`, `putIfAbsent`, conditional
`remove`, and both no-callback `replace` forms. Static `Collections` provides
natural-order `binarySearch`, `sort`, `min`, and `max`, plus `reverse`, `swap`,
`fill`, `copy`, `rotate`, `replaceAll`, `frequency`, and `disjoint`; seeded or
runtime-random `shuffle`; static `addAll`; both sublist searches; live
enumeration/list conversion; compact `nCopies`; immutable singleton factories;
and cached ordinary, sorted, and navigable empty factories with exact singleton
aliases, range/navigation views, and natural/reverse comparator behavior.
`binarySearch`, `sort`, `min`, and `max` accept null/natural, bare Sleep-closure,
and portable proxy Comparators; `reverseOrder` preserves singleton and retained
proxy identities and delegates comparator callbacks. Comparator-backed sort
and the stock Sleep sort family use stable TimSort with the observed callback
order for small and large inputs. An inconsistent comparator produces Sleep's
exact general-contract warning, abandons only the active block, resumes its
caller, and leaves the input unmodified. A comparator's isolated Sleep `throw`
instead matches the Java bridge's deferred-flow behavior: TimSort commits the
result reached without further closure calls, stock `sort` then transfers the
throw, and `Collections.sort` consumes it. Genuine importer, cancellation, and
resource failures still leave the input unchanged. Two
compact repeated lists compare in constant time,
including at Java's signed-32-bit maximum size; operations that would
materialize more than 1,048,576 elements fail with a typed
`OutOfMemoryError` instead of risking the importing Go process. Their
cached `keySet`, `values`, and `entrySet` objects are live backed views:
removal, bulk filtering, clearing, and iterator removal update the map, while
direct insertion remains unsupported. Portable `Map.Entry` objects expose key/value access, backed
`setValue`, equality, hashing, string conversion, and their Java type
relationship. An entry node remains stable and live while its mapping exists,
then detaches on removal or clear and does not reattach after same-key
reinsertion. The bounded adapter still has Sleep string-key coercion,
boxed-type erasure, simplified Tree ordering without comparator/navigable APIs,
portable-entry-only interoperability, a stable identity-hash approximation,
opaque importer Comparator and functional-method gaps, foreign Random objects,
and less-common sorted/navigable wrappers and utilities. Script-created
collection growth is covered by the opt-in runtime-family entry budget.

The portable Java bridge also implements `java.util.Random`, including the
unqualified `Random` name that Sleep imports from `java.util.*`. Constructors,
`setSeed`, all zero/bound/origin-bound `nextInt`, `nextLong`, `nextFloat`, and
`nextDouble` forms, `nextBoolean`, mutable-byte-array `nextBytes`, and cached
polar-method `nextGaussian()` use Java's specified 48-bit recurrence and are
safe to share across concurrent script activity. The bridge also exposes the
`RandomGenerator` type relationship, `isDeprecated`, and the identity case of
`Random.from`. `setSeed` clears the cached Gaussian, matching Java. Inherited
`nextGaussian(mean, stddev)` and `nextExponential()` use a pure-Go
modified-ziggurat sampler with tables generated by the pinned BSD-3-Clause
OpenJDK utility; this state path remains separate from cached
`nextGaussian()`. All twelve lazy primitive `ints`/`longs`/`doubles` source
overloads expose a bounded one-shot base/iterator/`count`/`sum`/`toArray`
subset, and `RandomGenerator.of("Random")` returns a fresh classic generator.
Other named/default algorithms, additional stream operations, Java object
serialization, and foreign-generator wrappers remain outside this bounded
adapter. The exact source and license boundaries are recorded in `NOTICE` and
enforced by focused tests.

The bridge also implements immutable `java.util.UUID` values: the `(long,
long)` constructor; `randomUUID`, `nameUUIDFromBytes`, and `fromString`;
bit/version/variant and version-1 field accessors; and Java-compatible string,
hash, equality, and comparison methods. Random UUIDs use Go's cryptographic
random source. OPFOR-authored factory/coercion tests pin this surface;
`ofEpochMillis` and UUID object-stream encoding remain explicit gaps.

Arguments passed to `Runtime.Load`, `Runtime.Execute`, or `Runtime.Eval`
populate Sleep's `@ARGV`. They are launcher arguments, not arguments to the
top-level body, so top-level `@_` is empty and `$1` through `$n` are null.

`WithInitialGlobals` installs scalar, array, hash, function, or object values
before a script's top-level body. Bare names become scalar names; `$`, `@`, and
`%` sigils are retained. Compound values preserve reference identity, so reuse
shares their backing value across scripts; a lifecycle callback can instead
install a fresh container with `Script.Set`. The runtime reserves
`$__SCRIPT__`, `$__SCRIPT_NAME__`, and `@ARGV` and rejects those initial names.
`WithScriptLifecycleObserver` receives `ScriptLoaded` after globals are ready
but before top-level execution, followed exactly once by `ScriptUnloaded` on
rollback, one-shot completion, explicit terminal `Script.Unload`, or runtime
close. A portable `ScriptLoader` child receives one load notification, but its
logical `unloadScript` generation retirement emits no lifecycle notification;
terminal loader/parent/runtime cleanup emits its one unload notification.
Internal Sleep fork instances do not represent new loads and receive neither
fresh globals nor lifecycle notifications.
The first `Runtime.Eval` creates one persistent observed script; later Eval
calls reuse it, and runtime close delivers its unload notification.

Controller integrations can resolve an exact active identity with
`Runtime.ScriptByID`, read or replace its raw Sleep debug mask with
`Script.DebugFlags` and `Script.SetDebugFlags`, and obtain a detached report
with `Script.SnapshotProfile`. Profile snapshots use stable JSON field names,
sort counters deterministically, and remain safe to inspect while independent
script calls are executing. Resolution never falls back to a primary script or
path, and retained controllers receive `ErrScriptUnloaded` after revocation.

`WithVariableProvider` is the lazy, authoritative counterpart to eager initial
globals. Its `VariableContainer` is the Go equivalent of Sleep's
`sleep.interfaces.Variable`: existence, get, put, and remove operations cross
the importer boundary, and local/internal containers are created from the
script's global container. OPFOR preserves the exact `*Cell` supplied by the
provider so references and assignments address importer-owned storage. A fork
uses the parent global container's internal-container factory for a detached
child global, while a portable `ScriptLoader` child runtime inherits the
provider and creates a new global container with its own runtime/script
provenance. Provider errors propagate through execution and the context-aware
`Script.GetContext`, `SetContext`, and `GlobalsContext` APIs. Legacy `Set`
still returns errors but uses a background context; legacy `Get` and `Globals`
cannot represent provider failures. Because Sleep's interface has no
enumeration method, `GlobalsContext` can list only names OPFOR has observed.
Provider-backed closure state is deliberately outside the Java serialization
compatibility adapter; ordinary in-process use performs no serialization.

`WithFunction` installs or overrides one native function. `WithHost` receives
otherwise unresolved function and predicate calls, while `WithObjectHost`
receives Java-style construction, method, property, and type-test operations.
Importer-owned opaque values may implement `opfor.Iterator` to participate in
`foreach` and iterator-consuming builtins. Implementing `opfor.MutableIterator`
also exposes Java-compatible current-element removal through zero-argument
`remove()`.
`WithSourceResolver` controls Sleep `include` loading for embedded assets or
application-owned modules. Without an override, OPFOR reads ordinary files,
directory members, and ZIP/JAR members relative to the runtime working
directory, initialized from the process directory and updated by Sleep
`chdir`. A `FileSourceResolver` also preserves whitespace in file names and
can search Sleep's semicolon/colon-separated path for ordinary one-argument
includes and explicit directory/ZIP/JAR containers with `SetSleepClasspath`;
`WithSleepClasspath` configures that path on OPFOR's runtime-owned default
resolver. It cannot be combined with `WithSourceResolver`, whose configuration
remains importer-owned. Portable `ScriptLoader` children inherit the path for
later include/import lookup, while the default File and String-filename main
load overloads remain direct. Filesystem-backed main files and include
dependencies participate in `ScriptInstance.hasChanged`; virtual resolver
names remain importer-owned and are not treated as local paths.

`getScripts` and `getScriptsByKey` return the same persistent live mutable list
and map objects on every call. They are deliberately independent registries:
mutating one does not silently repair the other. A non-null shared environment
must be a portable `java.util.Hashtable`; that exact mutable object carries
Sleep's `(isloaded)` marker, installs the loader's global function bridge once
per marker generation, and shares script-defined subs and pure-Go
provider-installed native functions across associated children. Its map entries
follow the same stable-live-then-detached invariant as other portable maps.

The same portable loader implements `setCharsetConversion`,
`isCharsetConversions`, `setCharset`, and `getCharset` for file/resolver and
`InputStream` bytes; direct Java `String` source remains unchanged. Disabled
conversion maps each octet to the same-valued UTF-16 unit, explicit names use
OPFOR's finite charset registry, and the unset default is deterministic UTF-8.
It also implements type-exact `getScriptsToLoad` and `getScriptsToUnload`
helpers: each accepts a portable Java `Set` and returns a new ordered
`LinkedHashSet` difference. Focused ScriptLoader tests record
unsupported-charset fallback, ordering, implemented type-exact overloads,
official-JAR differentials, and the implemented `ScriptEnvironment`
block/function-environment/
predicate-environment/filter-environment/predicate/operator introspection
tables. `ScriptEnvironment.setEnvironment` installs the exact portable
`Hashtable` passed to that child, accepts null without replacing its stack,
and leaves sibling children attached to their prior table; a wrong non-table
argument remains Sleep's soft overload mismatch. `getEnvironmentStack` returns
one stable, mutable, sibling-isolated portable `java.util.Stack` identity per
environment with source-compatible end-of-Vector `push`/`peek`/`pop`,
`empty`/`search`, iterator order, and Stack/Vector interface relations.
Remaining boundaries include
direct JVM-interface invocation, arbitrary-JVM bridge typing, mutable
`Scalar`/`ScriptVariables`, JVM context/check frame materialization inside
environment stacks, complete `Hashtable` edges, the global parsed-`Block`
cache, and serialized-`Block` surfaces.

The materialized default table also preserves the source-observable shared
bridge identities for the numeric, string-alias, utility, BasicIO, filesystem,
regex, and environment groups, including the distinct wrapper identities used
when taint mode is enabled. Calls still dispatch by their table key, so sharing
an introspection object does not conflate function behavior.

`WithLoadableProvider` is the typed pure-Go boundary for Sleep `use()` calls.
It resolves a requested class and optional JAR/directory source to a
script-local `LoadableBridge`. A bridge can install native functions with
`Script.RegisterFunction`; OPFOR caches the resolved bridge per script and
source/class pair, calls `ScriptLoaded` for each `use`, revokes its functions,
and delivers paired `ScriptUnloaded` cleanup in reverse order when its owning
portable `ScriptLoader` generation retires or the Script/loader hierarchy is
terminally unloaded. The
bridge must permit concurrent `ScriptLoaded` calls, including repeated uses in
one Script. Returning `UnsupportedError` from class resolution declines that
identity and continues to the built-in fixture/Host fallback. The provider
follows portable `ScriptLoader` children. With no typed provider, the pinned
canonical Sleep fixture remains portable and other classes reach the generic
`WithHost` boundary before becoming `ClassNotFoundException` soft errors.
OPFOR never executes JAR bytecode itself.

Explicit portable `ScriptLoader.unloadScript` removes the registry entries and
marks the instance unloaded without terminally unloading or replacing its
child `Script`. It retires only the current importer-capability generation:
that generation's bindings, Loadable-installed native functions, popup/UI
resources, command and Beacon-technique catalog layers, retained
`Invocation`/`ObjectInvocation` callbacks, and opaque `AggressorBindings`
become unavailable at unload admission. After already-admitted calls drain,
OPFOR invokes binding-observer and Loadable teardown before an ordinary unload
call returns. If unload is called reentrantly by code holding that generation's
own execution lease, registry revocation is still synchronous but teardown
finishes asynchronously to avoid waiting on itself.

The retained `ScriptInstance`, stable child `Script`, globals, environment,
script-defined functions, and raw Sleep closure handles remain runnable, as
the pinned Sleep runtime requires. A later `runScript` reuses that state while
using the fresh importer-capability generation installed by retirement; it
does not set `isLoaded` back to true and cannot revive old importer handles.
Logical generation retirement emits no terminal `ScriptLifecycleObserver`
notification. Terminal
loader, parent, or runtime cleanup still unloads the child exactly once. This
is a lifetime discipline for OPFOR-created importer capabilities, not a
sandbox around trusted raw `*Runtime`, `Invocation.Runtime`, `*Script`,
`Value`, cell, or independently owned fork access.

A successfully published Sleep `fork` is a distinct, initially loaded child Script and owns
its own importer-capability generation. Parent `ScriptLoader.unloadScript`
therefore retires the parent's generation but does not retire the fork's
generation, including bindings, Loadable publications, UI/catalog resources,
or callbacks created inside the fork after that parent unload. Fork worker
completion alone also does not invalidate a callback returned from the fork;
the child generation remains owned until terminal fork/loader/parent/runtime
hierarchy teardown (or importer cancellation or a runtime limit). A fork call
which has not passed the parent's normal generation-admission boundary is
rejected instead of escaping into a successor generation.

OPFOR sanitizes every asynchronous handoff. It preserves importer
cancellation, deadlines, ordinary context values, and the selected instruction
meter, but drops private evaluator/fiber, binding, lifecycle, loadable/native
dispatch, UI ancestry, cleanup, and run-owner tokens. The child then receives
fresh Script execution and generation provenance. Explicitly passed raw
values retain Sleep's shared backing identity; they do not become a sandboxed
copy merely by crossing the fork boundary. Focused ScriptLoader regressions
pin the detailed source and oracle mapping.
Recursive includes are rejected with `ErrIncludeCycle` by default. Importers
that require Sleep 2.1's exact unbounded behavior can select
`WithIncludeCyclePolicy(IncludeCycleAllow)` and should pair it with an
instruction limit or cancelable context.
`WithBindingObserver` reports registrations to an application, and
`Runtime.DispatchEvent`/`Runtime.InvokeBinding` let it deliver external events
and invoke hooks. `Runtime.BindingByID` and `Runtime.InvokeBindingByID` preserve
an exact registration identity when multiple scripts or declarations use the
same name; binding IDs are local to their owning `ScriptID`. `WithEnvironment`
plus `Runtime.Compile` or `Runtime.CompileString` registers importer-defined
ordinary, filter, and predicate environment keywords before parsing. Each
observed `Binding`
retains its ordered raw/evaluated selectors; filter parameters remain raw and
predicate bindings expose a script-owned `PredicateEvaluator`. Nested popup,
menu, and item registrations carry their parent composition invocation, and a
repeated popup/menu composition replaces its previous descendant tree instead
of growing persistent registrations. Missing Aggressor behavior in `.cna`
execution produces an
error chain containing a typed `UnsupportedError`; OPFOR does not pretend that
a Cobalt, UI, network, or payload action succeeded. With the default host,
unresolved calls in ordinary Sleep `.sl` source retain Sleep's warning-and-null
behavior instead.

Host and native callbacks can retain a function argument with
`Invocation.Callback`; object adapters have the matching
`ObjectInvocation.Callback` method. The returned capability remains callable
after the importer call returns, honors each invocation context, and becomes
`ErrScriptUnloaded` when its owning execution generation retires, its Script is
terminally unloaded, or the runtime closes. The `aggressor` adapter exposes
the same guard as `Argument.Callback` and `ScriptCallback` without revealing
the raw runtime.

The underlying `opfor.Callable` contract is synchronous and may be invoked
concurrently. Implementations must observe but not retain the supplied context;
the argument slice is borrowed for the call, while arrays, hashes, functions,
and objects inside copied `Value`s retain shared reference identity. Returned
errors are authoritative. `opfor.CallableFunc` adapts an ordinary Go function
to this interface; invoking a nil adapter returns `$null` with
`ErrInvalidCallable`. Wrapping an arbitrary callable with `FunctionValue` does
not add a script lifecycle guard, so retained script callbacks must still come
from `Invocation.Callback` or `Invocation.RetainCallback`.

The host boundary is not a sandbox. Portable Sleep file and process functions,
plus portable `java.io.File` creation, deletion, directory, rename,
permission, and timestamp methods, perform real local effects by default; this
includes `openf`, file mutation functions, `exec`, and backticks. An
application running untrusted scripts should override the relevant native
functions and source resolver, and use operating-system isolation in addition
to context cancellation and an instruction limit.

Sleep-compatible taint tracking is available per runtime and is disabled by
default. `WithTaintMode(true)` enables recursive source marking, propagation
through expressions, calls, and objects, sanitizer behavior, sensitive-call
rejection, and the `taint`, `untaint`, and `-istainted` script surface.
Importers classify native or Host-resolved functions with `WithTaintPolicy`,
`WithTaintFunction`, `RegisterTaintPolicy`, or `RegisterTaintFunction` using
`TaintSource`, `TaintSanitizer`, `TaintSensitive`,
`TaintSensitiveSource`, or the default `TaintPermeable` policy. Go callbacks
can inspect `Value.IsTainted()` and use `Runtime.Taint`, `TaintAll`, or
`Untaint`. Taint tracking is a data-flow compatibility feature, not a security
sandbox; applications must still constrain effects at the host and operating
system boundaries.

## CLI

Build the Cobra-based interpreter and run a `.cna` or Sleep program directly:

```console
go build -o opfor ./cmd/opfor
./opfor script.cna argument-one argument-two
```

The explicit command forms are:

```text
opfor run <script> [args...]  compile and execute a script
opfor check <script>          compile without executing
opfor eval <code>             evaluate one source string
opfor repl                    use a persistent line-oriented session
opfor serve [script] [args...] keep one runtime and dynamically manage scripts through a local JSON-lines adapter
opfor version                 print build version information
```

Flags stop being parsed at the script path, so script arguments beginning with
`-` pass through without requiring `--`. Use `-` as the script path to read
source from standard input. Script arguments populate Sleep's `@ARGV`; the
top-level `@_` remains empty, matching the reference launcher. The CLI uses the
same stream defaults, portable builtins, and explicit unsupported-host errors
as the Go library.

`serve` accepts an optional startup script. Its JSON-lines protocol can
`load`, list, `reload`, and `unload` multiple scripts, target `call` by script
ID or path, control one exact script's debug flags with `trace`, return detached
profiler snapshots with `profile`, and dispatch runtime-wide events and
bindings.

Global runtime flags must precede the command or direct script path:

```text
--taint                  enable Sleep-compatible taint tracking
--debug <flags>          set the initial Sleep debug bitmask (default 1)
--max-instructions <n>   bound VM instructions per execution (0 is unlimited)
--max-collection-entries <n>  bound created collection entries per runtime family
--max-output-bytes <n>         bound written/buffered output bytes per runtime family
--max-input-bytes <n>          bound bytes delivered by input reads per runtime family
--max-decompressed-bytes <n>   bound gunzip output bytes per runtime family
--max-source-bytes <n>         bound admitted source bytes per runtime family
--classpath <path>       set Sleep's semicolon/colon-separated container path
```

For example, `opfor --taint run script.sl external-input` marks launcher input
as tainted, and `opfor --max-instructions 100000 script.cna` stops runaway VM
execution with a nonzero exit status. The same instruction limit applies when
an importer enters script code through event or popup dispatch, hook invocation,
or an exact binding ID; synchronous nested dispatch reuses the active budget.

`opfor` is an offline interpreter for local source; it is not the proprietary
`agscript` Team Server client and does not implement its connection, login, or
session protocol. Direct execution and `opfor run` are one-shot: registered
events, commands, aliases, and hooks are unloaded when execution completes.
`opfor repl` provides a persistent local evaluation session. `opfor serve`
creates one persistent runtime with an optional startup script and accepts
newline-delimited JSON requests on standard input for script lifecycle,
dispatch, invocation, introspection, and shutdown;
script output and warnings are isolated on standard error while protocol
responses use standard output. The lifecycle methods are `load`,
`scripts`/`ls`, `reload`, and `unload`; `call` can target a loaded script by ID
or path, while `trace` and `profile` require an exact numeric script ID.
`--fire-ready` dispatches the `ready` event only after a startup
script loads. Since the protocol owns standard input, any supplied startup
script must be a filesystem path rather than `-`. JSON arrays and objects map
to Sleep arrays and hashes; byte strings use the lossless tagged form
`{"$opfor":"binary","base64":"..."}`. Embedders can use `Runtime.Load`
directly for a typed, in-process callback host. The request ABI distinguishes
raw console input from positional callback arguments.

`opfor eval '-1 + 2'` accepts code beginning with a hyphen without flag
rewriting; `--` is also accepted before code when disambiguating `-h` or
`--help`. SIGINT and SIGTERM cancel active execution, after which the CLI closes
the runtime and unloads registrations with a bounded cleanup context.

Release archives are built by the internal release-engineering command, not by
a second shipped CLI:

```sh
go run ./internal/cmd/releasepack -version v0.1.0-alpha.1 -out dist
```

It produces normalized `tar.gz` archives for Linux and macOS, ZIP archives for
Windows, and `checksums.txt` across `amd64` and `arm64`; bit-for-bit
reproduction requires the same source bytes and Go toolchain. Each archive
contains `opfor`, README, LICENSE, NOTICE, and the third-party license bundle.
On an alpha tag, the checked-in Actions workflow requires the source version
to equal the tag, waits for Linux/macOS/Windows tests, race, pure-Go, vet,
module verification, all six budgeted fuzz targets, and all six cross-build
gates, then smoke-tests all six native packages before creating a GitHub
prerelease. A local package build is not evidence that those tag-time remote
gates passed.

## Compatibility strategy

OPFOR uses the canonical Sleep 2.1 regression suite as its language oracle and
the Apache-licensed `Cobalt-Strike/aggressor_script_examples` repository as its
only external `.cna` corpus. Corpus files are pinned, hashed, and retain their
upstream licenses. OPFOR-authored synthetic snippets remain inline in Go tests
so they cannot be mistaken for another imported script corpus.
Optional differential tests may use a separately supplied licensed Cobalt
Strike installation, but proprietary artifacts are never required by normal
tests or vendored into this repository.

The current automated baseline verifies all 342 canonical Sleep source files
against their expected parser outcome (330 accepted programs and 12 malformed
fixtures). Its execution/diagnostic inventory includes 302 byte-exact central
goldens, 10 byte-exact mode-specific executions, 14 identity/path-sanitized
reference cases, one hermetic inert-JAR byte fixture, and 15 compile/load
diagnostic comparisons. OPFOR
also compiles and loads all 18 approved official `.cna` sources through inert
recording adapters, and each has a representative mock execution. These tests
perform no Cobalt, process, network, or UI effects. A worktree guard rejects any
worktree `.cna` path that is outside the approved directory or absent from
its SHA-pinned manifest. The public [`aggressor` catalog](../aggressor) exposes
the machine-readable name inventory and callback classifications.

## Current limitations

Catalog presence or successful parsing is not an execution-compatibility
claim. Core parked `callcc` continuations and isolated `fork` instances are
implemented, including duplex `$source` pipes and repeatable `wait`. `exec`
returns a managed duplex `ProcessObject` with child stdin/stdout, repeatable
exit-status `wait`, `closef` destruction, and runtime-owned cleanup; backticks
remain the separate output-array form. `eval`, `expr`, and `include` preserve
Sleep's dynamic continuation groups: fresh yields and callccs return through
the surrounding expression, then resume the saved dynamic contexts in
reference order on later closure invocations. This also works through
same-runtime native and `Runtime.Invoke` reentry without recompilation or AST
replay. The bounded Java-compatible closure codec preserves the observed
dynamic-only, mixed outer/dynamic, inline, expression-return, multiple-root,
and suspended-foreach shapes. Metadata that Sleep itself omits from the wire,
and unsupported complex atom graphs, remain explicit typed failures rather
than a private serialization fallback.
Source- and JAR-pinned core flow also returns `1` from `done` and `2` from
`halt`, treats bare and explicit-null `throw` as no-ops, accepts bare `callcc`
through the normal non-function warning/yield path, and makes zero-argument
`expr()` an active-script block warning while direct importer misuse remains
an explicit error.
Java-regex compatibility is source-audited and JAR-differentially tested. It
includes Java's line and case modes, the complete Unicode 17 OpenJDK block
registry, `\N{name}` with OpenJDK `Character.codePointOf` naming and fallback
semantics, `javaMirrored`, the remaining non-deprecated `java.lang.Character`
predicates, and the six current emoji properties. A minimally modified,
embedded regexp2 v1.12.0 fork keeps Java's ASCII and Unicode case modes
distinct. Dedicated engine nodes implement `\X` and `\b{g}` with OpenJDK's
Unicode-17 UAX #29 grapheme behavior, including Indic conjuncts, emoji ZWJ
sequences, and regional-indicator parity. Categories, scripts,
contributory/binary properties, identifier predicates, character names, Java
case mappings, and grapheme classifications are generated from authenticated
Unicode 17 inputs and do not drift with the Go toolchain's Unicode version.
Configurable runtime-family source, collection-entry, output-byte, input-byte,
and decompression quotas complement context cancellation and the per-execution
VM instruction limit. They deliberately do not claim to bound every ordinary
string, regex, serialization, or importer-owned allocation.
The pure-Go runtime includes `connect` and `listen` as duplex TCP handles with
Sleep's synchronous/callback arity, soft errors, requested-port listener cache,
peer output, `wait`, and `closef` release behavior. Socket workers and listeners
are runtime-owned so unload/close cannot leak them; positive `backlog` values
use Go's platform default because the portable `net` API does not expose the
kernel backlog. It also includes `readc` and `setEncoding`: text
reads and `print`/`println`/`printAll` share an incremental codec while `readb`,
`writeb`, `bread`, and `bwrite` remain raw bytes. Its deterministic default is
UTF-8, rather than the JVM process default, and its finite case-insensitive charset
inventory is UTF-8, US-ASCII, ISO-8859-1, UTF-16, UTF-16BE, UTF-16LE, and
windows-1252, including the standard JDK aliases for those families. It also
includes `bread`/`bwrite` over the same pattern
engine as `pack`/`unpack`, `consume`/`skip` with mark/reset-aware partial-count
semantics, and the pipeline-closure `-eof` predicate. Failed `openf` calls
retain Sleep's non-null inert handle while reporting invalid descriptors,
missing paths, and directory read targets through `checkError`. Callback `read`
returns after launching a worker and invokes `&read` with `$1` as the handle,
`$2` as data, and exactly two positional values in `@_`; binary mode delivers
a final partial chunk before flagging EOF. Script source must supply a Sleep
closure or closure name, not an arbitrary native `Function` scalar. Sleep
throws remain local to the callback worker, while importer, instruction-limit,
and runtime-resource errors are latched and returned by `wait` without binary
callback retry. `wait` follows the handle's replaceable current worker while
retaining a distinct fork-completion token. Script/runtime teardown
cancels and joins workers backed by owned blocking transports. A borrowed
reader may implement `ReadContext(context.Context, []byte)` to provide the same
truthful cancellation without transferring close ownership. If a plain
borrowed `io.Reader` is already blocked, teardown returns
`ErrReadCancellationUnsupported`, leaves the reader and actual worker open,
and suppresses its callback; one result may later be consumed and discarded
when the host lets that read return. Owned transport cancellation, `closef`,
EOF, and lifecycle teardown share one close coordinator, including duplex
handles whose read and write sides use the same closer, so the underlying
`Close` is invoked at most once. The runtime also includes the complete
official `FileSystemBridge` function inventory, including multi-component
`getFileProper`, atomic file creation, metadata mutation, and root enumeration.
`systemProperties` returns a fresh, detached read-only snapshot of portable
host properties: indexed writes are non-persistent and structural removal
warns, while JVM-, vendor-, class-path-, encoding-, and arbitrary
`-D`-specific properties remain absent.
Sleep strings retain exact Java UTF-16 code units plus optional per-unit binary
provenance. `readc` and `chr` therefore preserve lone surrogates, while
concatenation, interpolation, slicing, comparison, hashing, and string builtins
keep the same code units. Binary-producing APIs use byte-sized code units;
embedders use `BinaryString` for binary input and `String` for text.
Aggressor's portable GZIP, encoding, chunking, and repeating-key XOR helpers
use that same UTF-16/byte-provenance model. Their native loops check context
cancellation between bounded blocks. `gunzip` is unlimited only when
`MaxDecompressedBytesPerRuntime` is left at its zero default; embedders
processing untrusted data should set that shared expansion budget and provide
a deadline or cancellation policy.
Scalar, raw boxed value, shared/cyclic array, ordinary or ordered hash, and
bounded executable closure/continuation serialization interoperates with the
official Sleep 2.1 JAR through `readObject`/`writeObject`, their `AsObject`
variants, and binary format `o`. Yielded/local/inline contexts, saved callcc
continuations, suspended foreach contexts, recursive closure transfer through a
fork, and the observed dynamic-source continuation groups are covered.
Unobserved nested/inline-combined foreach resume graphs, saved
exception-handler contexts, and unsupported closure atoms remain typed
serialization failures. Focused interoperability tests pin the official Sleep
2.1 descriptor profile. A few
debug-only goldens also embed JVM object identity strings that a pure-Go value
cannot reproduce. Arbitrary JVM, Swing, and Cobalt objects require an
`ObjectHost`; the default runtime supplies portable Java scalar helpers,
class literals and import validation, mutable `StringBuilder`/`StringBuffer`,
raw `cast`-created native arrays, eager Sleep-container/string conversion for
Java method array returns, bounded `reflect.Array.newInstance` allocation with
Java's 255-total-dimension rule, `StringTokenizer`, common lists, sets, maps, complete
bidirectional list iterators, `Arrays`,
`reflect.Array`, `Collections`,
`MessageDigest`, `System.out`, `java.util.Random`, `java.util.UUID`, path-backed
`java.io.File` objects with the three common constructors, path/metadata
accessors and predicates including `canExecute`, absolute/canonical/parent path
and object accessors, `equals`, `compareTo`, `hashCode`, atomic creation,
deletion, directory creation, platform rename, timestamp and permission
setters, Linux/Darwin/Windows filesystem-space queries, and mutable-snapshot
directory listing with unfiltered, explicit-null, and portable non-null
closure/proxy `FilenameFilter`/`FileFilter` overloads, plus static path
separators, root enumeration, and atomic temporary-file factories with
normalized candidate validation and filesystem-component-length shortening,
and `toURI`, deprecated `toURL`, and
cached-identity `toPath` conversions returning bounded immutable
`java.net.URI`, `java.net.URL`, and default-provider Path objects; safe
source-backed `ScriptLoader` objects, including repeatable instances,
string/File/InputStream/compiled-Block loading, environment evaluation, persistent
live independently mutable registries, shared `Hashtable` environments and
functions, populated private per-instance `Hashtable` environments,
typed `ScriptEnvironment` function/block/environment/predicate/operator
introspection, exact table replacement/null behavior and stable per-instance
portable environment stacks, filesystem-backed `hasChanged` dependency
tracking, charset conversion controls, type-exact overloads, and configured-set
load/unload differences, plus
source-audited corpus fixture adapters and inert closure-backed interface
proxies, and the observable `Thread.currentThread` fork identity.
Bare Sleep closures and `newInstance` interface proxies are supported as
listing filters with source-ordered callbacks, Java boolean coercion, and
soft-error propagation. Arbitrary JVM filter objects and `deleteOnExit` remain
importer-owned `File` operations. The returned URI object provides file-component getters,
string/value methods, comparison, and hashing; its `toASCIIString` is exact for
ASCII-only values, while the non-ASCII NFC-normalizing branch remains
importer-owned. The legacy URL object exposes its unescaped component/value
surface without connection methods. The cached Path preserves the provider's
normalization and repeated-call identity, but intentionally exposes only
class/type/string coercion and `toFile`, matching the methods Sleep 2.1 can
reflect on the package-private provider class. Filesystem-space calls are exact
on Linux, Darwin, and Windows and return zero on unsupported targets or native
failure.
File keeps its normalized abstract pathname as exact Java UTF-16 units plus
OPFOR byte provenance, separately from the deliberate host-filesystem
encoding used for effects. Windows invalid-path checks include NUL and trailing
component spaces. Windows hidden-attribute behavior and Unix directory/special
file write-access checks remain conservative pure-Go boundaries; regular-file
read/write checks use effective opens. Unpaired surrogates remain exact for
identity but require replacement at Go's OS-path boundary. Focused portable
File tests pin the source boundary and edge cases.
Resource quotas are opt-in and are not a security sandbox. Importers must still
bound their own callbacks, object bridges, retained values, and operating-system
effects.

## Independence

OPFOR is an independent compatibility implementation. Cobalt Strike and
Aggressor Script are names used to describe compatibility; this project is not
affiliated with or endorsed by Fortra or the Cobalt Strike authors.

## License

OPFOR is licensed under the Apache License 2.0. Vendored third-party fixtures
remain under their stated upstream licenses.
The stable, typed, error-returning TimSort implementation is adapted from the
MIT-licensed Go implementation by Mike Kroutikov and the maintained psilva261
fork; `NOTICE` and `third_party_licenses/psilva261-timsort-MIT.txt` retain the
attribution and license text.
