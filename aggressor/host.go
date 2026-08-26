package aggressor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/sliverarmory/opfor"
)

// Value is OPFOR's public Sleep scalar representation. The alias keeps
// callback signatures centered on this adapter package without hiding value
// identity or coercion behavior from importers.
type Value = opfor.Value

// ErrNotReference is returned when a callback attempts to mutate an expression
// temporary that has no caller-owned scalar cell.
var ErrNotReference = errors.New("aggressor: argument is not a pass-by-name reference")

// ErrNoRuntime is returned when a runtime capability is used on a Request
// constructed without an originating OPFOR runtime.
var ErrNoRuntime = errors.New("aggressor: request has no originating runtime")

// Position is a source position detached from OPFOR's lexer representation.
type Position struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Location is the source range of a host call.
type Location struct {
	Source string   `json:"source,omitempty"`
	Start  Position `json:"start"`
	End    Position `json:"end"`
}

// Runtime is an opaque, comparable capability for the OPFOR runtime and, when
// applicable, script generation that made a host request. It lets callbacks
// emit events and invoke script hooks without exposing evaluator state or
// requiring opfor.BindingKind values.
//
// A Runtime capability may be retained after its callback returns; doing so
// also retains the underlying OPFOR runtime. Its methods are concurrency-safe,
// honor their caller-supplied context, and follow the originating script
// generation's lifecycle. Retention does not keep scripts or registrations
// active: after explicit unload, calls return opfor.ErrScriptUnloaded. The zero
// value is invalid.
type Runtime struct {
	bindings opfor.AggressorBindings
}

// Valid reports whether this capability identifies an originating runtime. It
// reports provenance rather than liveness and remains true after that runtime
// closes.
func (runtime Runtime) Valid() bool {
	return runtime.bindings.Valid()
}

// Same reports whether two capabilities identify the same runtime. Runtime is
// also a comparable type and may be used directly as a map key.
func (runtime Runtime) Same(other Runtime) bool {
	return runtime.bindings.Same(other.bindings)
}

// DispatchEvent invokes exact and wildcard event registrations.
func (runtime Runtime) DispatchEvent(ctx context.Context, name string, arguments ...Value) ([]Value, error) {
	if !runtime.Valid() {
		return nil, ErrNoRuntime
	}
	return runtime.bindings.DispatchEvent(ctx, name, arguments...)
}

// InvokeHook invokes the most recently registered set <name> callback.
func (runtime Runtime) InvokeHook(ctx context.Context, name string, arguments ...Value) (Value, error) {
	if !runtime.Valid() {
		return opfor.Null(), ErrNoRuntime
	}
	return runtime.bindings.InvokeHook(ctx, name, arguments...)
}

// InvokePopupHook invokes the most recently registered popup <name> callback.
// Use DispatchPopupHook for normal additive menu composition.
func (runtime Runtime) InvokePopupHook(ctx context.Context, name string, arguments ...Value) (Value, error) {
	if !runtime.Valid() {
		return opfor.Null(), ErrNoRuntime
	}
	return runtime.bindings.InvokePopupHook(ctx, name, arguments...)
}

// DispatchPopupHook invokes every exact popup <name> callback in load order.
// Popup declarations are additive, so this is the normal host entry point for
// composing an Aggressor popup across all loaded scripts. The returned Values
// preserve callback order; most popup bodies return null and communicate their
// menu contributions through registered item/menu bindings.
func (runtime Runtime) DispatchPopupHook(ctx context.Context, name string, arguments ...Value) ([]Value, error) {
	if !runtime.Valid() {
		return nil, ErrNoRuntime
	}
	return runtime.bindings.DispatchPopupHook(ctx, name, arguments...)
}

// Argument is a callback-facing ordinary, named, or pass-by-name argument.
// Value observes the current scalar. Set succeeds for a bare variable or an
// explicit pass-by-name reference, but not for an expression temporary. A
// retained reference argument remains a trusted mutation capability after the
// callback and is not lifecycle-revoked; snapshot Value when retention does
// not require pass-by-name writes.
type Argument struct {
	Name string `json:"name,omitempty"`

	value       Value
	reference   *opfor.Cell
	callback    ScriptCallback
	callbackErr error
}

// Value returns the argument's current value.
func (argument Argument) Value() Value {
	if argument.reference != nil {
		return argument.reference.Get()
	}
	return argument.value
}

// IsReference reports whether Set may mutate the caller's scalar.
func (argument Argument) IsReference() bool {
	return argument.reference != nil
}

// Set mutates an argument backed by the caller's scalar cell. It intentionally
// retains that low-level capability if Argument is copied beyond the callback.
func (argument Argument) Set(value Value) error {
	if argument.reference == nil {
		return ErrNotReference
	}
	argument.reference.Set(value)
	return nil
}

// Callback returns an immutable capability for a function-valued argument.
// The capability snapshots the function passed to the original host call and
// remains usable after that call returns, until the owning execution
// generation retires or the script unloads.
func (argument Argument) Callback() (ScriptCallback, error) {
	if argument.callbackErr != nil {
		return ScriptCallback{}, argument.callbackErr
	}
	if !argument.callback.Valid() {
		return ScriptCallback{}, opfor.ErrInvalidCallable
	}
	return argument.callback, nil
}

// ScriptCallback is a retained script-owned callback capability. Its zero
// value is invalid. Invoke honors its context and rejects use after the
// originating script unloads or its runtime closes, without exposing the raw
// OPFOR runtime.
type ScriptCallback struct {
	callable opfor.Callable
}

// Valid reports whether callback contains a retained script-owned function.
func (callback ScriptCallback) Valid() bool {
	return callback.callable != nil
}

// Invoke calls the retained function with positional Sleep values.
func (callback ScriptCallback) Invoke(ctx context.Context, arguments ...Value) (Value, error) {
	if callback.callable == nil {
		return opfor.Null(), opfor.ErrInvalidCallable
	}
	return callback.callable.Invoke(ctx, arguments...)
}

// Request is the stable importer-facing form of an Aggressor host call. It
// exposes only an opaque runtime capability and omits the raw *opfor.Runtime
// and internal evaluator details.
//
// Arguments is a detached top-level slice and may be retained. Scalar Values
// are immutable, while compound Values preserve reference identity. A retained
// reference Argument remains a trusted mutation capability after the callback;
// Runtime and ScriptCallback values retain their separately documented
// lifecycle-bound capabilities.
type Request struct {
	Name      string     `json:"name"`
	ScriptID  uint64     `json:"script_id,omitempty"`
	Runtime   Runtime    `json:"-"`
	Arguments []Argument `json:"-"`
	Location  Location   `json:"location"`
}

// Arg returns the zero-based argument and whether it exists.
func (request Request) Arg(index int) (Argument, bool) {
	if index < 0 || index >= len(request.Arguments) {
		return Argument{}, false
	}
	return request.Arguments[index], true
}

// Values snapshots the request's current argument values.
func (request Request) Values() []Value {
	values := make([]Value, len(request.Arguments))
	for index, argument := range request.Arguments {
		values[index] = argument.Value()
	}
	return values
}

// Callback handles one named Aggressor function or predicate. Calls are
// synchronous and may occur concurrently for independent script executions.
// Implementations should observe ctx and must not retain it after returning;
// asynchronous work should retain the supplied Runtime or an Argument's
// ScriptCallback and use a new caller-owned context. A successful compound
// Value is transferred directly to script code, and an error is authoritative
// for the host invocation.
type Callback func(context.Context, Request) (Value, error)

// Host is a concurrency-safe callback registry implementing opfor.Host. Its
// zero value is ready for use. Register replaces an existing callback with the
// same normalized name. Registry operations are safe during dispatch, but Host
// does not serialize callback execution; registered callbacks must follow the
// concurrency contract documented by Callback.
type Host struct {
	mu       sync.RWMutex
	handlers map[string]Callback
	fallback Callback
}

var _ opfor.Host = (*Host)(nil)

// NewHost creates an empty Aggressor callback host.
func NewHost() *Host {
	return &Host{handlers: make(map[string]Callback)}
}

// Register installs or replaces a callback. A leading & is accepted and
// removed so documentation spelling and Sleep function-reference spelling may
// be used interchangeably.
func (host *Host) Register(name string, callback Callback) error {
	if host == nil {
		return errors.New("aggressor: host is nil")
	}
	normalized, err := normalizeCallbackName(name)
	if err != nil {
		return err
	}
	if callback == nil {
		return fmt.Errorf("aggressor: callback %q is nil", normalized)
	}
	host.mu.Lock()
	if host.handlers == nil {
		host.handlers = make(map[string]Callback)
	}
	host.handlers[normalized] = callback
	host.mu.Unlock()
	return nil
}

// Unregister removes a callback and reports whether it was present.
func (host *Host) Unregister(name string) bool {
	if host == nil {
		return false
	}
	normalized, err := normalizeCallbackName(name)
	if err != nil {
		return false
	}
	host.mu.Lock()
	_, present := host.handlers[normalized]
	delete(host.handlers, normalized)
	host.mu.Unlock()
	return present
}

// SetFallback installs the callback used for names without an exact
// registration. Passing nil restores explicit UnsupportedError behavior.
func (host *Host) SetFallback(callback Callback) {
	if host == nil {
		return
	}
	host.mu.Lock()
	host.fallback = callback
	host.mu.Unlock()
}

// Names returns registered callback names in lexical order.
func (host *Host) Names() []string {
	if host == nil {
		return nil
	}
	host.mu.RLock()
	names := make([]string, 0, len(host.handlers))
	for name := range host.handlers {
		names = append(names, name)
	}
	host.mu.RUnlock()
	sort.Strings(names)
	return names
}

// Call adapts an OPFOR invocation to an importer-facing Request.
func (host *Host) Call(ctx context.Context, invocation opfor.Invocation) (Value, error) {
	if err := ctx.Err(); err != nil {
		return opfor.Null(), err
	}
	normalized, err := normalizeCallbackName(invocation.Name)
	if err != nil {
		return opfor.Null(), err
	}

	var callback Callback
	if host != nil {
		host.mu.RLock()
		callback = host.handlers[normalized]
		if callback == nil {
			callback = host.fallback
		}
		host.mu.RUnlock()
	}
	if callback == nil {
		return opfor.Null(), &opfor.UnsupportedError{
			Operation: "Aggressor function",
			Name:      normalized,
			Span:      invocation.Span,
		}
	}

	arguments := make([]Argument, len(invocation.Arguments))
	for index, argument := range invocation.Arguments {
		adapted := Argument{
			Name:      argument.Name,
			value:     argument.Resolve(),
			reference: argument.Reference,
		}
		if _, callable := adapted.value.Function(); callable {
			retained, callbackErr := invocation.RetainCallback(adapted.value)
			if callbackErr == nil {
				adapted.callback = ScriptCallback{callable: retained}
			} else {
				// Preserve scriptless Runtime.Invoke and manually constructed Host
				// calls. The request remains inspectable, while explicitly asking
				// for a retained callback reports why no safe capability exists.
				adapted.callbackErr = callbackErr
			}
		}
		arguments[index] = adapted
	}
	request := Request{
		Name:      normalized,
		ScriptID:  uint64(invocation.Script),
		Runtime:   Runtime{bindings: invocation.Bindings()},
		Arguments: arguments,
		Location:  locationFromSpan(invocation.Span),
	}
	return callback(ctx, request)
}

func normalizeCallbackName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "&")
	if name == "" {
		return "", errors.New("aggressor: callback name is empty")
	}
	if strings.ContainsAny(name, " \t\r\n(){}[];,:\"'`") {
		return "", fmt.Errorf("aggressor: invalid callback name %q", name)
	}
	return name, nil
}

func locationFromSpan(span opfor.Span) Location {
	return Location{
		Source: span.Source,
		Start: Position{
			Offset: span.Start.Offset,
			Line:   span.Start.Line,
			Column: span.Start.Column,
		},
		End: Position{
			Offset: span.End.Offset,
			Line:   span.End.Line,
			Column: span.End.Column,
		},
	}
}
