# Client UI and execution-control callbacks

This guide covers the Aggressor functions whose effects belong to an embedding
client: 84 general UI functions, the 20-function custom-dialog family, five
prompt functions, `brk()`, and `dispatch_event(callback)`. Start with the
[Aggressor extension index](README.md) and apply the context, ownership,
precedence, and error rules in the [shared provider contract](provider-contract.md)
to every implementation below.

OPFOR owns parsing, arity checks, argument resolution, callback retention,
script provenance, and lifecycle revocation. The embedding application owns
actual windows, event-loop integration, clipboard and browser effects, user
input, and breakpoint presentation. None of these APIs implies Swing or any
other UI toolkit.

## Install the boundaries

The five independent options are:

```go
runtime, err := opfor.New(
	opfor.WithAggressorClientUIProvider(clientUI),
	opfor.WithAggressorDialogProvider(dialogs),
	opfor.WithAggressorPromptProvider(prompts),
	opfor.WithAggressorBreakpointProvider(breakpoints),
	opfor.WithAggressorEventDispatcher(events),
)
```

Install only boundaries whose complete family the application is prepared to
own. Once a typed provider is configured, its error is authoritative; returning
`UnsupportedError` does not make OPFOR retry the call through `WithHost`.
`WithFunction(name, ...)` remains the exact-name override with highest
precedence regardless of option order.

## General client UI: 84 functions

`WithAggressorClientUIProvider` installs:

```go
type AggressorClientUIProvider interface {
	HandleAggressorClientUI(
		context.Context,
		AggressorClientUIRequest,
	) (Value, error)
}
```

Switch on `request.Operation`, not a second table of raw names. Every
`AggressorClientUIOperation` string is the exact script-facing function name;
`request.Name` is retained for diagnostics. `RuntimeID`, `Script`, and `Span`
identify the call. `Bindings` is the guarded host-to-script capability described
in [catalogs, bindings, and dispatch](catalogs-bindings-dispatch.md).

`Arguments` is a detached top-level slice whose entries were resolved exactly
once. Compound and object values retain normal identity and capability
provenance. `openPayloadHelper` is the exception: its callback is removed from
`Arguments` and delivered as the retained, multi-shot `Callback` field.

### Tabs, visualization, popup, navigation, and messages

| Function | Arity | Typed-provider success result | Extra capability |
| --- | ---: | --- | --- |
| `addTab` | 3 | provider `Value` | — |
| `addVisualization` | 2 | provider `Value` | — |
| `showVisualization` | 1 | provider `Value` | — |
| `show_popup` | 3 | provider `Value` | `Popup` when the named popup has registrations |
| `menubar` | 2 | provider `Value` | `Popup` when the named popup has registrations |
| `popup_clear` | 1 | `$null`; provider value discarded | clears exact popup layers only after provider success |
| `separator` | 0 | provider `Value` | `Composition` inside popup/menu composition |
| `removeTab` | 0 | provider `Value` | — |
| `nextTab` | 0 | provider `Value` | — |
| `previousTab` | 0 | provider `Value` | — |
| `add_to_clipboard` | 1 | provider `Value` | — |
| `url_open` | 1 | provider `Value` | — |
| `show_error` | 1 | provider `Value` | — |
| `show_message` | 1 | provider `Value` | — |

`Popup` implements `AggressorPopupComposer`. Calling `Compose(ctx)` invokes
every exact popup layer captured for that request in registration order. It is
pinned to that generation: `popup_clear`, binding-owner unload, or creator
unload makes it return `ErrAggressorPopupStale`; a newer same-name registration
is never substituted. `Composition` is a detached ancestry snapshot used to
place `separator`, `insert_color_menu`, and `insert_component` in the current
menu tree.

### Browser and component helpers

| Functions | Arity | Typed-provider success result |
| --- | ---: | --- |
| `bbrowser`, `pgraph`, `sbrowser`, `tbrowser` | 0 | provider `Value` |
| `colorMenu` | 2 | provider `Value` |
| `file_browser`, `process_browser` | 0 | `$null`; provider value discarded |
| `insert_color_menu`, `insert_component` | 1 | `$null`; provider value discarded |

The two insertion functions and `separator` may carry `Composition` when
called during popup/menu evaluation. A provider supplies actual component
objects and event-loop behavior; OPFOR does not construct or inspect widgets.

### The 61 `open*` functions

All ordinary `open*` calls transfer the provider's successful `Value` directly
to the script. Return an application object where the host contract exposes
one, or `opfor.Null()` for an effect-only window. The only forced-null member is
`openPayloadHelper`, because its selected listener is delivered through the
callback.

| Arity | Functions |
| ---: | --- |
| 0 | `openAboutDialog`, `openApplicationManager`, `openAutoRunDialog`, `openBeaconBrowser`, `openCloneSiteDialog`, `openConnectDialog`, `openCredentialManager`, `openDefaultShortcutsDialog`, `openDownloadBrowser`, `openEventLog`, `openHTMLApplicationDialog`, `openHostFileDialog`, `openInterfaceManager`, `openJavaSignedAppletDialog`, `openJavaSmartAppletDialog`, `openKeystrokeBrowser`, `openListenerManager`, `openMalleableProfileDialog`, `openOfficeMacroDialog`, `openPayloadGeneratorDialog`, `openPayloadGeneratorStageDialog`, `openPayloadStoreManager`, `openPowerShellWebDialog`, `openPreferencesDialog`, `openSOCKSBrowser`, `openScreenshotBrowser`, `openScriptConsole`, `openScriptManager`, `openScriptedWebDialog`, `openSiteManager`, `openSpearPhishDialog`, `openSystemInformationDialog`, `openSystemProfilerDialog`, `openTargetBrowser`, `openWebLog`, `openWindowsExecutableDialog`, `openWindowsExecutableStageAllDialog`, `openWindowsExecutableStageDialog` |
| 1 | `openBeaconConsole`, `openBrowserPivotSetup`, `openCovertVPNSetup`, `openElevateDialog`, `openFileBrowser`, `openGoldenTicketDialog`, `openMakeTokenDialog`, `openNewCredentialDialog`, `openOneLinerDialog`, `openOrActivate`, `openPivotListenerSetup`, `openPortScanner`, `openPortScannerLocal`, `openProcessBrowser`, `openSOCKSSetup`, `openServiceBrowser`, `openSpawnAsDialog`, `openSpawnDialog` |
| 2 | `openJobConsole`, `openJumpDialog` |
| 0–1 | `openJobBrowser` |
| 4–6 | `openUserDefinedBrowser` |
| 1 callback | `openPayloadHelper` |

`openPayloadHelper` receives a non-nil retained `request.Callback`; invoke it
with the selected listener as the first positional argument. The callback is
multi-shot, accepts a fresh caller context for every call, and is rejected
after its creating generation unloads or the runtime closes. A successful
provider call itself returns `$null`.

### Provider skeleton

This skeleton treats `postToUI` and the concrete client operations as
application-owned adapters. Complete every operation before installing the
provider.

```go
clientUI := opfor.AggressorClientUIProviderFunc(func(
	ctx context.Context,
	request opfor.AggressorClientUIRequest,
) (opfor.Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}

	switch request.Operation {
	case opfor.AggressorClientUIShowPopup,
		opfor.AggressorClientUIMenubar:
		if request.Popup == nil {
			return opfor.Null(), nil
		}
		// Compose with a caller-owned context on the application's UI thread.
		return opfor.Null(), postToUI(ctx, func(uiCtx context.Context) error {
			return request.Popup.Compose(uiCtx)
		})

	case opfor.AggressorClientUIOpenPayloadHelper:
		callback := request.Callback
		return opfor.Null(), postToUI(ctx, func(uiCtx context.Context) error {
			listener, err := chooseListener(uiCtx)
			if err != nil {
				return err
			}
			_, err = callback.Invoke(uiCtx, opfor.String(listener))
			return err
		})

	case opfor.AggressorClientUIOpenURL:
		return opfor.Null(), openURL(ctx, request.Arguments[0].String())

	default:
		return opfor.Null(), fmt.Errorf(
			"client UI operation %q is not implemented", request.Operation,
		)
	}
})
```

Do not retain `ctx` after `HandleAggressorClientUI` returns. Retained
`Callback`, `Popup`, `Bindings`, compound values, and objects have their own
documented lifetimes and may retain runtime or script capabilities.

### Results and fallback

- A configured provider is called synchronously exactly once for every valid
  call and may be called concurrently.
- Provider errors are authoritative and return `$null` plus the error.
- With no provider, the original reference-bearing `Invocation` reaches
  `WithHost` exactly once, and the Host result/error passes through unchanged.
- `popup_clear` snapshots its name before Host dispatch and removes local
  registrations only after Host or provider success. An error preserves them.
- `WithFunction` overrides arity checking, provider dispatch, and Host fallback
  for that exact name.

## Custom dialogs: 20 functions

Install `WithAggressorDialogProvider` to let OPFOR own dialog construction while
the application presents the final snapshot:

```go
type AggressorDialogProvider interface {
	PresentAggressorDialog(
		context.Context,
		AggressorDialogPresentation,
		AggressorDialogResponder,
	) error
}
```

| Functions | Arity | Behavior |
| --- | ---: | --- |
| `dialog` | 3 | `(title, %defaults, callback)`; returns an opaque runtime-owned dialog |
| `dialog_description` | 2–3 | adds description and optional line count; returns `$null` |
| `dialog_show` | 1 | presents the completed snapshot; returns `$null` |
| `dbutton_action`, `dbutton_help` | 2 | add an action label or provider-owned Help URL; return `$null` |
| `drow_beacon`, `drow_exploits`, `drow_file`, `drow_interface`, `drow_krbtgt`, `drow_listener`, `drow_listener_smb`, `drow_listener_stage`, `drow_mailserver`, `drow_proxyserver`, `drow_site`, `drow_text_big` | 3 | `(dialog, name, label)` |
| `drow_checkbox`, `drow_combobox` | 4 | add checkbox text or an array of options |
| `drow_text` | 3–4 | optional width |

`drow_listener_smb` is deprecated and canonicalizes to the listener-stage row
kind while retaining its exact function spelling. `drow_proxyserver` is also
deprecated but remains a distinct row kind.

`AggressorDialogPresentation` is a detached top-level snapshot with stable
dialog, row, and button IDs plus creator/presenter provenance. Values inside
defaults, rows, and option lists retain ordinary identity. An action response
uses:

```go
value, err := responder.Activate(
	callbackCtx,
	actionButton.ID,
	opfor.AggressorDialogRowValue{RowID: row.ID, Value: selected},
)
```

Unknown or repeated row IDs and Help-button IDs are rejected without consuming
the responder. Omitted rows receive their captured default or `$null`. A valid
action calls the script callback with `(dialog, buttonLabel, %rowValues)`;
`Dismiss()` closes the dialog without entering script code. The first valid
terminal operation wins, even if its script callback later fails.

A provider may answer synchronously or retain the responder for later UI work:

```go
dialogs := opfor.AggressorDialogProviderFunc(func(
	ctx context.Context,
	presentation opfor.AggressorDialogPresentation,
	responder opfor.AggressorDialogResponder,
) error {
	return postDialog(ctx, presentation, func(
		callbackCtx context.Context,
		button opfor.AggressorDialogButtonID,
		rows []opfor.AggressorDialogRowValue,
	) error {
		_, err := responder.Activate(callbackCtx, button, rows...)
		return err
	}, responder.Dismiss)
})
```

If no dialog provider is configured, every valid family call reaches Host with
its original invocation. Once a provider is configured, OPFOR's dialog handle
cannot be mixed with a partial Host implementation; implement the complete
family or override selected names deliberately with `WithFunction`.

## Prompts: five functions

`WithAggressorPromptProvider` receives one resolved presentation and a one-shot
responder:

| Function | Arity | Presentation fields | Callback ABI |
| --- | ---: | --- | --- |
| `prompt_confirm` | 3 | text, title | zero values |
| `prompt_text` | 3 | text, default | one provider-produced `Value` |
| `prompt_directory_open` | 4 | title, default, exact multiple-selection value and truth | one `Value` |
| `prompt_file_open` | 4 | title, default, exact multiple-selection value and truth | one `Value` |
| `prompt_file_save` | 2 | default | one `Value` |

OPFOR does not invent a representation for multiple selection. A compatible
provider should supply the documented comma-separated scalar itself. The value
passed to `Accept` reaches callback `$1` unchanged.

```go
prompts := opfor.AggressorPromptProviderFunc(func(
	ctx context.Context,
	presentation opfor.AggressorPromptPresentation,
	responder opfor.AggressorPromptResponder,
) error {
	return postPrompt(ctx, presentation,
		func(callbackCtx context.Context, answer opfor.Value) error {
			if presentation.Kind == opfor.AggressorPromptConfirm {
				_, err := responder.Accept(callbackCtx)
				return err
			}
			_, err := responder.Accept(callbackCtx, answer)
			return err
		},
		responder.Dismiss,
	)
})
```

`Done()` on dialog and prompt responders closes on response, dismissal,
provider failure, lifecycle revocation, or runtime close. A response begun
before the provider method returns is drained at that boundary and shares the
presenting execution's instruction meter. A retained response invoked later is
a detached UI event with a fresh top-level meter. Synchronous callbacks may
reenter script unload or runtime close; avoid holding application UI locks while
entering them. Returning an error after a successful response cannot roll the
response back, so providers should normally return nil once `Activate`,
`Accept`, or `Dismiss` succeeds.

## Breakpoints: `brk()`

`WithAggressorBreakpointProvider` is a synchronous presentation boundary for
the interpreter-owned zero-argument `brk()` function. The provider receives a
detached `AggressorBreakpointSnapshot` containing runtime/script provenance,
source and timestamp, locals, globals, closure variables, innermost-first stack
frames and call stack, and the current function.

```go
breakpoints := opfor.AggressorBreakpointProviderFunc(func(
	ctx context.Context,
	snapshot opfor.AggressorBreakpointSnapshot,
) error {
	// Blocking until Continue is valid, but ctx must end the wait.
	return debugger.Pause(ctx, snapshot.Clone())
})
```

The successful script result is the documented nine-key debug hash whether a
provider is present or not. Without a provider, OPFOR prints that hash to the
runtime's stdout and returns it. `brk()` never falls through to Host. A provider
error is authoritative; `WithFunction("brk", ...)` still overrides the native
implementation.

## Event-loop scheduling: `dispatch_event`

`dispatch_event(callback)` always validates one callable and returns `$null` on
success. The stock dispatcher invokes it synchronously with the live evaluator
context. `WithAggressorEventDispatcher` lets an importer marshal it onto an
event loop:

```go
events := opfor.AggressorEventDispatcherFunc(func(
	ctx context.Context,
	callback opfor.Callable,
) error {
	return eventLoop.Enqueue(ctx, func(callbackCtx context.Context) error {
		_, err := callback.Invoke(callbackCtx)
		return err
	})
})
```

An importer dispatcher receives a sanitized context that preserves importer
values, cancellation, and deadlines without exposing evaluator-private state.
An invocation begun before `DispatchAggressorEvent` returns shares the caller's
instruction meter; a queued invocation begun later gets a fresh top-level
meter. The retained callback rejects calls after unload or runtime close.
Dispatcher and callback errors are authoritative, and this native function has
no Host fallback. `WithFunction("dispatch_event", ...)` has highest precedence.

## ScriptLoader children

Portable `ScriptLoader` children are fresh runtimes. They inherit configured
client-UI, dialog, prompt, breakpoint, and event-dispatcher boundaries through
the active Aggressor extension profile, including through nested children.
They do not inherit a parent's script-owned popup/menu registrations or open UI
resources. A child has a distinct `RuntimeID`; requests and retained callbacks
target the child and its exact script generation. Unconfigured providers remain
unconfigured, so each child keeps the same Host/headless default policy.

## Tests and drift checks

At minimum, an adapter should test:

- every operation discriminator and exact arity;
- successful value/null behavior and missing-provider fallback;
- authoritative provider errors and cancellation;
- callback/responder use before and after unload and runtime close;
- synchronous reentry, retained asynchronous invocation, and concurrent calls;
- popup generation pinning, clear ordering, and composition ancestry; and
- ScriptLoader child runtime identity and provider inheritance.

Use the runtime inventory instead of maintaining an unchecked second list:

```go
for _, contract := range opfor.DefaultAggressorFunctionContracts() {
	switch contract.TypedProvider {
	case "AggressorClientUIProvider",
		"AggressorDialogProvider",
		"AggressorPromptProvider":
		// Assert that the adapter has an intentional route for contract.Name and
		// that its expectation matches arity, callbacks, and TypedResult.
	}
}
```

The focused runtime regressions are in
[`builtins_aggressor_client_ui_test.go`](../../internal/opfor/builtins_aggressor_client_ui_test.go),
[`builtins_aggressor_client_ui_open_test.go`](../../internal/opfor/builtins_aggressor_client_ui_open_test.go),
[`builtins_aggressor_ui_test.go`](../../internal/opfor/builtins_aggressor_ui_test.go),
[`builtins_aggressor_ui_window_test.go`](../../internal/opfor/builtins_aggressor_ui_window_test.go),
[`builtins_aggressor_breakpoint_test.go`](../../internal/opfor/builtins_aggressor_breakpoint_test.go),
and [`builtins_aggressor_client_test.go`](../../internal/opfor/builtins_aggressor_client_test.go).
