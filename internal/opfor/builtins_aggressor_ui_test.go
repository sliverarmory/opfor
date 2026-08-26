package opfor

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type aggressorUITestCallable func(context.Context, ...Value) (Value, error)

func (function aggressorUITestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return function(ctx, values...)
}

type aggressorUITestDialogCall struct {
	presentation AggressorDialogPresentation
	responder    AggressorDialogResponder
}

type aggressorUITestDialogProvider struct {
	mu      sync.Mutex
	calls   []aggressorUITestDialogCall
	present func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error
}

func (provider *aggressorUITestDialogProvider) PresentAggressorDialog(
	ctx context.Context,
	presentation AggressorDialogPresentation,
	responder AggressorDialogResponder,
) error {
	provider.mu.Lock()
	provider.calls = append(provider.calls, aggressorUITestDialogCall{
		presentation: presentation,
		responder:    responder,
	})
	present := provider.present
	provider.mu.Unlock()
	if present == nil {
		return nil
	}
	return present(ctx, presentation, responder)
}

func (provider *aggressorUITestDialogProvider) snapshot() []aggressorUITestDialogCall {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]aggressorUITestDialogCall(nil), provider.calls...)
}

type aggressorUITestPromptCall struct {
	presentation AggressorPromptPresentation
	responder    AggressorPromptResponder
}

type aggressorUITestPromptProvider struct {
	mu      sync.Mutex
	calls   []aggressorUITestPromptCall
	present func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error
}

func (provider *aggressorUITestPromptProvider) PresentAggressorPrompt(
	ctx context.Context,
	presentation AggressorPromptPresentation,
	responder AggressorPromptResponder,
) error {
	provider.mu.Lock()
	provider.calls = append(provider.calls, aggressorUITestPromptCall{
		presentation: presentation,
		responder:    responder,
	})
	present := provider.present
	provider.mu.Unlock()
	if present == nil {
		return nil
	}
	return present(ctx, presentation, responder)
}

func (provider *aggressorUITestPromptProvider) snapshot() []aggressorUITestPromptCall {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]aggressorUITestPromptCall(nil), provider.calls...)
}

type aggressorUITestContextKey struct{}

func aggressorUITestOwner(t *testing.T, runtimeInstance *Runtime) *Script {
	t.Helper()
	program, err := CompileString(t.Name()+"-owner.cna", `return $null;`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func aggressorUITestInvocation(
	runtimeInstance *Runtime,
	owner *Script,
	name string,
	span Span,
	values ...Value,
) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	scriptID := ScriptID(0)
	if owner != nil {
		scriptID = owner.ID()
	}
	return Invocation{
		Runtime:   runtimeInstance,
		Script:    scriptID,
		Name:      name,
		Span:      span,
		Arguments: arguments,
	}
}

func aggressorUITestInvoke(
	ctx context.Context,
	runtimeInstance *Runtime,
	owner *Script,
	name string,
	span Span,
	values ...Value,
) (Value, error) {
	return runtimeInstance.invoke(ctx, aggressorUITestInvocation(runtimeInstance, owner, name, span, values...))
}

func assertAggressorUITestDoneOpen(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("responder Done channel closed before a terminal operation")
	default:
	}
}

func assertAggressorUITestDoneClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(quiescenceTestTimeout):
		t.Fatal("timed out waiting for responder Done channel")
	}
}

var aggressorUITestDialogArities = map[string][2]int{
	"dialog":              {3, 3},
	"dialog_description":  {2, 3},
	"dialog_show":         {1, 1},
	"dbutton_action":      {2, 2},
	"dbutton_help":        {2, 2},
	"drow_beacon":         {3, 3},
	"drow_checkbox":       {4, 4},
	"drow_combobox":       {4, 4},
	"drow_exploits":       {3, 3},
	"drow_file":           {3, 3},
	"drow_interface":      {3, 3},
	"drow_krbtgt":         {3, 3},
	"drow_listener":       {3, 3},
	"drow_listener_smb":   {3, 3},
	"drow_listener_stage": {3, 3},
	"drow_mailserver":     {3, 3},
	"drow_proxyserver":    {3, 3},
	"drow_site":           {3, 3},
	"drow_text":           {3, 4},
	"drow_text_big":       {3, 3},
}

var aggressorUITestPromptArities = map[string][2]int{
	"prompt_confirm":        {3, 3},
	"prompt_directory_open": {4, 4},
	"prompt_file_open":      {4, 4},
	"prompt_file_save":      {2, 2},
	"prompt_text":           {3, 3},
}

func TestAggressorUICompleteFunctionSetsAndArities(t *testing.T) {
	t.Parallel()

	dialogNames := make([]string, 0, len(aggressorUITestDialogArities))
	for name := range (&Runtime{}).aggressorDialogFunctions() {
		dialogNames = append(dialogNames, name)
	}
	sort.Strings(dialogNames)
	wantDialogNames := make([]string, 0, len(aggressorUITestDialogArities))
	for name := range aggressorUITestDialogArities {
		wantDialogNames = append(wantDialogNames, name)
	}
	sort.Strings(wantDialogNames)
	if !reflect.DeepEqual(dialogNames, wantDialogNames) {
		t.Fatalf("Aggressor dialog functions = %q, want %q", dialogNames, wantDialogNames)
	}

	promptNames := make([]string, 0, len(aggressorUITestPromptArities))
	for name := range (&Runtime{}).aggressorPromptFunctions() {
		promptNames = append(promptNames, name)
	}
	sort.Strings(promptNames)
	wantPromptNames := make([]string, 0, len(aggressorUITestPromptArities))
	for name := range aggressorUITestPromptArities {
		wantPromptNames = append(wantPromptNames, name)
	}
	sort.Strings(wantPromptNames)
	if !reflect.DeepEqual(promptNames, wantPromptNames) {
		t.Fatalf("Aggressor prompt functions = %q, want %q", promptNames, wantPromptNames)
	}

	var dialogCalls atomic.Int32
	var promptCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorDialogProvider(AggressorDialogProviderFunc(func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
			dialogCalls.Add(1)
			return nil
		})),
		WithAggressorPromptProvider(AggressorPromptProviderFunc(func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
			promptCalls.Add(1)
			return nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	allArities := make(map[string][2]int, len(aggressorUITestDialogArities)+len(aggressorUITestPromptArities))
	for name, arity := range aggressorUITestDialogArities {
		allArities[name] = arity
	}
	for name, arity := range aggressorUITestPromptArities {
		allArities[name] = arity
	}
	for name, arity := range allArities {
		name, arity := name, arity
		t.Run(name, func(t *testing.T) {
			counts := []int{arity[0] - 1, arity[1] + 1}
			for _, count := range counts {
				arguments := make([]Value, count)
				for index := range arguments {
					arguments[index] = Int(int32(index + 1))
				}
				result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
				if invokeErr == nil || !result.IsNull() {
					t.Errorf("%s/%d = (%s, %v), want null arity error", name, count, result.Describe(), invokeErr)
				}
			}
		})
	}
	if dialogCalls.Load() != 0 || promptCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("invalid arities reached dialog/prompt/Host = %d/%d/%d", dialogCalls.Load(), promptCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorDialogCompletePresentationAndCallbackABI(t *testing.T) {
	t.Parallel()

	provider := &aggressorUITestDialogProvider{}
	var hostCalls atomic.Int32
	var callbackMu sync.Mutex
	var callbackContext context.Context
	var callbackArguments []Value
	callbackResult := ArrayValue(NewArray(String("callback-result")))
	var responder AggressorDialogResponder
	var injectedExecutionToken *scriptExecutionToken
	callback := aggressorUITestCallable(func(ctx context.Context, values ...Value) (Value, error) {
		select {
		case <-responder.Done():
		default:
			return Null(), errors.New("dialog Done was open inside activated callback")
		}
		callbackMu.Lock()
		callbackContext = ctx
		callbackArguments = append([]Value(nil), values...)
		callbackMu.Unlock()
		return callbackResult, nil
	})
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed dialog route reached Host")
		})),
		WithAggressorDialogProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)

	opaqueDefault := ObjectValue(&struct{ name string }{"opaque-default"})
	nestedDefault := ArrayValue(NewArray(String("nested-default")))
	defaultHash := NewOrderedHash()
	defaultNames := []string{
		"beacon", "checkbox", "combobox", "exploits", "file", "interface",
		"krbtgt", "listener", "listener_smb", "listener_stage", "mailserver",
		"proxyserver", "site", "text", "text_narrow",
	}
	defaultValues := make(map[string]Value, len(defaultNames))
	for index, name := range defaultNames {
		value := String("default-" + name)
		if index == 0 {
			value = opaqueDefault
		} else if index == 1 {
			value = nestedDefault
		}
		defaultValues[name] = value
		defaultHash.Set(name, value)
	}
	creatorSpan := Span{Source: "complete-dialog.cna", Start: Position{Line: 3, Column: 5}}
	dialogValue, err := aggressorUITestInvoke(
		context.Background(), runtimeInstance, owner, "dialog", creatorSpan,
		String("Complete dialog"), HashValue(defaultHash), FunctionValue(callback),
	)
	if err != nil {
		t.Fatalf("dialog: %v", err)
	}
	if dialogValue.Kind() != KindObject {
		t.Fatalf("dialog result = %s, want private object", dialogValue.Describe())
	}
	// Creation snapshots the top-level dictionary while preserving the Values
	// observed at that instant.
	defaultHash.Set("beacon", String("mutated-after-dialog"))
	defaultHash.Set("late", String("not-presented"))

	descriptionSpan := Span{Source: "complete-dialog.cna", Start: Position{Line: 4, Column: 5}}
	result, err := aggressorUITestInvoke(
		context.Background(), runtimeInstance, owner, "dialog_description", descriptionSpan,
		dialogValue, String("Every documented row"), Int(7),
	)
	if err != nil || !result.IsNull() {
		t.Fatalf("dialog_description = (%s, %v), want null/nil", result.Describe(), err)
	}

	optionObject := ObjectValue(&struct{ name string }{"option-object"})
	options := NewArray(optionObject, String("second-option"))
	type rowExpectation struct {
		function     string
		kind         AggressorDialogRowKind
		name         string
		label        string
		extra        []Value
		checkboxText string
		options      []Value
		width        int32
		hasWidth     bool
		deprecated   bool
		equivalent   AggressorDialogRowKind
	}
	rows := []rowExpectation{
		{function: "drow_beacon", kind: AggressorDialogRowBeacon, name: "beacon", label: "Beacon"},
		{function: "drow_checkbox", kind: AggressorDialogRowCheckbox, name: "checkbox", label: "Checkbox", extra: []Value{String("Enabled")}, checkboxText: "Enabled"},
		{function: "drow_combobox", kind: AggressorDialogRowCombobox, name: "combobox", label: "Combobox", extra: []Value{ArrayValue(options)}, options: []Value{optionObject, String("second-option")}},
		{function: "drow_exploits", kind: AggressorDialogRowExploits, name: "exploits", label: "Exploits"},
		{function: "drow_file", kind: AggressorDialogRowFile, name: "file", label: "File"},
		{function: "drow_interface", kind: AggressorDialogRowInterface, name: "interface", label: "Interface"},
		{function: "drow_krbtgt", kind: AggressorDialogRowKRBTGT, name: "krbtgt", label: "KRBTGT"},
		{function: "drow_listener", kind: AggressorDialogRowListener, name: "listener", label: "Listener"},
		{function: "drow_listener_smb", kind: AggressorDialogRowListenerStage, name: "listener_smb", label: "SMB listener", deprecated: true, equivalent: AggressorDialogRowListenerStage},
		{function: "drow_listener_stage", kind: AggressorDialogRowListenerStage, name: "listener_stage", label: "Staged listener"},
		{function: "drow_mailserver", kind: AggressorDialogRowMailServer, name: "mailserver", label: "Mail server"},
		{function: "drow_proxyserver", kind: AggressorDialogRowProxyServer, name: "proxyserver", label: "Proxy server", deprecated: true},
		{function: "drow_site", kind: AggressorDialogRowSite, name: "site", label: "Site"},
		{function: "drow_text", kind: AggressorDialogRowText, name: "text", label: "Text", extra: []Value{Int(73)}, width: 73, hasWidth: true},
		{function: "drow_text", kind: AggressorDialogRowText, name: "text_narrow", label: "Text without width"},
		{function: "drow_text_big", kind: AggressorDialogRowTextBig, name: "text_big", label: "Big text"},
	}
	rowSpans := make([]Span, len(rows))
	for index, row := range rows {
		span := Span{Source: "complete-dialog.cna", Start: Position{Line: 10 + index, Column: 2}}
		rowSpans[index] = span
		arguments := []Value{dialogValue, String(row.name), String(row.label)}
		arguments = append(arguments, row.extra...)
		rowResult, rowErr := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, row.function, span, arguments...)
		if rowErr != nil || !rowResult.IsNull() {
			t.Fatalf("%s = (%s, %v), want null/nil", row.function, rowResult.Describe(), rowErr)
		}
	}
	options.Append(String("late-option"))

	actionSpan := Span{Source: "complete-dialog.cna", Start: Position{Line: 30, Column: 2}}
	helpSpan := Span{Source: "complete-dialog.cna", Start: Position{Line: 31, Column: 2}}
	for _, button := range []struct {
		name  string
		span  Span
		value string
	}{
		{name: "dbutton_action", span: actionSpan, value: "Run"},
		{name: "dbutton_help", span: helpSpan, value: "https://example.invalid/help"},
	} {
		buttonResult, buttonErr := aggressorUITestInvoke(
			context.Background(), runtimeInstance, owner, button.name, button.span,
			dialogValue, String(button.value),
		)
		if buttonErr != nil || !buttonResult.IsNull() {
			t.Fatalf("%s = (%s, %v), want null/nil", button.name, buttonResult.Describe(), buttonErr)
		}
	}
	if got := len(provider.snapshot()); got != 0 {
		t.Fatalf("dialog provider called during construction %d time(s)", got)
	}

	showSpan := Span{Source: "complete-dialog.cna", Start: Position{Line: 32, Column: 2}}
	showResult, err := aggressorUITestInvoke(
		context.Background(), runtimeInstance, owner, "dialog_show", showSpan, dialogValue,
	)
	if err != nil || !showResult.IsNull() {
		t.Fatalf("dialog_show = (%s, %v), want null/nil", showResult.Describe(), err)
	}
	calls := provider.snapshot()
	if len(calls) != 1 {
		t.Fatalf("dialog provider calls = %d, want one", len(calls))
	}
	call := calls[0]
	responder = call.responder
	presentation := call.presentation
	if presentation.ID == 0 || presentation.RuntimeID != runtimeInstance.ID() ||
		presentation.CreatorScript != owner.ID() || presentation.CreationSpan != creatorSpan ||
		presentation.PresenterScript != owner.ID() || presentation.PresentationSpan != showSpan ||
		presentation.Title != "Complete dialog" {
		t.Fatalf("dialog presentation identity/provenance = %#v", presentation)
	}
	if !presentation.HasDescription || presentation.Description != "Every documented row" ||
		presentation.DescriptionLines != 7 || presentation.DescriptionScript != owner.ID() ||
		presentation.DescriptionSpan != descriptionSpan {
		t.Fatalf("dialog description = %#v", presentation)
	}
	if len(presentation.Defaults) != len(defaultNames) {
		t.Fatalf("dialog defaults = %d, want %d", len(presentation.Defaults), len(defaultNames))
	}
	for index, name := range defaultNames {
		entry := presentation.Defaults[index]
		if entry.Name != name || !entry.Value.IdentityEqual(defaultValues[name]) {
			t.Fatalf("default[%d] = %q/%s, want %q/%s", index, entry.Name, entry.Value.Describe(), name, defaultValues[name].Describe())
		}
	}
	if len(presentation.Rows) != len(rows) {
		t.Fatalf("dialog rows = %d, want %d", len(presentation.Rows), len(rows))
	}
	seenRows := make(map[AggressorDialogRowID]struct{}, len(rows))
	for index, want := range rows {
		got := presentation.Rows[index]
		if got.ID == 0 {
			t.Fatalf("row[%d] has zero ID", index)
		}
		if _, duplicate := seenRows[got.ID]; duplicate {
			t.Fatalf("row[%d] repeats ID %d", index, got.ID)
		}
		seenRows[got.ID] = struct{}{}
		if got.Function != want.function || got.Kind != want.kind || got.Name != want.name || got.Label != want.label ||
			got.CheckboxText != want.checkboxText || got.Width != want.width || got.HasWidth != want.hasWidth ||
			got.Deprecated != want.deprecated || got.Equivalent != want.equivalent ||
			got.Script != owner.ID() || got.Span != rowSpans[index] {
			t.Fatalf("row[%d] = %#v, want %#v", index, got, want)
		}
		wantDefault, hasDefault := defaultValues[want.name]
		if got.HasDefault != hasDefault || (hasDefault && !got.Default.IdentityEqual(wantDefault)) || (!hasDefault && !got.Default.IsNull()) {
			t.Fatalf("row[%d] default = %s/%v, want %s/%v", index, got.Default.Describe(), got.HasDefault, wantDefault.Describe(), hasDefault)
		}
		if len(got.Options) != len(want.options) {
			t.Fatalf("row[%d] options = %d, want %d (top-level options were not detached)", index, len(got.Options), len(want.options))
		}
		for optionIndex := range want.options {
			if !got.Options[optionIndex].IdentityEqual(want.options[optionIndex]) {
				t.Fatalf("row[%d] option[%d] = %s, want identical %s", index, optionIndex, got.Options[optionIndex].Describe(), want.options[optionIndex].Describe())
			}
		}
	}
	if len(presentation.Buttons) != 2 || presentation.Buttons[0].ID == 0 || presentation.Buttons[1].ID == 0 ||
		presentation.Buttons[0].ID == presentation.Buttons[1].ID {
		t.Fatalf("dialog buttons = %#v", presentation.Buttons)
	}
	if got := presentation.Buttons[0]; got.Kind != AggressorDialogButtonAction || got.Label != "Run" || got.URL != "" || got.Script != owner.ID() || got.Span != actionSpan {
		t.Fatalf("action button = %#v", got)
	}
	if got := presentation.Buttons[1]; got.Kind != AggressorDialogButtonHelp || got.Label != "Help" || got.URL != "https://example.invalid/help" || got.Script != owner.ID() || got.Span != helpSpan {
		t.Fatalf("help button = %#v", got)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("typed dialog route reached Host %d time(s)", hostCalls.Load())
	}
	assertAggressorUITestDoneOpen(t, responder.Done())

	providedBeacon := ObjectValue(&struct{ name string }{"provided-beacon"})
	providedBigText := ArrayValue(NewArray(String("provided-big-text")))
	injectedExecutionToken = &scriptExecutionToken{script: owner}
	injectedExecutionToken.active.Store(true)
	activateContext := context.WithValue(context.Background(), aggressorUITestContextKey{}, "importer-context")
	activateContext = context.WithValue(activateContext, scriptExecutionContextKey{}, injectedExecutionToken)
	activateResult, err := responder.Activate(
		activateContext,
		presentation.Buttons[0].ID,
		AggressorDialogRowValue{RowID: presentation.Rows[0].ID, Value: providedBeacon},
		AggressorDialogRowValue{RowID: presentation.Rows[len(presentation.Rows)-1].ID, Value: providedBigText},
	)
	if err != nil || !activateResult.IdentityEqual(callbackResult) {
		t.Fatalf("Activate = (%s, %v), want identical callback result %s", activateResult.Describe(), err, callbackResult.Describe())
	}
	assertAggressorUITestDoneClosed(t, responder.Done())
	callbackMu.Lock()
	seenContext := callbackContext
	seenArguments := append([]Value(nil), callbackArguments...)
	callbackMu.Unlock()
	if seenContext == nil || seenContext.Value(aggressorUITestContextKey{}) != "importer-context" {
		t.Fatalf("callback importer context = %#v, want preserved value", seenContext)
	}
	if currentFiber(seenContext) != nil || currentBindingInvocation(seenContext) != nil ||
		seenContext.Value(executionMeterKey{}) != nil ||
		seenContext.Value(nativeDispatchStateContextKey{}) != nil ||
		seenContext.Value(runtimeExecutionContextKey{}) != nil ||
		seenContext.Value(scriptUnloadContextKey{}) != nil ||
		seenContext.Value(runtimeCloseContextKey{}) != nil {
		t.Fatal("dialog callback retained OPFOR-private evaluator or lifecycle state")
	}
	callbackExecutionToken, _ := seenContext.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	if callbackExecutionToken == nil || callbackExecutionToken == injectedExecutionToken ||
		callbackExecutionToken.script != owner || callbackExecutionToken.parent != nil {
		t.Fatalf("dialog callback execution token = %#v, want fresh owner admission without injected ancestry", callbackExecutionToken)
	}
	if len(seenArguments) != 3 || !seenArguments[0].IdentityEqual(dialogValue) || seenArguments[1].String() != "Run" {
		t.Fatalf("dialog callback arguments = %v, want exact (dialog, label, values-hash) ABI", seenArguments)
	}
	responseHash, ok := seenArguments[2].Hash()
	if !ok || responseHash == nil || responseHash.Len() != len(rows) {
		t.Fatalf("dialog callback values = %s, want fresh %d-entry hash", seenArguments[2].Describe(), len(rows))
	}
	if seenArguments[2].IdentityEqual(HashValue(defaultHash)) {
		t.Fatal("dialog callback reused the input defaults hash")
	}
	for index, row := range rows {
		value, exists := responseHash.Get(row.name)
		if !exists {
			t.Fatalf("dialog callback hash omitted row %q", row.name)
		}
		want := Null()
		if index == 0 {
			want = providedBeacon
		} else if index == len(rows)-1 {
			want = providedBigText
		} else if capturedDefault, exists := defaultValues[row.name]; exists {
			want = capturedDefault
		}
		if !value.IdentityEqual(want) {
			t.Fatalf("dialog callback value %q = %s, want identical %s", row.name, value.Describe(), want.Describe())
		}
	}
	if err := responder.Dismiss(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Dismiss after Activate error = %v, want descriptive closed error", err)
	}
	if result, err := responder.Activate(context.Background(), presentation.Buttons[0].ID); err == nil || !result.IsNull() || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("second Activate = (%s, %v), want null descriptive closed error", result.Describe(), err)
	}
}

func TestAggressorDialogDescriptionDefaultsAndCapsLines(t *testing.T) {
	t.Parallel()

	provider := &aggressorUITestDialogProvider{}
	runtimeInstance, err := New(WithAggressorDialogProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) { return Null(), nil }))

	for _, test := range []struct {
		name      string
		lineValue []Value
		wantLines int32
	}{
		{name: "default", wantLines: 2},
		{name: "explicit", lineValue: []Value{Int(8)}, wantLines: 8},
		{name: "capped", lineValue: []Value{Int(99)}, wantLines: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			dialogValue, createErr := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog", Span{}, String(test.name), HashValue(NewHash()), callback)
			if createErr != nil {
				t.Fatal(createErr)
			}
			arguments := []Value{dialogValue, String("description")}
			arguments = append(arguments, test.lineValue...)
			if _, descriptionErr := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_description", Span{}, arguments...); descriptionErr != nil {
				t.Fatal(descriptionErr)
			}
			if _, showErr := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue); showErr != nil {
				t.Fatal(showErr)
			}
		})
	}
	calls := provider.snapshot()
	if len(calls) != 3 {
		t.Fatalf("dialog provider calls = %d, want 3", len(calls))
	}
	for index, want := range []int32{2, 8, 20} {
		if calls[index].presentation.DescriptionLines != want {
			t.Fatalf("description[%d] lines = %d, want %d", index, calls[index].presentation.DescriptionLines, want)
		}
	}
}

func TestAggressorDialogResponseValidationDoesNotConsume(t *testing.T) {
	t.Parallel()

	provider := &aggressorUITestDialogProvider{}
	var callbackCalls atomic.Int32
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		callbackCalls.Add(1)
		return Int(42), nil
	}))
	runtimeInstance, err := New(WithAggressorDialogProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	defaults := NewHash()
	defaults.Set("value", String("default"))
	dialogValue, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog", Span{}, String("validation"), HashValue(defaults), callback)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "drow_text", Span{}, dialogValue, String("value"), String("Value")); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dbutton_action", Span{}, dialogValue, String("Accept")); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dbutton_help", Span{}, dialogValue, String("https://example.invalid")); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue); err != nil {
		t.Fatal(err)
	}
	calls := provider.snapshot()
	if len(calls) != 1 {
		t.Fatalf("provider calls = %d, want one", len(calls))
	}
	call := calls[0]
	action := call.presentation.Buttons[0].ID
	help := call.presentation.Buttons[1].ID
	row := call.presentation.Rows[0].ID

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name      string
		ctx       context.Context
		button    AggressorDialogButtonID
		rowValues []AggressorDialogRowValue
		want      error
	}{
		{name: "canceled context", ctx: canceledContext, button: action, want: context.Canceled},
		{name: "unknown button", ctx: context.Background(), button: action + 100},
		{name: "help button", ctx: context.Background(), button: help},
		{name: "unknown row", ctx: context.Background(), button: action, rowValues: []AggressorDialogRowValue{{RowID: row + 100, Value: String("bad")}}},
		{name: "duplicate row", ctx: context.Background(), button: action, rowValues: []AggressorDialogRowValue{{RowID: row, Value: String("first")}, {RowID: row, Value: String("second")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, responseErr := call.responder.Activate(test.ctx, test.button, test.rowValues...)
			if responseErr == nil || !result.IsNull() {
				t.Fatalf("Activate = (%s, %v), want null validation error", result.Describe(), responseErr)
			}
			if test.want != nil && !errors.Is(responseErr, test.want) {
				t.Fatalf("Activate error = %v, want %v", responseErr, test.want)
			}
			assertAggressorUITestDoneOpen(t, call.responder.Done())
			if callbackCalls.Load() != 0 {
				t.Fatalf("validation failure invoked callback %d time(s)", callbackCalls.Load())
			}
		})
	}
	result, err := call.responder.Activate(context.Background(), action, AggressorDialogRowValue{RowID: row, Value: String("valid")})
	if err != nil || result.Int32() != 42 {
		t.Fatalf("valid Activate = (%s, %v), want 42/nil", result.Describe(), err)
	}
	if callbackCalls.Load() != 1 {
		t.Fatalf("callback calls = %d, want one", callbackCalls.Load())
	}
	assertAggressorUITestDoneClosed(t, call.responder.Done())
}

func TestAggressorDialogIdentityTypeAndOneShotState(t *testing.T) {
	t.Parallel()

	firstProvider := &aggressorUITestDialogProvider{}
	firstRuntime, err := New(WithAggressorDialogProvider(firstProvider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstRuntime.Close(context.Background()) })
	firstOwner := aggressorUITestOwner(t, firstRuntime)
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) { return Null(), nil }))

	create := func(title string) Value {
		value, createErr := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dialog", Span{}, String(title), HashValue(NewHash()), callback)
		if createErr != nil {
			t.Fatalf("dialog(%q): %v", title, createErr)
		}
		return value
	}
	first := create("first")
	second := create("second")
	if first.IdentityEqual(second) {
		t.Fatal("two dialogs shared object identity")
	}
	if _, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dbutton_action", Span{}, first, String("Run")); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dialog_show", Span{}, first); err != nil {
		t.Fatal(err)
	}
	firstCalls := firstProvider.snapshot()
	if len(firstCalls) != 1 || firstCalls[0].presentation.ID == 0 {
		t.Fatalf("first presentation = %#v", firstCalls)
	}
	if _, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dbutton_action", Span{}, second, String("Run")); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dialog_show", Span{}, second); err != nil {
		t.Fatal(err)
	}
	firstCalls = firstProvider.snapshot()
	if len(firstCalls) != 2 || firstCalls[1].presentation.ID == 0 || firstCalls[0].presentation.ID == firstCalls[1].presentation.ID {
		t.Fatalf("runtime-local dialog IDs = %v/%v", firstCalls[0].presentation.ID, firstCalls[1].presentation.ID)
	}

	for _, mutation := range []struct {
		name   string
		values []Value
	}{
		{name: "drow_text", values: []Value{first, String("late"), String("Late")}},
		{name: "dbutton_help", values: []Value{first, String("https://example.invalid/late")}},
		{name: "dialog_description", values: []Value{first, String("late")}},
		{name: "dialog_show", values: []Value{first}},
	} {
		result, mutationErr := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, mutation.name, Span{}, mutation.values...)
		if mutationErr == nil || !result.IsNull() {
			t.Errorf("post-show %s = (%s, %v), want null state error", mutation.name, result.Describe(), mutationErr)
		}
	}
	if got := len(firstProvider.snapshot()); got != 2 {
		t.Fatalf("second dialog_show re-entered provider: calls = %d", got)
	}

	secondProvider := &aggressorUITestDialogProvider{}
	secondRuntime, err := New(WithAggressorDialogProvider(secondProvider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondRuntime.Close(context.Background()) })
	secondOwner := aggressorUITestOwner(t, secondRuntime)
	for _, foreign := range []Value{Int(1), ObjectValue(&struct{}{}), first} {
		result, foreignErr := aggressorUITestInvoke(context.Background(), secondRuntime, secondOwner, "drow_text", Span{}, foreign, String("name"), String("label"))
		if foreignErr == nil || !result.IsNull() {
			t.Errorf("foreign dialog %s = (%s, %v), want null type/runtime error", foreign.Describe(), result.Describe(), foreignErr)
		}
	}
	if got := len(secondProvider.snapshot()); got != 0 {
		t.Fatalf("invalid dialog identities reached provider %d time(s)", got)
	}

	invalidDefaults, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dialog", Span{}, String("bad defaults"), String("not a hash"), callback)
	if err == nil || !invalidDefaults.IsNull() {
		t.Fatalf("dialog scalar defaults = (%s, %v), want null type error", invalidDefaults.Describe(), err)
	}
	invalidCallback, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dialog", Span{}, String("bad callback"), HashValue(NewHash()), String("not callable"))
	if err == nil || !invalidCallback.IsNull() || !errors.Is(err, ErrInvalidCallable) {
		t.Fatalf("dialog scalar callback = (%s, %v), want null ErrInvalidCallable", invalidCallback.Describe(), err)
	}

	comboboxDialog := create("combobox validation")
	badCombobox, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "drow_combobox", Span{}, comboboxDialog, String("combo"), String("Combo"), String("not an array"))
	if err == nil || !badCombobox.IsNull() {
		t.Fatalf("drow_combobox scalar options = (%s, %v), want null type error", badCombobox.Describe(), err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "drow_combobox", Span{}, comboboxDialog, String("combo"), String("Combo"), ArrayValue(NewArray(String("valid")))); err != nil {
		t.Fatalf("valid drow_combobox after validation failure: %v", err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), firstRuntime, firstOwner, "dialog_show", Span{}, comboboxDialog); err != nil {
		t.Fatalf("dialog_show after non-consuming validation failure: %v", err)
	}
}

func TestAggressorPromptCompletePresentationsAndCallbackABIs(t *testing.T) {
	t.Parallel()

	provider := &aggressorUITestPromptProvider{}
	var hostCalls atomic.Int32
	var callbackMu sync.Mutex
	callbackArguments := make([][]Value, 5)
	callbackContexts := make([]context.Context, 5)
	injectedExecutionTokens := make([]*scriptExecutionToken, 5)
	callbackResults := make([]Value, 5)
	callbacks := make([]Value, 5)
	for index := range callbacks {
		index := index
		callbackResults[index] = ArrayValue(NewArray(String("callback-result"), Int(int32(index))))
		callbacks[index] = FunctionValue(aggressorUITestCallable(func(ctx context.Context, values ...Value) (Value, error) {
			callbackMu.Lock()
			callbackContexts[index] = ctx
			callbackArguments[index] = append([]Value(nil), values...)
			callbackMu.Unlock()
			return callbackResults[index], nil
		}))
	}
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed prompt route reached Host")
		})),
		WithAggressorPromptProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)

	textDefault := ObjectValue(&struct{ name string }{"text-default"})
	directoryDefault := ArrayValue(NewArray(String("directory-default")))
	directoryMultiple := ArrayValue(NewArray(String("truthy-multiple")))
	fileDefault := HashValue(NewHash())
	fileMultiple := String("0")
	saveDefault := ObjectValue(&struct{ name string }{"save-default"})
	type promptExpectation struct {
		name          string
		kind          AggressorPromptKind
		values        []Value
		text          string
		title         string
		defaultValue  Value
		hasDefault    bool
		multipleValue Value
		hasMultiple   bool
		allowMultiple bool
		acceptValue   Value
	}
	prompts := []promptExpectation{
		{name: "prompt_confirm", kind: AggressorPromptConfirm, values: []Value{String("Continue?"), String("Confirm"), callbacks[0]}},
		{name: "prompt_text", kind: AggressorPromptText, values: []Value{String("Enter text"), textDefault, callbacks[1]}, text: "Enter text", defaultValue: textDefault, hasDefault: true, acceptValue: ObjectValue(&struct{ name string }{"text-answer"})},
		{name: "prompt_directory_open", kind: AggressorPromptDirectoryOpen, values: []Value{String("Choose directory"), directoryDefault, directoryMultiple, callbacks[2]}, title: "Choose directory", defaultValue: directoryDefault, hasDefault: true, multipleValue: directoryMultiple, hasMultiple: true, allowMultiple: true, acceptValue: ArrayValue(NewArray(String("/one"), String("/two")))},
		{name: "prompt_file_open", kind: AggressorPromptFileOpen, values: []Value{String("Choose file"), fileDefault, fileMultiple, callbacks[3]}, title: "Choose file", defaultValue: fileDefault, hasDefault: true, multipleValue: fileMultiple, hasMultiple: true, allowMultiple: false, acceptValue: HashValue(NewHash())},
		{name: "prompt_file_save", kind: AggressorPromptFileSave, values: []Value{saveDefault, callbacks[4]}, defaultValue: saveDefault, hasDefault: true, acceptValue: ObjectValue(&struct{ name string }{"save-answer"})},
	}
	prompts[0].text = "Continue?"
	prompts[0].title = "Confirm"
	spans := make([]Span, len(prompts))
	for index, prompt := range prompts {
		spans[index] = Span{Source: "all-prompts.cna", Start: Position{Line: 4 + index, Column: 3}}
		result, promptErr := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, prompt.name, spans[index], prompt.values...)
		if promptErr != nil || !result.IsNull() {
			t.Fatalf("%s = (%s, %v), want null/nil", prompt.name, result.Describe(), promptErr)
		}
	}
	calls := provider.snapshot()
	if len(calls) != len(prompts) {
		t.Fatalf("prompt provider calls = %d, want %d", len(calls), len(prompts))
	}
	seenIDs := make(map[AggressorPromptID]struct{}, len(calls))
	for index, call := range calls {
		want := prompts[index]
		got := call.presentation
		if got.ID == 0 {
			t.Fatalf("prompt[%d] has zero ID", index)
		}
		if _, duplicate := seenIDs[got.ID]; duplicate {
			t.Fatalf("prompt[%d] repeats ID %d", index, got.ID)
		}
		seenIDs[got.ID] = struct{}{}
		if got.Kind != want.kind || got.Name != want.name || got.RuntimeID != runtimeInstance.ID() ||
			got.Script != owner.ID() || got.Span != spans[index] || got.Text != want.text || got.Title != want.title ||
			got.HasDefault != want.hasDefault || got.HasMultiple != want.hasMultiple || got.AllowMultiple != want.allowMultiple {
			t.Fatalf("prompt[%d] presentation = %#v, want %#v", index, got, want)
		}
		if want.hasDefault {
			if !got.Default.IdentityEqual(want.defaultValue) {
				t.Fatalf("prompt[%d] default = %s, want identical %s", index, got.Default.Describe(), want.defaultValue.Describe())
			}
		} else if !got.Default.IsNull() {
			t.Fatalf("prompt[%d] absent default = %s, want null", index, got.Default.Describe())
		}
		if want.hasMultiple {
			if !got.Multiple.IdentityEqual(want.multipleValue) {
				t.Fatalf("prompt[%d] multiple = %s, want identical %s", index, got.Multiple.Describe(), want.multipleValue.Describe())
			}
		} else if !got.Multiple.IsNull() {
			t.Fatalf("prompt[%d] absent multiple = %s, want null", index, got.Multiple.Describe())
		}
		assertAggressorUITestDoneOpen(t, call.responder.Done())
		injectedExecutionTokens[index] = &scriptExecutionToken{script: owner}
		injectedExecutionTokens[index].active.Store(true)
		acceptContext := context.WithValue(context.Background(), aggressorUITestContextKey{}, index)
		acceptContext = context.WithValue(acceptContext, scriptExecutionContextKey{}, injectedExecutionTokens[index])
		var acceptResult Value
		var acceptErr error
		if want.kind == AggressorPromptConfirm {
			acceptResult, acceptErr = call.responder.Accept(acceptContext)
		} else {
			acceptResult, acceptErr = call.responder.Accept(acceptContext, want.acceptValue)
		}
		if acceptErr != nil || !acceptResult.IdentityEqual(callbackResults[index]) {
			t.Fatalf("prompt[%d] Accept = (%s, %v), want identical callback result %s", index, acceptResult.Describe(), acceptErr, callbackResults[index].Describe())
		}
		assertAggressorUITestDoneClosed(t, call.responder.Done())
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("typed prompt route reached Host %d time(s)", hostCalls.Load())
	}
	callbackMu.Lock()
	defer callbackMu.Unlock()
	for index, prompt := range prompts {
		ctx := callbackContexts[index]
		if ctx == nil || ctx.Value(aggressorUITestContextKey{}) != index {
			t.Fatalf("prompt[%d] callback importer context = %#v", index, ctx)
		}
		if currentFiber(ctx) != nil || currentBindingInvocation(ctx) != nil ||
			ctx.Value(executionMeterKey{}) != nil || ctx.Value(nativeDispatchStateContextKey{}) != nil ||
			ctx.Value(runtimeExecutionContextKey{}) != nil ||
			ctx.Value(scriptUnloadContextKey{}) != nil || ctx.Value(runtimeCloseContextKey{}) != nil {
			t.Fatalf("prompt[%d] callback retained OPFOR-private evaluator or lifecycle state", index)
		}
		callbackExecutionToken, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
		if callbackExecutionToken == nil || callbackExecutionToken == injectedExecutionTokens[index] ||
			callbackExecutionToken.script != owner || callbackExecutionToken.parent != nil {
			t.Fatalf("prompt[%d] callback execution token = %#v, want fresh owner admission without injected ancestry", index, callbackExecutionToken)
		}
		arguments := callbackArguments[index]
		if prompt.kind == AggressorPromptConfirm {
			if len(arguments) != 0 {
				t.Fatalf("confirm callback arguments = %v, want zero-argument ABI", arguments)
			}
		} else if len(arguments) != 1 || !arguments[0].IdentityEqual(prompt.acceptValue) {
			t.Fatalf("prompt[%d] callback arguments = %v, want one identical Value", index, arguments)
		}
	}
}

func TestAggressorPromptValidationAndDismissDoNotConsumeOrCallback(t *testing.T) {
	t.Parallel()

	provider := &aggressorUITestPromptProvider{}
	var callbackCalls atomic.Int32
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		callbackCalls.Add(1)
		return String("accepted"), nil
	}))
	runtimeInstance, err := New(WithAggressorPromptProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("Text"), String("default"), callback); err != nil {
		t.Fatal(err)
	}
	call := provider.snapshot()[0]
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name   string
		ctx    context.Context
		values []Value
		want   error
	}{
		{name: "canceled", ctx: canceledContext, values: []Value{String("answer")}, want: context.Canceled},
		{name: "missing", ctx: context.Background()},
		{name: "extra", ctx: context.Background(), values: []Value{String("one"), String("two")}},
	} {
		result, acceptErr := call.responder.Accept(test.ctx, test.values...)
		if acceptErr == nil || !result.IsNull() {
			t.Errorf("%s Accept = (%s, %v), want null validation error", test.name, result.Describe(), acceptErr)
		}
		if test.want != nil && !errors.Is(acceptErr, test.want) {
			t.Errorf("%s Accept error = %v, want %v", test.name, acceptErr, test.want)
		}
		assertAggressorUITestDoneOpen(t, call.responder.Done())
	}
	if callbackCalls.Load() != 0 {
		t.Fatalf("invalid prompt responses invoked callback %d time(s)", callbackCalls.Load())
	}
	if err := call.responder.Dismiss(); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	assertAggressorUITestDoneClosed(t, call.responder.Done())
	if callbackCalls.Load() != 0 {
		t.Fatalf("Dismiss invoked callback %d time(s)", callbackCalls.Load())
	}
	if result, err := call.responder.Accept(context.Background(), String("late")); err == nil || !result.IsNull() || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Accept after Dismiss = (%s, %v), want null descriptive closed error", result.Describe(), err)
	}
	if err := call.responder.Dismiss(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("second Dismiss error = %v, want descriptive closed error", err)
	}

	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_confirm", Span{}, String("Confirm"), String("Title"), callback); err != nil {
		t.Fatal(err)
	}
	confirm := provider.snapshot()[1]
	if result, err := confirm.responder.Accept(context.Background(), String("unexpected")); err == nil || !result.IsNull() {
		t.Fatalf("confirm Accept with value = (%s, %v), want null arity error", result.Describe(), err)
	}
	assertAggressorUITestDoneOpen(t, confirm.responder.Done())
	if result, err := confirm.responder.Accept(context.Background()); err != nil || result.String() != "accepted" {
		t.Fatalf("valid confirm Accept = (%s, %v), want accepted/nil", result.Describe(), err)
	}
	if callbackCalls.Load() != 1 {
		t.Fatalf("callback calls after valid confirm = %d, want one", callbackCalls.Load())
	}
}

func TestAggressorUIProviderErrorsAreAuthoritativeAndCloseResponders(t *testing.T) {
	t.Parallel()

	dialogErr := errors.New("dialog provider failed")
	promptErr := errors.New("prompt provider failed")
	dialogProvider := &aggressorUITestDialogProvider{present: func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
		return dialogErr
	}}
	promptProvider := &aggressorUITestPromptProvider{present: func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
		return promptErr
	}}
	var hostCalls atomic.Int32
	var callbackCalls atomic.Int32
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		callbackCalls.Add(1)
		return Null(), nil
	}))
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host fallback"), nil
		})),
		WithAggressorDialogProvider(dialogProvider),
		WithAggressorPromptProvider(promptProvider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)

	dialogValue, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog", Span{}, String("failure"), HashValue(NewHash()), callback)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dbutton_action", Span{}, dialogValue, String("Run")); err != nil {
		t.Fatal(err)
	}
	result, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue)
	if !errors.Is(err, dialogErr) || !result.IsNull() {
		t.Fatalf("dialog_show provider error = (%s, %v), want null/%v", result.Describe(), err, dialogErr)
	}
	dialogCall := dialogProvider.snapshot()[0]
	assertAggressorUITestDoneClosed(t, dialogCall.responder.Done())
	if result, err := dialogCall.responder.Activate(context.Background(), dialogCall.presentation.Buttons[0].ID); err == nil || !result.IsNull() || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Activate after provider failure = (%s, %v), want null closed error", result.Describe(), err)
	}

	result, err = aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("failure"), String("default"), callback)
	if !errors.Is(err, promptErr) || !result.IsNull() {
		t.Fatalf("prompt_text provider error = (%s, %v), want null/%v", result.Describe(), err, promptErr)
	}
	promptCall := promptProvider.snapshot()[0]
	assertAggressorUITestDoneClosed(t, promptCall.responder.Done())
	if result, err := promptCall.responder.Accept(context.Background(), String("late")); err == nil || !result.IsNull() || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Accept after provider failure = (%s, %v), want null closed error", result.Describe(), err)
	}
	if hostCalls.Load() != 0 || callbackCalls.Load() != 0 {
		t.Fatalf("provider failure reached Host/callback = %d/%d", hostCalls.Load(), callbackCalls.Load())
	}
}

func TestAggressorUIUnsetProvidersPreserveHostForEveryDocumentedFunction(t *testing.T) {
	t.Parallel()

	allArities := make(map[string][2]int, len(aggressorUITestDialogArities)+len(aggressorUITestPromptArities))
	for name, arity := range aggressorUITestDialogArities {
		allArities[name] = arity
	}
	for name, arity := range aggressorUITestPromptArities {
		allArities[name] = arity
	}
	names := make([]string, 0, len(allArities))
	for name := range allArities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			wantErr := errors.New("Host result for " + name)
			wantResult := ArrayValue(NewArray(String(name), ObjectValue(&struct{ name string }{name})))
			var hostCalls atomic.Int32
			var captured Invocation
			runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
				hostCalls.Add(1)
				captured = invocation
				return wantResult, wantErr
			})))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

			count := allArities[name][0]
			arguments := make([]Value, count)
			for index := range arguments {
				// These are deliberately not massaged into typed-provider shapes:
				// callbacks are not callable, dialog handles are arbitrary, and the
				// combobox options argument is not an array. Host compatibility must
				// see the invocation before any typed validation.
				arguments[index] = ObjectValue(&struct {
					name  string
					index int
				}{name: name, index: index})
			}
			result, invokeErr := runtimeInstance.Invoke(context.Background(), name, arguments...)
			if !errors.Is(invokeErr, wantErr) || !result.IdentityEqual(wantResult) {
				t.Fatalf("Host fallback = (%s, %v), want identical %s/%v", result.Describe(), invokeErr, wantResult.Describe(), wantErr)
			}
			if hostCalls.Load() != 1 || captured.Name != name || captured.Runtime != runtimeInstance || captured.Script != 0 || captured.Span != (Span{}) || len(captured.Arguments) != len(arguments) {
				t.Fatalf("Host invocation/calls = %#v/%d", captured, hostCalls.Load())
			}
			for index, value := range captured.Values() {
				if !value.IdentityEqual(arguments[index]) {
					t.Fatalf("Host argument[%d] = %s, want identical %s", index, value.Describe(), arguments[index].Describe())
				}
			}
		})
	}
}

func TestAggressorUIHostFallbackPreservesRawInvocationAndNativeBoundaryError(t *testing.T) {
	t.Parallel()

	wantResult := HashValue(NewHash())
	firstCell := NewCell(String("before"))
	callbackCell := NewCell(String("not-callable-and-still-Host-owned"))
	span := Span{Source: "raw-host-ui.cna", Start: Position{Line: 9, Column: 4}}
	arguments := []Argument{
		{Name: "$text", Reference: firstCell},
		{Value: ObjectValue(&struct{ name string }{"default"})},
		{Name: "&callback", Reference: callbackCell},
	}
	var captured Invocation
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		captured = invocation
		if len(invocation.Arguments) != 3 || &invocation.Arguments[0] != &arguments[0] {
			return Null(), errors.New("wrapper replaced the raw Argument slice")
		}
		if !invocation.Arguments[0].Set(String("mutated-by-Host")) {
			return Null(), errors.New("Host lost the pass-by-name reference")
		}
		return wantResult, ErrUnsafeArrayView
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original := Invocation{Runtime: runtimeInstance, Script: 77, Name: "prompt_text", Span: span, Arguments: arguments}
	result, err := runtimeInstance.invoke(context.Background(), original)
	if !errors.Is(err, ErrUnsafeArrayView) || !result.IdentityEqual(wantResult) {
		t.Fatalf("raw Host fallback = (%s, %v), want identical partial result/ErrUnsafeArrayView", result.Describe(), err)
	}
	if captured.Runtime != runtimeInstance || captured.Script != 77 || captured.Name != "prompt_text" || captured.Span != span ||
		len(captured.Arguments) != len(arguments) || &captured.Arguments[0] != &arguments[0] {
		t.Fatalf("raw Host invocation changed = %#v", captured)
	}
	if got := firstCell.Get().String(); got != "mutated-by-Host" {
		t.Fatalf("Host reference mutation = %q, want mutated-by-Host", got)
	}
	if got := callbackCell.Get().String(); got != "not-callable-and-still-Host-owned" {
		t.Fatalf("Host callback-shaped reference = %q, want unchanged raw value", got)
	}
}

func TestAggressorUIProviderAndCallbackBoundaryErrorsBypassNativeTranslation(t *testing.T) {
	for _, boundaryErr := range []error{ErrUnsafeArrayView, ErrReadOnlyArray, ErrReadOnlyHash} {
		boundaryErr := boundaryErr
		for _, family := range []string{"dialog", "prompt"} {
			family := family
			t.Run(family+"/"+boundaryErr.Error(), func(t *testing.T) {
				var hostCalls atomic.Int32
				dialogProvider := &aggressorUITestDialogProvider{present: func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
					return boundaryErr
				}}
				promptProvider := &aggressorUITestPromptProvider{present: func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
					return boundaryErr
				}}
				runtimeInstance, err := New(
					WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
						hostCalls.Add(1)
						return Null(), nil
					})),
					WithAggressorDialogProvider(dialogProvider),
					WithAggressorPromptProvider(promptProvider),
					WithFunction("ordinary_ui_boundary_control", func(context.Context, Invocation) (Value, error) {
						return String("discarded"), ErrUnsafeArrayView
					}),
				)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
				owner := aggressorUITestOwner(t, runtimeInstance)
				callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) { return Null(), nil }))

				var result Value
				if family == "dialog" {
					dialogValue, createErr := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog", Span{}, String("boundary"), HashValue(NewHash()), callback)
					if createErr != nil {
						t.Fatal(createErr)
					}
					result, err = aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue)
				} else {
					result, err = aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("boundary"), String("default"), callback)
				}
				if !errors.Is(err, boundaryErr) || !result.IsNull() {
					t.Fatalf("provider boundary = (%s, %v), want null/%v", result.Describe(), err, boundaryErr)
				}
				if hostCalls.Load() != 0 {
					t.Fatalf("provider boundary reached Host %d time(s)", hostCalls.Load())
				}
				ordinary, ordinaryErr := runtimeInstance.Invoke(context.Background(), "ordinary_ui_boundary_control")
				if ordinaryErr != nil || !ordinary.IsNull() {
					t.Fatalf("provider boundary marker contaminated later ordinary native = (%s, %v)", ordinary.Describe(), ordinaryErr)
				}
			})
		}
	}

	callbackResult := ObjectValue(&struct{ name string }{"callback-partial"})
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		return callbackResult, ErrUnsafeArrayView
	}))
	provider := &aggressorUITestPromptProvider{}
	runtimeInstance, err := New(WithAggressorPromptProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("callback"), String("default"), callback); err != nil {
		t.Fatal(err)
	}
	result, err := provider.snapshot()[0].responder.Accept(context.Background(), String("answer"))
	if !errors.Is(err, ErrUnsafeArrayView) || !result.IdentityEqual(callbackResult) {
		t.Fatalf("callback boundary = (%s, %v), want identical partial result/ErrUnsafeArrayView", result.Describe(), err)
	}
}

func TestAggressorUIWithFunctionOverridesEveryNameInBothOptionOrders(t *testing.T) {
	allNames := make([]string, 0, len(aggressorUITestDialogArities)+len(aggressorUITestPromptArities))
	for name := range aggressorUITestDialogArities {
		allNames = append(allNames, name)
	}
	for name := range aggressorUITestPromptArities {
		allNames = append(allNames, name)
	}
	sort.Strings(allNames)
	for _, name := range allNames {
		name := name
		for _, overrideFirst := range []bool{false, true} {
			order := "override-last"
			if overrideFirst {
				order = "override-first"
			}
			t.Run(name+"/"+order, func(t *testing.T) {
				var hostCalls atomic.Int32
				var dialogCalls atomic.Int32
				var promptCalls atomic.Int32
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				dialogOption := WithAggressorDialogProvider(AggressorDialogProviderFunc(func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
					dialogCalls.Add(1)
					return nil
				}))
				promptOption := WithAggressorPromptProvider(AggressorPromptProviderFunc(func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
					promptCalls.Add(1)
					return nil
				}))
				overrideOption := WithFunction(name, func(_ context.Context, invocation Invocation) (Value, error) {
					if invocation.Name != name || len(invocation.Arguments) != 0 {
						return Null(), errors.New("override received altered invocation")
					}
					return String("override:" + name), nil
				})
				options := []Option{hostOption, dialogOption, promptOption, overrideOption}
				if overrideFirst {
					options = []Option{hostOption, overrideOption, dialogOption, promptOption}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
				// Zero arguments are invalid for every stock wrapper. Success proves
				// resolution selected the importer override before stock validation.
				result, err := runtimeInstance.Invoke(context.Background(), name)
				if err != nil || result.String() != "override:"+name || hostCalls.Load() != 0 || dialogCalls.Load() != 0 || promptCalls.Load() != 0 {
					t.Fatalf("override = (%s, %v), Host/dialog/prompt %d/%d/%d", result.Describe(), err, hostCalls.Load(), dialogCalls.Load(), promptCalls.Load())
				}
			})
		}
	}
}

func TestAggressorUIPartialDialogOverrideCannotForgeNativeHandle(t *testing.T) {
	t.Parallel()

	foreign := ObjectValue(&struct{ name string }{"foreign-dialog"})
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorDialogProvider(AggressorDialogProviderFunc(func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
			providerCalls.Add(1)
			return nil
		})),
		WithFunction("dialog", func(context.Context, Invocation) (Value, error) { return foreign, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Invoke(context.Background(), "dialog")
	if err != nil || !value.IdentityEqual(foreign) {
		t.Fatalf("dialog override = (%s, %v), want foreign handle", value.Describe(), err)
	}
	result, err := runtimeInstance.Invoke(context.Background(), "dialog_show", value)
	if err == nil || !result.IsNull() || !strings.Contains(err.Error(), "not an Aggressor dialog") {
		t.Fatalf("dialog_show foreign override handle = (%s, %v), want null type error", result.Describe(), err)
	}
	if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("foreign native handle reached provider/Host = %d/%d", providerCalls.Load(), hostCalls.Load())
	}
}

type typedNilAggressorDialogProvider struct{}

func (*typedNilAggressorDialogProvider) PresentAggressorDialog(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
	panic("typed-nil Aggressor dialog provider was invoked")
}

type typedNilAggressorPromptProvider struct{}

func (*typedNilAggressorPromptProvider) PresentAggressorPrompt(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
	panic("typed-nil Aggressor prompt provider was invoked")
}

func TestAggressorUIRejectsTypedNilProvidersAndNilFunctionAdapters(t *testing.T) {
	t.Parallel()

	var dialogPointer *typedNilAggressorDialogProvider
	var promptPointer *typedNilAggressorPromptProvider
	var dialogFunction AggressorDialogProviderFunc
	var promptFunction AggressorPromptProviderFunc
	for _, test := range []struct {
		name   string
		option Option
		want   string
	}{
		{name: "dialog pointer", option: WithAggressorDialogProvider(dialogPointer), want: "Aggressor dialog provider is nil"},
		{name: "dialog function", option: WithAggressorDialogProvider(dialogFunction), want: "Aggressor dialog provider is nil"},
		{name: "prompt pointer", option: WithAggressorPromptProvider(promptPointer), want: "Aggressor prompt provider is nil"},
		{name: "prompt function", option: WithAggressorPromptProvider(promptFunction), want: "Aggressor prompt provider is nil"},
	} {
		if _, err := New(test.option); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s New error = %v, want %q", test.name, err, test.want)
		}
	}
	if err := dialogFunction.PresentAggressorDialog(context.Background(), AggressorDialogPresentation{}, nil); err == nil || !strings.Contains(err.Error(), "Aggressor dialog provider is nil") {
		t.Fatalf("nil dialog function adapter error = %v", err)
	}
	if err := promptFunction.PresentAggressorPrompt(context.Background(), AggressorPromptPresentation{}, nil); err == nil || !strings.Contains(err.Error(), "Aggressor prompt provider is nil") {
		t.Fatalf("nil prompt function adapter error = %v", err)
	}
}

func TestAggressorUIProviderCallsObserveScriptUnloadCancellationAndRevokeResponder(t *testing.T) {
	for _, family := range []string{"dialog", "prompt"} {
		family := family
		t.Run(family, func(t *testing.T) {
			entered := make(chan struct{})
			canceled := make(chan struct{})
			release := make(chan struct{})
			var enteredOnce sync.Once
			var canceledOnce sync.Once
			var callbackCalls atomic.Int32
			callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
				callbackCalls.Add(1)
				return Null(), nil
			}))
			dialogProvider := &aggressorUITestDialogProvider{}
			promptProvider := &aggressorUITestPromptProvider{}
			block := func(ctx context.Context) error {
				enteredOnce.Do(func() { close(entered) })
				<-ctx.Done()
				canceledOnce.Do(func() { close(canceled) })
				<-release
				return ctx.Err()
			}
			dialogProvider.present = func(ctx context.Context, _ AggressorDialogPresentation, _ AggressorDialogResponder) error {
				return block(ctx)
			}
			promptProvider.present = func(ctx context.Context, _ AggressorPromptPresentation, _ AggressorPromptResponder) error {
				return block(ctx)
			}
			runtimeInstance, err := New(
				WithInitialGlobals(map[string]Value{"ui_callback": callback}),
				WithAggressorDialogProvider(dialogProvider),
				WithAggressorPromptProvider(promptProvider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			source := `sub run_ui { return prompt_text("blocked prompt", "default", $ui_callback); }`
			if family == "dialog" {
				source = `
sub run_ui {
    $dialog = dialog("blocked dialog", %(), $ui_callback);
    dbutton_action($dialog, "Run");
    return dialog_show($dialog);
}
`
			}
			program, err := CompileString("blocked-"+family+"-provider.cna", source)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := runtimeInstance.Load(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			callResult := make(chan error, 1)
			go func() {
				_, callErr := owner.Call(context.Background(), "run_ui")
				callResult <- callErr
			}()
			awaitQuiescenceSignal(t, entered, family+" provider entry")

			unloadResult := make(chan error, 1)
			go func() { unloadResult <- owner.Unload(context.Background()) }()
			awaitQuiescenceSignal(t, canceled, family+" provider cancellation")
			assertQuiescencePending(t, unloadResult, family+" provider Unload")

			if family == "dialog" {
				calls := dialogProvider.snapshot()
				if len(calls) != 1 {
					t.Fatalf("dialog provider calls = %d, want one", len(calls))
				}
				assertAggressorUITestDoneClosed(t, calls[0].responder.Done())
				result, responseErr := calls[0].responder.Activate(context.Background(), calls[0].presentation.Buttons[0].ID)
				if !errors.Is(responseErr, ErrScriptUnloaded) || !result.IsNull() {
					t.Fatalf("Activate after unload admission = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), responseErr)
				}
			} else {
				calls := promptProvider.snapshot()
				if len(calls) != 1 {
					t.Fatalf("prompt provider calls = %d, want one", len(calls))
				}
				assertAggressorUITestDoneClosed(t, calls[0].responder.Done())
				result, responseErr := calls[0].responder.Accept(context.Background(), String("late"))
				if !errors.Is(responseErr, ErrScriptUnloaded) || !result.IsNull() {
					t.Fatalf("Accept after unload admission = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), responseErr)
				}
			}
			close(release)
			if err := awaitQuiescenceError(t, callResult, family+" provider call"); !errors.Is(err, context.Canceled) {
				t.Fatalf("script call error = %v, want context.Canceled", err)
			}
			if err := awaitQuiescenceError(t, unloadResult, family+" provider Unload"); err != nil {
				t.Fatalf("Unload: %v", err)
			}
			if callbackCalls.Load() != 0 {
				t.Fatalf("unload-canceled provider invoked callback %d time(s)", callbackCalls.Load())
			}
		})
	}
}

func TestAggressorUIRuntimeCloseRevokesUnusedDialogAndPromptResponders(t *testing.T) {
	t.Parallel()

	dialogProvider := &aggressorUITestDialogProvider{}
	promptProvider := &aggressorUITestPromptProvider{}
	var callbackCalls atomic.Int32
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		callbackCalls.Add(1)
		return Null(), nil
	}))
	runtimeInstance, err := New(
		WithAggressorDialogProvider(dialogProvider),
		WithAggressorPromptProvider(promptProvider),
	)
	if err != nil {
		t.Fatal(err)
	}
	owner := aggressorUITestOwner(t, runtimeInstance)
	dialogValue, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog", Span{}, String("close"), HashValue(NewHash()), callback)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dbutton_action", Span{}, dialogValue, String("Run")); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue); err != nil {
		t.Fatal(err)
	}
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("close"), String("default"), callback); err != nil {
		t.Fatal(err)
	}
	dialogCall := dialogProvider.snapshot()[0]
	promptCall := promptProvider.snapshot()[0]
	assertAggressorUITestDoneOpen(t, dialogCall.responder.Done())
	assertAggressorUITestDoneOpen(t, promptCall.responder.Done())
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertAggressorUITestDoneClosed(t, dialogCall.responder.Done())
	assertAggressorUITestDoneClosed(t, promptCall.responder.Done())
	if result, err := dialogCall.responder.Activate(context.Background(), dialogCall.presentation.Buttons[0].ID); !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("Activate after Close = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
	if result, err := promptCall.responder.Accept(context.Background(), String("late")); !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("Accept after Close = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
	if callbackCalls.Load() != 0 {
		t.Fatalf("Close invoked callback %d time(s)", callbackCalls.Load())
	}
}

func TestAggressorPromptCallbackExecutionIsCanceledAndAwaitedByUnload(t *testing.T) {
	provider := &aggressorUITestPromptProvider{}
	callbackEntered := make(chan struct{})
	callbackCanceled := make(chan struct{})
	releaseCallback := make(chan struct{})
	callback := FunctionValue(aggressorUITestCallable(func(ctx context.Context, _ ...Value) (Value, error) {
		close(callbackEntered)
		<-ctx.Done()
		close(callbackCanceled)
		<-releaseCallback
		return Null(), ctx.Err()
	}))
	runtimeInstance, err := New(WithAggressorPromptProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("callback"), String("default"), callback); err != nil {
		t.Fatal(err)
	}
	call := provider.snapshot()[0]
	acceptResult := make(chan error, 1)
	go func() {
		_, acceptErr := call.responder.Accept(context.Background(), String("answer"))
		acceptResult <- acceptErr
	}()
	awaitQuiescenceSignal(t, callbackEntered, "prompt callback entry")
	assertAggressorUITestDoneClosed(t, call.responder.Done())

	unloadResult := make(chan error, 1)
	go func() { unloadResult <- owner.Unload(context.Background()) }()
	awaitQuiescenceSignal(t, callbackCanceled, "prompt callback cancellation")
	assertQuiescencePending(t, unloadResult, "callback-owning script Unload")
	if result, err := call.responder.Accept(context.Background(), String("late")); err == nil || !result.IsNull() || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("second Accept during callback = (%s, %v), want null closed error", result.Describe(), err)
	}
	close(releaseCallback)
	if err := awaitQuiescenceError(t, acceptResult, "prompt Accept callback"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Accept error = %v, want context.Canceled", err)
	}
	if err := awaitQuiescenceError(t, unloadResult, "callback-owning script Unload"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
}

func TestAggressorPromptSynchronousProviderCallbacksShareInstructionMeter(t *testing.T) {
	const instructionLimit = 100
	var providerCalls atomic.Int32
	provider := AggressorPromptProviderFunc(func(ctx context.Context, presentation AggressorPromptPresentation, responder AggressorPromptResponder) error {
		providerCalls.Add(1)
		if presentation.Kind != AggressorPromptConfirm {
			return errors.New("recursive meter test received non-confirm prompt")
		}
		_, err := responder.Accept(ctx)
		return err
	})
	runtimeInstance, err := New(
		WithInstructionLimit(instructionLimit),
		WithAggressorPromptProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("synchronous-prompt-limit.cna", `
$calls = 0;
$callback = {
    $calls++;
    if ($calls < 512) {
        prompt_confirm("Continue?", "Instruction limit", $callback);
    }
};
prompt_confirm("Continue?", "Instruction limit", $callback);
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), quiescenceTestTimeout)
	defer cancel()
	_, err = runtimeInstance.Execute(ctx, program)
	if !errors.Is(err, ErrInstructionLimit) {
		t.Fatalf("nested synchronous prompt error = %v, want ErrInstructionLimit", err)
	}
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Resource != "instruction" || limit.Limit != instructionLimit {
		t.Fatalf("nested synchronous prompt LimitError = %+v", limit)
	}
	if calls := providerCalls.Load(); calls < 2 || calls >= 512 {
		t.Fatalf("synchronous prompt provider calls = %d, want recursive but instruction-bounded", calls)
	}
}

func TestAggressorPromptSynchronousCallbackCanReenterScriptUnload(t *testing.T) {
	const instructionLimit = 1000
	cleanupErr := errors.New("synchronous prompt unload cleanup")
	var owner *Script
	var callbackContext context.Context
	var cleanupCalls atomic.Int32
	cleanupContextErr := make(chan error, 1)
	reentrantUnload := make(chan error, 1)
	callback := FunctionValue(aggressorUITestCallable(func(ctx context.Context, values ...Value) (Value, error) {
		if len(values) != 0 {
			return Null(), errors.New("confirm callback received arguments")
		}
		callbackContext = ctx
		if ctx.Value(executionMeterKey{}) == nil {
			return Null(), errors.New("synchronous callback did not inherit presentation instruction meter")
		}
		err := owner.Unload(ctx)
		reentrantUnload <- err
		return Null(), err
	}))
	var responder AggressorPromptResponder
	provider := AggressorPromptProviderFunc(func(ctx context.Context, presentation AggressorPromptPresentation, candidate AggressorPromptResponder) error {
		if presentation.Kind != AggressorPromptConfirm {
			return errors.New("reentrant Unload test received non-confirm prompt")
		}
		responder = candidate
		_, err := candidate.Accept(ctx)
		return err
	})
	runtimeInstance, err := New(
		WithInstructionLimit(instructionLimit),
		WithInitialGlobals(map[string]Value{"ui_callback": callback}),
		WithAggressorPromptProvider(provider),
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{Unloaded: func(ctx context.Context, _ *Script) error {
			cleanupCalls.Add(1)
			cleanupContextErr <- ctx.Err()
			return cleanupErr
		}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("synchronous-prompt-unload.cna", `
sub run_ui {
    prompt_confirm("Unload?", "Reentrant unload", $ui_callback);
    return 99;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	callResult := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, callErr := owner.Call(ctx, "run_ui")
		callResult <- callErr
	}()
	if err := awaitQuiescenceError(t, reentrantUnload, "synchronous callback reentrant Unload"); err != nil {
		t.Fatalf("reentrant Unload: %v", err)
	}
	if err := awaitQuiescenceError(t, callResult, "script call after reentrant Unload"); !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
		t.Fatalf("script call error = %v, want context.Canceled and cleanup sentinel", err)
	}
	if cleanupCalls.Load() != 1 {
		t.Fatalf("ScriptUnloaded calls = %d, want one", cleanupCalls.Load())
	}
	if err := <-cleanupContextErr; err != nil {
		t.Fatalf("ScriptUnloaded context error = %v, want nil", err)
	}
	if owner.Active() {
		t.Fatal("script remained active after synchronous callback Unload")
	}
	if responder == nil {
		t.Fatal("synchronous provider did not retain responder for observation")
	}
	assertAggressorUITestDoneClosed(t, responder.Done())
	if callbackContext == nil {
		t.Fatal("synchronous callback context was not captured")
	}
	ancestry, _ := callbackContext.Value(aggressorUICallbackAncestryContextKey{}).(*aggressorUICallbackAncestry)
	if ancestry == nil || ancestry.active.Load() {
		t.Fatalf("callback ancestry after return = %#v, want present but inactive", ancestry)
	}
	if callbackContext.Value(executionMeterKey{}) != nil {
		t.Fatal("synchronous callback retained presentation instruction meter after return")
	}
	executionToken, _ := callbackContext.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	if executionToken == nil || executionToken.active.Load() {
		t.Fatalf("callback execution token after return = %#v, want present but inactive", executionToken)
	}
}

func TestAggressorPromptSynchronousCallbackCanReenterRuntimeClose(t *testing.T) {
	cleanupErr := errors.New("synchronous prompt close cleanup")
	var runtimeInstance *Runtime
	var owner *Script
	var cleanupCalls atomic.Int32
	cleanupContextErr := make(chan error, 1)
	reentrantClose := make(chan error, 1)
	callback := FunctionValue(aggressorUITestCallable(func(ctx context.Context, values ...Value) (Value, error) {
		if len(values) != 0 {
			return Null(), errors.New("confirm callback received arguments")
		}
		err := runtimeInstance.Close(ctx)
		reentrantClose <- err
		return Null(), err
	}))
	var responder AggressorPromptResponder
	provider := AggressorPromptProviderFunc(func(ctx context.Context, presentation AggressorPromptPresentation, candidate AggressorPromptResponder) error {
		if presentation.Kind != AggressorPromptConfirm {
			return errors.New("reentrant Close test received non-confirm prompt")
		}
		responder = candidate
		_, err := candidate.Accept(ctx)
		return err
	})
	created, err := New(
		WithInitialGlobals(map[string]Value{"ui_callback": callback}),
		WithAggressorPromptProvider(provider),
		WithScriptLifecycleObserver(ScriptLifecycleFuncs{Unloaded: func(ctx context.Context, _ *Script) error {
			cleanupCalls.Add(1)
			cleanupContextErr <- ctx.Err()
			return cleanupErr
		}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInstance = created
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("synchronous-prompt-close.cna", `
sub run_ui {
    prompt_confirm("Close?", "Reentrant close", $ui_callback);
    return 99;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	callResult := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, callErr := owner.Call(ctx, "run_ui")
		callResult <- callErr
	}()
	if err := awaitQuiescenceError(t, reentrantClose, "synchronous callback reentrant Close"); err != nil {
		t.Fatalf("reentrant Close: %v", err)
	}
	if err := awaitQuiescenceError(t, callResult, "script call after reentrant Close"); !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
		t.Fatalf("script call error = %v, want context.Canceled and cleanup sentinel", err)
	}
	if cleanupCalls.Load() != 1 {
		t.Fatalf("ScriptUnloaded calls = %d, want one", cleanupCalls.Load())
	}
	if err := <-cleanupContextErr; err != nil {
		t.Fatalf("ScriptUnloaded context error = %v, want nil", err)
	}
	if owner.Active() {
		t.Fatal("script remained active after synchronous callback Runtime.Close")
	}
	if responder == nil {
		t.Fatal("synchronous provider did not retain responder for observation")
	}
	assertAggressorUITestDoneClosed(t, responder.Done())
	if _, err := runtimeInstance.Invoke(context.Background(), "println", String("too late")); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Invoke after reentrant Close error = %v, want ErrRuntimeClosed", err)
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatalf("waiting Close: %v", err)
	}
}

func TestAggressorUICanceledInvocationStopsBeforeProviderOrHost(t *testing.T) {
	t.Parallel()

	var dialogCalls atomic.Int32
	var promptCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorDialogProvider(AggressorDialogProviderFunc(func(context.Context, AggressorDialogPresentation, AggressorDialogResponder) error {
			dialogCalls.Add(1)
			return nil
		})),
		WithAggressorPromptProvider(AggressorPromptProviderFunc(func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
			promptCalls.Add(1)
			return nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) { return Null(), nil }))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name   string
		values []Value
	}{
		{name: "dialog", values: []Value{String("canceled"), HashValue(NewHash()), callback}},
		{name: "prompt_text", values: []Value{String("canceled"), String("default"), callback}},
	} {
		result, invokeErr := aggressorUITestInvoke(canceled, runtimeInstance, owner, test.name, Span{}, test.values...)
		if !errors.Is(invokeErr, context.Canceled) || !result.IsNull() {
			t.Errorf("%s canceled invocation = (%s, %v), want null/context.Canceled", test.name, result.Describe(), invokeErr)
		}
	}
	if dialogCalls.Load() != 0 || promptCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("canceled invocation reached dialog/prompt/Host = %d/%d/%d", dialogCalls.Load(), promptCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorPromptConcurrentPresentationAllocatesUniqueIDsAndCallbacks(t *testing.T) {
	provider := &aggressorUITestPromptProvider{}
	var callbackCalls atomic.Int32
	callback := FunctionValue(aggressorUITestCallable(func(_ context.Context, values ...Value) (Value, error) {
		callbackCalls.Add(1)
		if len(values) != 1 {
			return Null(), errors.New("concurrent prompt callback ABI changed")
		}
		return values[0], nil
	}))
	runtimeInstance, err := New(WithAggressorPromptProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	owner := aggressorUITestOwner(t, runtimeInstance)

	const count = 64
	start := make(chan struct{})
	invocationErrors := make([]error, count)
	var invocationWait sync.WaitGroup
	invocationWait.Add(count)
	for index := 0; index < count; index++ {
		index := index
		go func() {
			defer invocationWait.Done()
			<-start
			result, invokeErr := aggressorUITestInvoke(
				context.Background(), runtimeInstance, owner, "prompt_text",
				Span{Source: "concurrent-prompts.cna", Start: Position{Line: index + 1, Column: 1}},
				String("prompt"), Int(int32(index)), callback,
			)
			if invokeErr == nil && !result.IsNull() {
				invokeErr = errors.New("prompt_text returned non-null")
			}
			invocationErrors[index] = invokeErr
		}()
	}
	close(start)
	invocationWait.Wait()
	for index, invokeErr := range invocationErrors {
		if invokeErr != nil {
			t.Fatalf("concurrent prompt[%d]: %v", index, invokeErr)
		}
	}
	calls := provider.snapshot()
	if len(calls) != count {
		t.Fatalf("concurrent provider calls = %d, want %d", len(calls), count)
	}
	ids := make(map[AggressorPromptID]struct{}, count)
	for index, call := range calls {
		if call.presentation.ID == 0 || call.presentation.RuntimeID != runtimeInstance.ID() {
			t.Fatalf("concurrent prompt[%d] identity = %#v", index, call.presentation)
		}
		if _, duplicate := ids[call.presentation.ID]; duplicate {
			t.Fatalf("concurrent prompt[%d] duplicate ID %d", index, call.presentation.ID)
		}
		ids[call.presentation.ID] = struct{}{}
	}

	callbackErrors := make([]error, count)
	var callbackWait sync.WaitGroup
	callbackWait.Add(count)
	start = make(chan struct{})
	for index, call := range calls {
		index, call := index, call
		go func() {
			defer callbackWait.Done()
			<-start
			want := Long(int64(call.presentation.ID))
			result, acceptErr := call.responder.Accept(context.Background(), want)
			if acceptErr == nil && !result.IdentityEqual(want) {
				acceptErr = errors.New("callback result lost response identity")
			}
			callbackErrors[index] = acceptErr
		}()
	}
	close(start)
	callbackWait.Wait()
	for index, callbackErr := range callbackErrors {
		if callbackErr != nil {
			t.Fatalf("concurrent Accept[%d]: %v", index, callbackErr)
		}
		assertAggressorUITestDoneClosed(t, calls[index].responder.Done())
	}
	if callbackCalls.Load() != count {
		t.Fatalf("concurrent callbacks = %d, want %d", callbackCalls.Load(), count)
	}
}

func TestAggressorUIConcurrentTerminalOperationsHaveExactlyOneWinner(t *testing.T) {
	t.Run("prompt", func(t *testing.T) {
		provider := &aggressorUITestPromptProvider{}
		var callbackCalls atomic.Int32
		callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
			callbackCalls.Add(1)
			return String("callback"), nil
		}))
		runtimeInstance, err := New(WithAggressorPromptProvider(provider))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		owner := aggressorUITestOwner(t, runtimeInstance)
		if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "prompt_text", Span{}, String("race"), String("default"), callback); err != nil {
			t.Fatal(err)
		}
		responder := provider.snapshot()[0].responder
		start := make(chan struct{})
		errorsSeen := make(chan error, 2)
		go func() {
			<-start
			_, acceptErr := responder.Accept(context.Background(), String("answer"))
			errorsSeen <- acceptErr
		}()
		go func() {
			<-start
			errorsSeen <- responder.Dismiss()
		}()
		close(start)
		firstErr, secondErr := <-errorsSeen, <-errorsSeen
		successes := 0
		for _, operationErr := range []error{firstErr, secondErr} {
			if operationErr == nil {
				successes++
			} else if !strings.Contains(operationErr.Error(), "closed") {
				t.Fatalf("losing prompt terminal error = %v, want descriptive closed error", operationErr)
			}
		}
		if successes != 1 {
			t.Fatalf("prompt terminal successes = %d, want one (errors %v/%v)", successes, firstErr, secondErr)
		}
		if got := callbackCalls.Load(); got != 0 && got != 1 {
			t.Fatalf("prompt callback calls = %d, want zero or one according to winner", got)
		}
		assertAggressorUITestDoneClosed(t, responder.Done())
	})

	t.Run("dialog", func(t *testing.T) {
		provider := &aggressorUITestDialogProvider{}
		var callbackCalls atomic.Int32
		callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
			callbackCalls.Add(1)
			return String("callback"), nil
		}))
		runtimeInstance, err := New(WithAggressorDialogProvider(provider))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		owner := aggressorUITestOwner(t, runtimeInstance)
		dialogValue, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog", Span{}, String("race"), HashValue(NewHash()), callback)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dbutton_action", Span{}, dialogValue, String("Run")); err != nil {
			t.Fatal(err)
		}
		if _, err := aggressorUITestInvoke(context.Background(), runtimeInstance, owner, "dialog_show", Span{}, dialogValue); err != nil {
			t.Fatal(err)
		}
		call := provider.snapshot()[0]
		start := make(chan struct{})
		errorsSeen := make(chan error, 2)
		go func() {
			<-start
			_, activateErr := call.responder.Activate(context.Background(), call.presentation.Buttons[0].ID)
			errorsSeen <- activateErr
		}()
		go func() {
			<-start
			errorsSeen <- call.responder.Dismiss()
		}()
		close(start)
		firstErr, secondErr := <-errorsSeen, <-errorsSeen
		successes := 0
		for _, operationErr := range []error{firstErr, secondErr} {
			if operationErr == nil {
				successes++
			} else if !strings.Contains(operationErr.Error(), "closed") {
				t.Fatalf("losing dialog terminal error = %v, want descriptive closed error", operationErr)
			}
		}
		if successes != 1 {
			t.Fatalf("dialog terminal successes = %d, want one (errors %v/%v)", successes, firstErr, secondErr)
		}
		if got := callbackCalls.Load(); got != 0 && got != 1 {
			t.Fatalf("dialog callback calls = %d, want zero or one according to winner", got)
		}
		assertAggressorUITestDoneClosed(t, call.responder.Done())
	})
}

func TestAggressorUIScriptLoaderChildrenInheritProvidersAndOwnResponderLifetime(t *testing.T) {
	dialogProvider := &aggressorUITestDialogProvider{}
	promptProvider := &aggressorUITestPromptProvider{}
	var hostCalls atomic.Int32
	callback := FunctionValue(aggressorUITestCallable(func(context.Context, ...Value) (Value, error) {
		return Null(), nil
	}))
	parent, err := New(
		WithInitialGlobals(map[string]Value{"ui_callback": callback}),
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader UI route reached Host")
		})),
		WithAggressorDialogProvider(dialogProvider),
		WithAggressorPromptProvider(promptProvider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close(context.Background()) })
	program, err := CompileString("ui-loader-parent.cna", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "ui-loader-child.cna", '
$dialog = dialog("child dialog", %(), $ui_callback);
dbutton_action($dialog, "Run");
dialog_show($dialog);
prompt_text("child prompt", "child default", $ui_callback);
return 17;
', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parent.Execute(context.Background(), program)
	if err != nil || result.Int32() != 17 {
		t.Fatalf("ScriptLoader child result = (%s, %v), want 17/nil", result.Describe(), err)
	}
	dialogCalls := dialogProvider.snapshot()
	promptCalls := promptProvider.snapshot()
	if len(dialogCalls) != 1 || len(promptCalls) != 1 {
		t.Fatalf("ScriptLoader provider calls dialog/prompt = %d/%d, want one/one", len(dialogCalls), len(promptCalls))
	}
	dialog := dialogCalls[0]
	prompt := promptCalls[0]
	if dialog.presentation.RuntimeID == 0 || dialog.presentation.RuntimeID == parent.ID() ||
		prompt.presentation.RuntimeID != dialog.presentation.RuntimeID {
		t.Fatalf("ScriptLoader child RuntimeIDs dialog/prompt/parent = %d/%d/%d", dialog.presentation.RuntimeID, prompt.presentation.RuntimeID, parent.ID())
	}
	if dialog.presentation.CreatorScript == 0 || dialog.presentation.PresenterScript != dialog.presentation.CreatorScript ||
		prompt.presentation.Script != dialog.presentation.CreatorScript {
		t.Fatalf("ScriptLoader child ScriptIDs dialog creator/presenter/prompt = %d/%d/%d", dialog.presentation.CreatorScript, dialog.presentation.PresenterScript, prompt.presentation.Script)
	}
	if dialog.presentation.CreationSpan.Source != "ui-loader-child.cna" ||
		dialog.presentation.PresentationSpan.Source != "ui-loader-child.cna" ||
		prompt.presentation.Span.Source != "ui-loader-child.cna" {
		t.Fatalf("ScriptLoader child spans dialog creation/show/prompt = %q/%q/%q", dialog.presentation.CreationSpan.Source, dialog.presentation.PresentationSpan.Source, prompt.presentation.Span.Source)
	}
	if dialog.presentation.Title != "child dialog" || len(dialog.presentation.Buttons) != 1 || dialog.presentation.Buttons[0].Label != "Run" {
		t.Fatalf("ScriptLoader dialog presentation = %#v", dialog.presentation)
	}
	if prompt.presentation.Kind != AggressorPromptText || prompt.presentation.Text != "child prompt" || prompt.presentation.Default.String() != "child default" {
		t.Fatalf("ScriptLoader prompt presentation = %#v", prompt.presentation)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("ScriptLoader provider route reached Host %d time(s)", hostCalls.Load())
	}
	// runScript's child lifecycle ends before Execute returns; retained child
	// responders must already be revoked even though the shared provider still
	// holds the Go interfaces.
	assertAggressorUITestDoneClosed(t, dialog.responder.Done())
	assertAggressorUITestDoneClosed(t, prompt.responder.Done())
	if result, err := dialog.responder.Activate(context.Background(), dialog.presentation.Buttons[0].ID); !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("ScriptLoader dialog responder after child unload = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
	if result, err := prompt.responder.Accept(context.Background(), String("late")); !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("ScriptLoader prompt responder after child unload = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
}

func TestAggressorUIScriptLoaderChildInheritsWithFunctionPrecedence(t *testing.T) {
	var hostCalls atomic.Int32
	var providerCalls atomic.Int32
	var overrideCalls atomic.Int32
	var childRuntimeID RuntimeID
	var childScriptID ScriptID
	parent, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), nil
		})),
		WithAggressorPromptProvider(AggressorPromptProviderFunc(func(context.Context, AggressorPromptPresentation, AggressorPromptResponder) error {
			providerCalls.Add(1)
			return nil
		})),
		WithFunction("prompt_text", func(_ context.Context, invocation Invocation) (Value, error) {
			overrideCalls.Add(1)
			childRuntimeID = invocation.Runtime.ID()
			childScriptID = invocation.Script
			if len(invocation.Arguments) != 0 {
				return Null(), errors.New("inherited override received altered arguments")
			}
			return String("child override"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close(context.Background()) })
	program, err := CompileString("ui-override-loader-parent.cna", `
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: "ui-override-loader-child.cna", 'return prompt_text();', $null];
return [$child runScript];
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parent.Execute(context.Background(), program)
	if err != nil || result.String() != "child override" {
		t.Fatalf("ScriptLoader inherited override = (%s, %v), want child override/nil", result.Describe(), err)
	}
	if overrideCalls.Load() != 1 || providerCalls.Load() != 0 || hostCalls.Load() != 0 {
		t.Fatalf("ScriptLoader override/provider/Host calls = %d/%d/%d", overrideCalls.Load(), providerCalls.Load(), hostCalls.Load())
	}
	if childRuntimeID == 0 || childRuntimeID == parent.ID() || childScriptID == 0 {
		t.Fatalf("ScriptLoader override provenance runtime/script/parent = %d/%d/%d", childRuntimeID, childScriptID, parent.ID())
	}
}
