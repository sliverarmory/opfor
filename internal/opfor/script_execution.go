package opfor

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// scriptExecutionToken marks one live execution lease. Contexts may be copied,
// retained, and used concurrently, so matching a token is not itself admission:
// every public or nested entry acquires its own counted lease. active prevents
// a retained stale context from masquerading as reentrant after its entry has
// returned, while parent lets liveness checks see an outer token shadowed by a
// completed nested entry. caller is the context before OPFOR merged in
// runtime-owned lease cancellation.
type scriptExecutionToken struct {
	script          *Script
	caller          context.Context
	parent          *scriptExecutionToken
	generation      *scriptGeneration
	generationLease bool
	active          atomic.Bool
}

type runtimeExecutionToken struct {
	runtime *Runtime
	caller  context.Context
	parent  *runtimeExecutionToken
	active  atomic.Bool
}

// Lifecycle tokens form ancestry chains for the same reason execution tokens
// do: cleanup may recursively enter another script or Runtime while retaining
// the caller's context. A single context marker lets the nested cleanup hide
// its parent, turning an otherwise harmless defensive Close or Unload call
// back into a waiter on the goroutine which is currently invoking it.
type scriptUnloadToken struct {
	script *Script
	parent *scriptUnloadToken
	active atomic.Bool
}

type runtimeCloseToken struct {
	runtime *Runtime
	parent  *runtimeCloseToken
	active  atomic.Bool
}

type scriptExecutionContextKey struct{}
type runtimeExecutionContextKey struct{}
type scriptUnloadContextKey struct{}
type runtimeCloseContextKey struct{}
type aggressorUICallbackAncestryContextKey struct{}

// aggressorUICallbackAncestry temporarily exposes the execution which entered
// an Aggressor UI provider to lifecycle classification without publishing raw
// evaluator tokens through the callback context. active is cleared as soon as
// the synchronous responder callback returns, so a callable which retains its
// context cannot later masquerade as the completed presentation execution.
type aggressorUICallbackAncestry struct {
	context context.Context
	caller  context.Context
	active  atomic.Bool
}

var errExecutionLeaseCancellation = errors.New("opfor: internal execution lease cancellation")

type detachedScriptCancellationContext struct {
	values       context.Context
	cancellation context.Context
}

func (ctx detachedScriptCancellationContext) Deadline() (deadline time.Time, ok bool) {
	return ctx.cancellation.Deadline()
}

func (ctx detachedScriptCancellationContext) Done() <-chan struct{} {
	return ctx.cancellation.Done()
}

func (ctx detachedScriptCancellationContext) Err() error {
	return ctx.cancellation.Err()
}

func (ctx detachedScriptCancellationContext) Value(key any) any {
	return ctx.values.Value(key)
}

func initializeScriptExecution(script *Script) {
	if script == nil {
		return
	}
	script.executionCtx, script.executionCancel = context.WithCancel(context.Background())
	script.unloadDone = make(chan struct{})
	initializeScriptGeneration(script)
}

func (runtime *Runtime) acquireRuntimeExecution(ctx context.Context) (context.Context, func() error, error) {
	if runtime == nil {
		return ctx, nil, errors.New("opfor: runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := executionContextError(ctx); err != nil {
		return ctx, nil, err
	}
	caller := captureExecutionCaller(ctx)
	parent, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken)

	runtime.mu.Lock()
	if runtime.closing || runtime.closed {
		runtime.mu.Unlock()
		return ctx, nil, ErrRuntimeClosed
	}
	if err := runtime.outputLimitError(); err != nil {
		runtime.mu.Unlock()
		return ctx, nil, err
	}
	runtime.executions++
	runtimeContext := runtime.executionCtx
	runtime.mu.Unlock()

	// Keep the execution itself coupled to every enclosing lease, but remember
	// only the importer-owned cancellation for work which deliberately detaches
	// later. A Host may reenter public Runtime.Invoke with a script execution
	// context; retaining that context verbatim here would cause an async fork or
	// socket task created by the nested invocation to be canceled when the outer
	// script callback merely returns.
	executionCtx, cancel := context.WithCancelCause(ctx)
	stopRuntimeCancel := context.AfterFunc(runtimeContext, func() {
		cancel(errExecutionLeaseCancellation)
	})
	token := &runtimeExecutionToken{
		runtime: runtime,
		caller:  caller,
		parent:  parent,
	}
	token.active.Store(true)
	executionCtx = context.WithValue(executionCtx, runtimeExecutionContextKey{}, token)
	release := func() error {
		if !token.active.Swap(false) {
			return nil
		}
		stopRuntimeCancel()
		cancel(errExecutionLeaseCancellation)
		runtime.mu.Lock()
		if runtime.executions > 0 {
			runtime.executions--
		}
		if runtime.closing && runtime.executions == 0 && !channelClosed(runtime.executionDone) {
			close(runtime.executionDone)
		}
		runtime.mu.Unlock()
		return nil
	}
	return executionCtx, release, nil
}

// acquireExecution atomically admits one execution entry while the script is
// active. Nested and concurrent calls each receive a counted lease; this keeps
// a context retained by Host code from bypassing unload quiescence.
func (script *Script) acquireExecution(ctx context.Context) (context.Context, func() error, error) {
	if script == nil {
		return ctx, nil, ErrScriptUnloaded
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := executionContextError(ctx); err != nil {
		return ctx, nil, err
	}
	caller := captureExecutionCaller(ctx)
	parent, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	generation := scriptGenerationFromContext(ctx, script)

	script.mu.Lock()
	if !script.active {
		script.mu.Unlock()
		return ctx, nil, ErrScriptUnloaded
	}
	if script.runtime != nil {
		if err := script.runtime.outputLimitError(); err != nil {
			script.mu.Unlock()
			return ctx, nil, err
		}
	}
	script.executions++
	if generation == nil {
		generation = script.generation
	}
	scriptContext := script.executionCtx
	script.mu.Unlock()

	executionCtx, cancel := context.WithCancelCause(ctx)
	stopScriptCancel := context.AfterFunc(scriptContext, func() {
		cancel(errExecutionLeaseCancellation)
	})
	token := &scriptExecutionToken{
		script:     script,
		caller:     caller,
		parent:     parent,
		generation: generation,
	}
	token.active.Store(true)
	executionCtx = context.WithValue(executionCtx, scriptExecutionContextKey{}, token)
	release := func() error {
		if !token.active.Swap(false) {
			return nil
		}
		stopScriptCancel()
		cancel(errExecutionLeaseCancellation)
		return script.releaseExecution(token)
	}
	return executionCtx, release, nil
}

func (script *Script) releaseExecution(token *scriptExecutionToken) error {
	if script == nil {
		return nil
	}
	var finalize bool
	var waitForCleanup bool
	var cleanupCtx context.Context
	var done chan struct{}
	script.mu.Lock()
	if token != nil && token.generationLease && token.generation != nil {
		generation := token.generation
		if generation.leases > 0 {
			generation.leases--
		}
		if generation.retiring && generation.leases == 0 && generation.drained != nil && !channelClosed(generation.drained) {
			close(generation.drained)
		}
	}
	if script.executions > 0 {
		script.executions--
	}
	if script.executions == 0 && script.unloadRequested && !script.unloadFinalizing {
		script.unloadFinalizing = true
		finalize = true
		cleanupCtx = script.unloadContext
	}
	if script.unloadRequested && script.unloadRecipient == token && script.unloadRecipientState == unloadRecipientReserved {
		// The outermost same-script execution which initiated reentrant unload
		// owns cleanup error delivery even when another concurrent execution is
		// the last lease to leave and therefore starts finalization.
		script.unloadWaiters++
		script.unloadRecipientState = unloadRecipientWaiting
		waitForCleanup = true
		done = script.unloadDone
		cleanupCtx = script.unloadContext
	}
	script.mu.Unlock()

	if finalize {
		go script.runtime.finishUnload(cleanupCtx, script)
	}
	if !waitForCleanup {
		return nil
	}
	if cleanupCtx == nil {
		cleanupCtx = context.Background()
	}
	select {
	case <-done:
		script.mu.Lock()
		if script.unloadWaiters > 0 {
			script.unloadWaiters--
		}
		err := script.consumeUnloadErrorLocked()
		if script.unloadRecipient == token && script.unloadRecipientState == unloadRecipientWaiting {
			script.unloadRecipient = nil
			script.unloadRecipientState = unloadRecipientDelivered
			if !channelClosed(script.unloadRecipientDone) {
				close(script.unloadRecipientDone)
			}
		}
		script.mu.Unlock()
		return err
	case <-cleanupCtx.Done():
		script.mu.Lock()
		if script.unloadWaiters > 0 {
			script.unloadWaiters--
		}
		if script.unloadRecipient == token && script.unloadRecipientState == unloadRecipientWaiting {
			script.unloadRecipient = nil
			script.unloadRecipientState = unloadRecipientAbandoned
			if !channelClosed(script.unloadRecipientDone) {
				close(script.unloadRecipientDone)
			}
		}
		script.mu.Unlock()
		return cleanupCtx.Err()
	}
}

func executionOwnsScript(ctx context.Context, script *Script) (*scriptExecutionToken, bool) {
	if ctx == nil || script == nil {
		return nil, false
	}
	token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	var outermost *scriptExecutionToken
	for token != nil {
		if token.script == script && token.active.Load() {
			outermost = token
		}
		token = token.parent
	}
	return outermost, outermost != nil
}

func contextRuntimeExecutionToken(ctx context.Context, runtime *Runtime) (*scriptExecutionToken, bool) {
	if ctx == nil || runtime == nil {
		return nil, false
	}
	token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	for token != nil {
		if token.script != nil && token.script.runtime == runtime && token.active.Load() {
			return token, true
		}
		token = token.parent
	}
	return nil, false
}

func contextOwnsRuntimeExecution(ctx context.Context, runtime *Runtime) bool {
	if _, ok := contextRuntimeExecutionToken(ctx, runtime); ok {
		return true
	}
	if ctx == nil || runtime == nil {
		return false
	}
	token, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken)
	for token != nil {
		if token.runtime == runtime && token.active.Load() {
			return true
		}
		token = token.parent
	}
	return false
}

func contextOwnsRuntimeUnload(ctx context.Context, runtime *Runtime) bool {
	return unloadingScriptForRuntime(ctx, runtime) != nil
}

// classifyUnloadContext describes active work from this Runtime carried by
// ctx. A cleanup call is non-waiting whenever such work exists. A same-script
// execution may own cleanup-error delivery only when no execution or unload
// ancestry from another script is present; this prevents A -> B -> Unload(A)
// and independent A/B cycles from reserving an orphaned recipient.
func classifyUnloadContext(ctx context.Context, runtime *Runtime, target *Script) (*scriptExecutionToken, bool) {
	outermost, active, foreign := classifyUnloadContextState(ctx, runtime, target)
	if foreign {
		outermost = nil
	}
	return outermost, active
}

func classifyUnloadContextState(ctx context.Context, runtime *Runtime, target *Script) (*scriptExecutionToken, bool, bool) {
	if ctx == nil || runtime == nil || target == nil {
		return nil, false, false
	}
	var outermost *scriptExecutionToken
	active := false
	foreign := false
	for token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken); token != nil; token = token.parent {
		if !token.active.Load() || token.script == nil || token.script.runtime != runtime {
			continue
		}
		active = true
		if token.script == target {
			outermost = token
		} else {
			foreign = true
		}
	}
	for token, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken); token != nil; token = token.parent {
		if token.active.Load() && token.runtime == runtime {
			active = true
		}
	}
	for token, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken); token != nil; token = token.parent {
		if !token.active.Load() || token.script == nil || token.script.runtime != runtime {
			continue
		}
		active = true
		if token.script != target {
			foreign = true
		}
	}
	if ancestry, _ := ctx.Value(aggressorUICallbackAncestryContextKey{}).(*aggressorUICallbackAncestry); ancestry != nil && ancestry.active.Load() {
		presentationOutermost, presentationActive, presentationForeign := classifyUnloadContextState(
			ancestry.context,
			runtime,
			target,
		)
		active = active || presentationActive
		foreign = foreign || presentationForeign
		// The presentation execution predates the callback's inner leases, so it
		// is the only safe recipient when both belong to the target script.
		if presentationOutermost != nil {
			outermost = presentationOutermost
		}
	}
	return outermost, active, foreign
}

// runtimeScriptActivity returns the root scripts whose active execution or
// unload finalizer is carried by ctx. Fork instances depend on their owning
// root script's teardown, so they map to that root. An execution token value is
// non-nil only when that exact root token can receive cleanup errors.
func runtimeScriptActivity(ctx context.Context, runtime *Runtime) (map[*Script]*scriptExecutionToken, map[*Script]struct{}) {
	executions := make(map[*Script]*scriptExecutionToken)
	unloads := make(map[*Script]struct{})
	if ctx == nil || runtime == nil {
		return executions, unloads
	}
	for token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken); token != nil; token = token.parent {
		if !token.active.Load() || token.script == nil || token.script.runtime != runtime {
			continue
		}
		root := rootLifecycleScript(token.script)
		if token.script == root {
			executions[root] = token
		} else if _, exists := executions[root]; !exists {
			executions[root] = nil
		}
	}
	for token, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken); token != nil; token = token.parent {
		if !token.active.Load() || token.script == nil || token.script.runtime != runtime {
			continue
		}
		unloads[rootLifecycleScript(token.script)] = struct{}{}
	}
	if ancestry, _ := ctx.Value(aggressorUICallbackAncestryContextKey{}).(*aggressorUICallbackAncestry); ancestry != nil && ancestry.active.Load() {
		presentationExecutions, presentationUnloads := runtimeScriptActivity(ancestry.context, runtime)
		for script, token := range presentationExecutions {
			// The presentation execution is older than the callback's leases and
			// therefore owns any deferred lifecycle-error delivery.
			executions[script] = token
		}
		for script := range presentationUnloads {
			unloads[script] = struct{}{}
		}
	}
	return executions, unloads
}

func rootLifecycleScript(script *Script) *Script {
	for script != nil && script.forkParent != nil {
		script = script.forkParent
	}
	return script
}

// scriptExecutionError observes both the importer context and the script-owned
// cancellation signal synchronously. The latter matters when importer code
// unloads its own script reentrantly: context.AfterFunc wakes blocked calls,
// while this check prevents the current evaluator expression from committing
// a mutation before that asynchronous wakeup is scheduled.
func scriptExecutionError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(context.Cause(ctx), ErrScriptUnloaded) {
			return ErrScriptUnloaded
		}
		return err
	}
	token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken)
	for token != nil {
		if token.active.Load() && token.script != nil && token.script.executionCtx != nil {
			if err := token.script.runtime.outputLimitError(); err != nil {
				return err
			}
			if err := token.script.executionCtx.Err(); err != nil {
				return err
			}
			if token.generationLease && token.generation != nil && token.generation.context != nil {
				if err := token.generation.context.Err(); err != nil {
					return ErrScriptUnloaded
				}
			}
		}
		token = token.parent
	}
	return nil
}

func runtimeExecutionError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	token, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken)
	for token != nil {
		if token.active.Load() && token.runtime != nil && token.runtime.executionCtx != nil {
			if err := token.runtime.outputLimitError(); err != nil {
				return err
			}
			if err := token.runtime.executionCtx.Err(); err != nil {
				return err
			}
		}
		token = token.parent
	}
	return nil
}

// executionContextError is the admission check for every public evaluator
// entry. A script Host can reenter Runtime.Invoke, and a runtime Host can enter
// a Script, so inspecting only the token type being acquired leaves a window
// between synchronous owner cancellation and context.AfterFunc propagation.
func executionContextError(ctx context.Context) error {
	if err := scriptExecutionError(ctx); err != nil {
		return err
	}
	return runtimeExecutionError(ctx)
}

// joinExecutionContextError gives a fatal execution condition precedence over
// an importer error returned by the same callback. In particular, importer
// code cannot hide a latched resource violation by returning an unrelated
// error after triggering it.
func joinExecutionContextError(ctx context.Context, err error) error {
	fatalErr := executionContextError(ctx)
	if fatalErr == nil {
		return err
	}
	return errors.Join(fatalErr, err)
}

func detachScriptCancellation(ctx context.Context, token *scriptExecutionToken) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if token == nil || token.caller == nil {
		return context.WithoutCancel(ctx)
	}
	return detachedScriptCancellationContext{
		values:       context.WithoutCancel(ctx),
		cancellation: token.caller,
	}
}

// captureExecutionCaller preserves cancellation added by importer code between
// nested OPFOR entries while filtering the private cancellation which ends an
// enclosing execution lease. Private entries use WithCancelCause, so a child
// context canceled explicitly by its importer remains distinguishable even if
// the filtering callback runs after the enclosing Host has returned.
func captureExecutionCaller(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if !hasActiveExecutionToken(ctx) {
		return ctx
	}
	fallback := detachExecutionLeaseCancellation(ctx)

	base := context.Background()
	deadlineCancel := func() {}
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		base, cancel = context.WithDeadline(base, deadline)
		deadlineCancel = cancel
	}
	bridge, cancel := context.WithCancelCause(base)
	propagate := func(source context.Context) {
		cause := context.Cause(source)
		if cause == nil || errors.Is(cause, errExecutionLeaseCancellation) {
			return
		}
		cancel(cause)
	}
	stopCurrent := context.AfterFunc(ctx, func() { propagate(ctx) })
	stopFallback := context.AfterFunc(fallback, func() { propagate(fallback) })
	context.AfterFunc(bridge, func() {
		stopCurrent()
		stopFallback()
		deadlineCancel()
	})
	return detachedScriptCancellationContext{
		values:       context.WithoutCancel(ctx),
		cancellation: bridge,
	}
}

// callbackContextSnapshot preserves the invoking context's cancellation and
// importer-owned values while permanently hiding OPFOR-private evaluator and
// lifecycle state. meter is selected atomically when one importer callback
// invocation begins and remains stable for that invocation's entire lifetime.
type callbackContextSnapshot struct {
	context.Context
	meter *executionMeter
}

func (ctx callbackContextSnapshot) Value(key any) any {
	switch key.(type) {
	case executionMeterKey:
		if ctx.meter == nil {
			return nil
		}
		return ctx.meter
	case currentFiberContextKey,
		includeChainContextKey,
		bindingInvocationContextKey,
		loadableResolutionContextKey,
		nativeDispatchStateContextKey,
		portableScriptInstanceRunContextKey,
		scriptExecutionContextKey,
		runtimeExecutionContextKey,
		scriptUnloadContextKey,
		runtimeCloseContextKey,
		aggressorUICallbackAncestryContextKey,
		scriptGenerationCleanupContextKey:
		return nil
	default:
		return ctx.Context.Value(key)
	}
}

func captureCallbackSchedulingContext(ctx context.Context) (context.Context, *executionMeter) {
	retained := captureExecutionCaller(ctx)
	meter, _ := retained.Value(executionMeterKey{}).(*executionMeter)
	return callbackContextSnapshot{Context: retained}, meter
}

func snapshotCallbackInvocationContext(ctx context.Context, meter *executionMeter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return callbackContextSnapshot{Context: ctx, meter: meter}
}

func hasActiveExecutionToken(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	for token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken); token != nil; token = token.parent {
		if token.active.Load() {
			return true
		}
	}
	for token, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken); token != nil; token = token.parent {
		if token.active.Load() {
			return true
		}
	}
	if ancestry, _ := ctx.Value(aggressorUICallbackAncestryContextKey{}).(*aggressorUICallbackAncestry); ancestry != nil && ancestry.active.Load() {
		// The UI callback context deliberately hides raw presenter tokens. Treat
		// its short-lived marker as active ancestry so captureExecutionCaller
		// filters the presenter's internal lease cancellation before an inner
		// callback token records its caller context.
		return hasActiveExecutionToken(ancestry.context)
	}
	return false
}

func hasActiveLifecycleToken(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	for token, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken); token != nil; token = token.parent {
		if token.active.Load() {
			return true
		}
	}
	for token, _ := ctx.Value(runtimeCloseContextKey{}).(*runtimeCloseToken); token != nil; token = token.parent {
		if token.active.Load() {
			return true
		}
	}
	return false
}

// detachExecutionLeaseCancellation lets a runtime-owned asynchronous task
// outlive the callback that created it while retaining importer cancellation,
// deadlines, and importer-owned context values. The synchronous
// ScriptInstance run owner is deliberately hidden. Fork, socket, and process
// tasks register separate owner cleanup hooks, so Script unload still cancels
// them explicitly.
func detachExecutionLeaseCancellation(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	result := ctx
	detached := false
	if token, _ := ctx.Value(scriptExecutionContextKey{}).(*scriptExecutionToken); token != nil {
		for token != nil && !token.active.Load() {
			token = token.parent
		}
		if token != nil {
			result = detachScriptCancellation(result, token)
			detached = true
		}
	}
	if token, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken); token != nil {
		for token != nil && !token.active.Load() {
			token = token.parent
		}
		if token != nil && token.caller != nil {
			result = detachedScriptCancellationContext{
				values:       context.WithoutCancel(result),
				cancellation: token.caller,
			}
			detached = true
		}
	}
	// ScriptInstance.runScript/evaluate installs an additional private cancel
	// context around the evaluator. A fork, socket, or process created inside
	// that call is owned by its Script hierarchy, not by the synchronous method
	// frame, so use the pre-frame importer cancellation captured by its token.
	for token, _ := ctx.Value(portableScriptInstanceRunContextKey{}).(*portableScriptInstanceRunToken); token != nil; token = token.parent {
		if token.active.Load() && token.caller != nil {
			result = detachedScriptCancellationContext{
				values:       context.WithoutCancel(result),
				cancellation: token.caller,
			}
			detached = true
			break
		}
	}
	if !detached {
		if ancestry, _ := ctx.Value(aggressorUICallbackAncestryContextKey{}).(*aggressorUICallbackAncestry); ancestry != nil && ancestry.active.Load() {
			// callbackContext captured the responder-supplied context's filtered
			// caller before hiding its raw tokens. Keep that exact cancellation
			// source as capture's fallback after a presentation lease is canceled;
			// in particular, a provider which deliberately supplies Background
			// does not accidentally inherit the presenter's caller cancellation.
			caller := ancestry.caller
			if caller == nil {
				caller = context.Background()
			}
			result = detachedScriptCancellationContext{
				values:       context.WithoutCancel(result),
				cancellation: caller,
			}
		}
	}
	return withoutPortableScriptInstanceRunOwner(result)
}

// detachAsynchronousExecutionContext is the launch context for runtime-owned
// asynchronous work. It retains importer cancellation, deadlines, values, and
// the selected instruction meter while hiding evaluator, binding, include,
// loadable-resolution, native-dispatch, UI-ancestry, lifecycle,
// generation-cleanup, and ScriptInstance-run tokens. The new task acquires its
// own Script execution and generation provenance.
func detachAsynchronousExecutionContext(ctx context.Context) context.Context {
	detached := detachExecutionLeaseCancellation(ctx)
	meter, _ := detached.Value(executionMeterKey{}).(*executionMeter)
	return callbackContextSnapshot{Context: detached, meter: meter}
}

func detachRuntimeCancellation(ctx context.Context, runtime *Runtime) context.Context {
	if ctx == nil || runtime == nil {
		return ctx
	}
	token, _ := ctx.Value(runtimeExecutionContextKey{}).(*runtimeExecutionToken)
	for token != nil {
		if token.runtime == runtime && token.active.Load() && token.caller != nil {
			return detachedScriptCancellationContext{
				values:       context.WithoutCancel(ctx),
				cancellation: token.caller,
			}
		}
		token = token.parent
	}
	return ctx
}

func withScriptUnloadContext(ctx context.Context, script *Script) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken)
	token := &scriptUnloadToken{script: script, parent: parent}
	token.active.Store(true)
	return context.WithValue(ctx, scriptUnloadContextKey{}, token), func() {
		token.active.Store(false)
	}
}

func unloadingScriptFromContext(ctx context.Context) *Script {
	if ctx == nil {
		return nil
	}
	token, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken)
	for token != nil {
		if token.active.Load() && token.script != nil {
			return token.script
		}
		token = token.parent
	}
	return nil
}

func unloadingScriptForRuntime(ctx context.Context, runtime *Runtime) *Script {
	if ctx == nil || runtime == nil {
		return nil
	}
	token, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken)
	for token != nil {
		if token.active.Load() && token.script != nil && token.script.runtime == runtime {
			return token.script
		}
		token = token.parent
	}
	return nil
}

func contextUnloadsScript(ctx context.Context, script *Script) bool {
	if ctx == nil || script == nil {
		return false
	}
	token, _ := ctx.Value(scriptUnloadContextKey{}).(*scriptUnloadToken)
	for token != nil {
		if token.active.Load() && token.script == script {
			return true
		}
		token = token.parent
	}
	return false
}

func withRuntimeCloseContext(ctx context.Context, runtime *Runtime) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent, _ := ctx.Value(runtimeCloseContextKey{}).(*runtimeCloseToken)
	token := &runtimeCloseToken{runtime: runtime, parent: parent}
	token.active.Store(true)
	return context.WithValue(ctx, runtimeCloseContextKey{}, token), func() {
		token.active.Store(false)
	}
}

func closingRuntimeFromContext(ctx context.Context) *Runtime {
	if ctx == nil {
		return nil
	}
	token, _ := ctx.Value(runtimeCloseContextKey{}).(*runtimeCloseToken)
	for token != nil {
		if token.active.Load() && token.runtime != nil {
			return token.runtime
		}
		token = token.parent
	}
	return nil
}

func contextClosesRuntime(ctx context.Context, runtime *Runtime) bool {
	if ctx == nil || runtime == nil {
		return false
	}
	token, _ := ctx.Value(runtimeCloseContextKey{}).(*runtimeCloseToken)
	for token != nil {
		if token.active.Load() && token.runtime == runtime {
			return true
		}
		token = token.parent
	}
	return false
}

// withoutScriptLifecycleTokens retains importer values, deadlines, and
// cancellation while preventing the private Close worker from being mistaken
// for the Host/observer goroutine whose reentrant call started it.
func withoutScriptLifecycleTokens(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, scriptExecutionContextKey{}, (*scriptExecutionToken)(nil))
	ctx = context.WithValue(ctx, runtimeExecutionContextKey{}, (*runtimeExecutionToken)(nil))
	return context.WithValue(ctx, scriptUnloadContextKey{}, (*scriptUnloadToken)(nil))
}

func (script *Script) consumeUnloadErrorLocked() error {
	if script == nil || script.unloadErrDelivered {
		return nil
	}
	script.unloadErrDelivered = true
	return script.unloadErr
}

func (script *Script) consumeUnloadErrorForWaiter(ctx context.Context) error {
	if script == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		script.mu.Lock()
		state := script.unloadRecipientState
		recipientDone := script.unloadRecipientDone
		if state != unloadRecipientReserved && state != unloadRecipientWaiting {
			err := script.consumeUnloadErrorLocked()
			script.mu.Unlock()
			return err
		}
		script.mu.Unlock()
		select {
		case <-recipientDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (script *Script) waitUnloaded(ctx context.Context) error {
	if script == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	script.mu.Lock()
	done := script.unloadDone
	complete := script.unloadRequested && script.unloadFinalizing && channelClosed(done)
	if !complete {
		script.unloadWaiters++
	}
	script.mu.Unlock()
	if complete {
		return script.consumeUnloadErrorForWaiter(ctx)
	}

	select {
	case <-done:
		script.mu.Lock()
		if script.unloadWaiters > 0 {
			script.unloadWaiters--
		}
		script.mu.Unlock()
		return script.consumeUnloadErrorForWaiter(ctx)
	case <-ctx.Done():
		script.mu.Lock()
		if script.unloadWaiters > 0 {
			script.unloadWaiters--
		}
		script.mu.Unlock()
		return ctx.Err()
	}
}

func channelClosed(channel <-chan struct{}) bool {
	if channel == nil {
		return false
	}
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func joinExecutionError(err error, release func() error) error {
	if release == nil {
		return err
	}
	return errors.Join(err, release())
}
