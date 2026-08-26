package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/sliverarmory/opfor"
	"github.com/spf13/cobra"
)

// persistentScriptRuntime is the library surface needed by the local
// JSON-lines adapter. Keeping it separate from scriptRuntime leaves ordinary
// run/eval/check dependency tests small while the real Runtime supplies all
// methods.
type persistentScriptRuntime interface {
	scriptRuntime
	Load(context.Context, *opfor.Program, ...opfor.Value) (*opfor.Script, error)
	DispatchEvent(context.Context, string, ...opfor.Value) ([]opfor.Value, error)
	DispatchPopupHook(context.Context, string, ...opfor.Value) ([]opfor.Value, error)
	InvokeBinding(context.Context, opfor.BindingKind, string, ...opfor.Value) (opfor.Value, error)
	InvokeBindingByID(context.Context, opfor.ScriptID, uint64, ...opfor.Value) (opfor.Value, error)
	InvokeConsole(context.Context, opfor.ConsoleInvocation) (opfor.Value, error)
	Invoke(context.Context, string, ...opfor.Value) (opfor.Value, error)
	Bindings(opfor.BindingKind, string) []opfor.Binding
	ScriptByID(opfor.ScriptID) (*opfor.Script, error)
}

type serveRequest struct {
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Kind    string          `json:"kind,omitempty"`
	Name    string          `json:"name,omitempty"`
	Path    string          `json:"path,omitempty"`
	Args    []any           `json:"args,omitempty"`
	Raw     string          `json:"raw,omitempty"`
	Session any             `json:"session,omitempty"`
	Script  uint64          `json:"script,omitempty"`
	Binding uint64          `json:"binding_id,omitempty"`
	Flags   *int32          `json:"-"`

	// argsJSON is the unmodified JSON template supplied by the controller.
	// Reload decodes a fresh Value graph from these bytes instead of retaining
	// mutable Values which the old script may have changed through @ARGV.
	argsJSON  json.RawMessage
	argsSet   bool
	scriptSet bool
	flagsSet  bool
}

type serveResponse struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result *any            `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

const serveValueTag = "$opfor"

func newServeCommand(options Options, dependencies dependencies, settings *runtimeSettings) *cobra.Command {
	var fireReady bool
	command := &cobra.Command{
		Use:   "serve [script] [args...]",
		Short: "Keep scripts loaded and dispatch local JSON-lines requests",
		Long: "Keep Sleep or Aggressor scripts loaded and dispatch newline-delimited JSON requests from stdin.\n\n" +
			"Script output and warnings go to stderr; protocol responses go to stdout. This is a local adapter and does not connect to a Team Server.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if args[0] == "-" {
				return errors.New("serve requires a filesystem script path; standard input is reserved for protocol requests")
			}
			return validateScriptPath(args[0])
		},
		RunE: func(command *cobra.Command, args []string) (resultErr error) {
			var program *opfor.Program
			var err error
			if len(args) != 0 {
				program, err = compileFile(dependencies, args[0], options.Stdin, settings.maxSourceBytes)
				if err != nil {
					return err
				}
			}
			// The protocol owns stdin. A script that explicitly reads from its
			// console sees EOF instead of consuming request frames.
			base, err := dependencies.newRuntime(strings.NewReader(""), options.Stderr, options.Stderr, *settings)
			if err != nil {
				return fmt.Errorf("create runtime: %w", err)
			}
			defer func() { resultErr = errors.Join(resultErr, closeRuntime(base)) }()
			runtime, ok := base.(persistentScriptRuntime)
			if !ok {
				return errors.New("runtime does not support persistent dispatch")
			}
			session := newServeSession(runtime, dependencies, settings.maxSourceBytes)
			if program != nil {
				values := make([]opfor.Value, len(args)-1)
				for index, argument := range args[1:] {
					values[index] = opfor.String(argument)
				}
				script, loadErr := runtime.Load(command.Context(), program, values...)
				if loadErr != nil {
					return fmt.Errorf("load %s: %w", args[0], loadErr)
				}
				argumentsJSON, marshalErr := json.Marshal(args[1:])
				if marshalErr != nil {
					return fmt.Errorf("encode startup arguments: %w", marshalErr)
				}
				if err := session.adoptStartup(script, args[0], argumentsJSON); err != nil {
					return err
				}
			}
			if fireReady && program != nil {
				if _, err := runtime.DispatchEvent(command.Context(), "ready"); err != nil {
					return fmt.Errorf("dispatch ready: %w", err)
				}
			}
			return runServe(command.Context(), session, options.Stdin, options.Stdout)
		},
	}
	command.Flags().BoolVar(&fireReady, "fire-ready", false, "dispatch the ready event after the script loads")
	command.Flags().SetInterspersed(false)
	return command
}

func runServe(
	ctx context.Context,
	session *serveSession,
	input io.Reader,
	output io.Writer,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	readerCtx, cancelReader := context.WithCancel(ctx)
	defer cancelReader()

	type inputLine struct {
		data []byte
		err  error
	}
	lines := make(chan inputLine)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 64*1024), maxREPLLine)
		for scanner.Scan() {
			line := inputLine{data: append([]byte(nil), scanner.Bytes()...)}
			select {
			case lines <- line:
			case <-readerCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- inputLine{err: err}:
			case <-readerCtx.Done():
			}
		}
	}()

	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	for {
		var line inputLine
		var ok bool
		select {
		case <-readerCtx.Done():
			return readerCtx.Err()
		case line, ok = <-lines:
			if !ok {
				return readerCtx.Err()
			}
		}
		if line.err != nil {
			return fmt.Errorf("read serve request: %w", line.err)
		}
		frame := bytes.TrimSpace(line.data)
		if len(frame) == 0 {
			continue
		}
		request, err := decodeServeRequest(frame)
		if err != nil {
			if encodeErr := encoder.Encode(serveResponse{Error: err.Error()}); encodeErr != nil {
				return fmt.Errorf("write serve response: %w", encodeErr)
			}
			continue
		}
		result, stop, dispatchErr := dispatchServeRequest(ctx, session, request)
		response := serveResponse{ID: request.ID}
		if dispatchErr != nil {
			response.Error = dispatchErr.Error()
		} else {
			response.Result = &result
		}
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("write serve response: %w", err)
		}
		if stop {
			return nil
		}
	}
}

func decodeServeRequest(line []byte) (serveRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	var request serveRequest
	if err := decoder.Decode(&request); err != nil {
		return serveRequest{}, fmt.Errorf("invalid JSON request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return serveRequest{}, errors.New("invalid JSON request: multiple values")
		}
		return serveRequest{}, fmt.Errorf("invalid JSON request: %w", err)
	}
	request.Method = strings.ToLower(strings.TrimSpace(request.Method))
	if request.Method == "" {
		return serveRequest{}, errors.New("request method is empty")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return serveRequest{}, fmt.Errorf("invalid JSON request: %w", err)
	}
	if arguments, ok := fields["args"]; ok {
		request.argsSet = true
		request.argsJSON = append(json.RawMessage(nil), arguments...)
	}
	_, request.scriptSet = fields["script"]
	flags, flagsSet := fields["flags"]
	request.flagsSet = flagsSet
	if request.Method == "trace" && request.flagsSet && !bytes.Equal(bytes.TrimSpace(flags), []byte("null")) {
		var value int32
		if err := json.Unmarshal(flags, &value); err != nil {
			return serveRequest{}, fmt.Errorf("invalid JSON request: %w", err)
		}
		request.Flags = &value
	}
	return request, nil
}

func dispatchServeRequest(
	ctx context.Context,
	session *serveSession,
	request serveRequest,
) (any, bool, error) {
	if session == nil || session.runtime == nil {
		return nil, false, errors.New("persistent script session is unavailable")
	}
	runtime := session.runtime
	arguments := make([]opfor.Value, len(request.Args))
	for index, argument := range request.Args {
		value, err := serveJSONToValue(argument)
		if err != nil {
			return nil, false, fmt.Errorf("argument %d: %w", index+1, err)
		}
		arguments[index] = value
	}
	switch request.Method {
	case "load":
		result, err := session.load(ctx, request.Path, serveRequestArgumentsJSON(request))
		return result, false, err
	case "scripts", "ls":
		return session.list(), false, nil
	case "trace":
		script, err := serveControlScript(runtime, request, "trace")
		if err != nil {
			return nil, false, err
		}
		var flags int32
		if request.flagsSet {
			if request.Flags == nil {
				return nil, false, errors.New("trace flags must be a 32-bit integer")
			}
			flags, err = script.SetDebugFlags(*request.Flags)
		} else {
			flags, err = script.DebugFlags()
		}
		if err != nil {
			return nil, false, err
		}
		return map[string]any{"script": uint64(script.ID()), "flags": flags}, false, nil
	case "profile":
		script, err := serveControlScript(runtime, request, "profile")
		if err != nil {
			return nil, false, err
		}
		report, err := script.SnapshotProfile()
		if err != nil {
			return nil, false, err
		}
		return report, false, nil
	case "reload":
		return session.reload(ctx, request)
	case "unload":
		return session.unload(ctx, request)
	case "event":
		if request.Name == "" {
			return nil, false, errors.New("event name is empty")
		}
		values, err := runtime.DispatchEvent(ctx, request.Name, arguments...)
		if err != nil {
			return nil, false, err
		}
		return serveValuesToJSON(values), false, nil
	case "popup":
		if request.Name == "" {
			return nil, false, errors.New("popup hook name is empty")
		}
		values, err := runtime.DispatchPopupHook(ctx, request.Name, arguments...)
		if err != nil {
			return nil, false, err
		}
		return serveValuesToJSON(values), false, nil
	case "binding":
		if request.Script != 0 || request.Binding != 0 {
			if request.Script == 0 || request.Binding == 0 {
				return nil, false, errors.New("exact binding invocation requires script and binding_id")
			}
			value, err := runtime.InvokeBindingByID(ctx, opfor.ScriptID(request.Script), request.Binding, arguments...)
			if err != nil {
				return nil, false, err
			}
			return serveValueToJSON(value, make(map[any]struct{})), false, nil
		}
		if request.Kind == "" || request.Name == "" {
			return nil, false, errors.New("binding kind and name are required, or supply script and binding_id")
		}
		value, err := runtime.InvokeBinding(ctx, opfor.BindingKind(request.Kind), request.Name, arguments...)
		if err != nil {
			return nil, false, err
		}
		return serveValueToJSON(value, make(map[any]struct{})), false, nil
	case "console":
		if request.Kind == "" || request.Name == "" {
			return nil, false, errors.New("console binding kind and name are required")
		}
		session := opfor.Null()
		if request.Session != nil {
			var err error
			session, err = serveJSONToValue(request.Session)
			if err != nil {
				return nil, false, fmt.Errorf("console session: %w", err)
			}
		}
		value, err := runtime.InvokeConsole(ctx, opfor.ConsoleInvocation{
			Kind:      opfor.BindingKind(request.Kind),
			Name:      request.Name,
			RawInput:  request.Raw,
			SessionID: session,
		})
		if err != nil {
			return nil, false, err
		}
		return serveValueToJSON(value, make(map[any]struct{})), false, nil
	case "call":
		if request.Name == "" {
			return nil, false, errors.New("script function name is empty")
		}
		script, err := session.resolveCallTarget(request)
		if err != nil {
			return nil, false, err
		}
		value, err := script.Call(ctx, request.Name, arguments...)
		if err != nil {
			return nil, false, err
		}
		return serveValueToJSON(value, make(map[any]struct{})), false, nil
	case "invoke":
		if request.Name == "" {
			return nil, false, errors.New("function name is empty")
		}
		value, err := runtime.Invoke(ctx, request.Name, arguments...)
		if err != nil {
			return nil, false, err
		}
		return serveValueToJSON(value, make(map[any]struct{})), false, nil
	case "bindings":
		if request.Kind == "" {
			return nil, false, errors.New("binding kind is required")
		}
		bindings := runtime.Bindings(opfor.BindingKind(request.Kind), request.Name)
		result := make([]any, len(bindings))
		for index, binding := range bindings {
			result[index] = serveBindingToJSON(binding)
		}
		return result, false, nil
	case "shutdown":
		return "bye", true, nil
	default:
		return nil, false, fmt.Errorf("unknown request method %q", request.Method)
	}
}

func serveControlScript(runtime persistentScriptRuntime, request serveRequest, method string) (*opfor.Script, error) {
	if !request.scriptSet {
		return nil, fmt.Errorf("%s requires script", method)
	}
	if request.Script == 0 {
		return nil, fmt.Errorf("%s script must be a positive integer", method)
	}
	return runtime.ScriptByID(opfor.ScriptID(request.Script))
}

func serveBindingToJSON(binding opfor.Binding) map[string]any {
	selectors := make([]any, len(binding.Selectors))
	for index, selector := range binding.Selectors {
		entry := map[string]any{
			"raw":       selector.Raw,
			"evaluated": selector.Evaluated,
			"span":      serveSpanToJSON(selector.Span),
		}
		if selector.Evaluated {
			entry["value"] = serveValueToJSON(selector.Value, make(map[any]struct{}))
		}
		selectors[index] = entry
	}
	result := map[string]any{
		"id":          binding.ID,
		"kind":        string(binding.Kind),
		"keyword":     binding.Keyword,
		"environment": serveEnvironmentKind(binding.Environment),
		"lifetime":    serveBindingLifetime(binding.Lifetime),
		"name":        binding.Name,
		"script":      uint64(binding.Script),
		"span":        serveSpanToJSON(binding.Span),
		"selectors":   selectors,
		"filter":      binding.Filter,
		"predicate":   binding.Predicate != nil,
	}
	if binding.Parent != nil {
		result["parent"] = serveBindingInvocationToJSON(binding.Parent)
	}
	return result
}

func serveBindingLifetime(lifetime opfor.BindingLifetime) string {
	switch lifetime {
	case opfor.BindingPersistent:
		return "persistent"
	case opfor.BindingOnce:
		return "once"
	default:
		return fmt.Sprintf("unknown(%d)", lifetime)
	}
}

func serveEnvironmentKind(kind opfor.EnvironmentKind) string {
	switch kind {
	case opfor.EnvironmentOrdinary:
		return "ordinary"
	case opfor.EnvironmentFilter:
		return "filter"
	case opfor.EnvironmentPredicate:
		return "predicate"
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}

func serveBindingInvocationToJSON(invocation *opfor.BindingInvocation) any {
	if invocation == nil {
		return nil
	}
	result := map[string]any{
		"binding_id": invocation.BindingID,
		"kind":       string(invocation.Kind),
		"keyword":    invocation.Keyword,
		"name":       invocation.Name,
		"script":     uint64(invocation.Script),
		"args":       serveValuesToJSON(invocation.Arguments),
	}
	if invocation.Parent != nil {
		result["parent"] = serveBindingInvocationToJSON(invocation.Parent)
	}
	return result
}

func serveSpanToJSON(span opfor.Span) map[string]any {
	return map[string]any{
		"source": span.Source,
		"start": map[string]any{
			"offset": span.Start.Offset,
			"line":   span.Start.Line,
			"column": span.Start.Column,
		},
		"end": map[string]any{
			"offset": span.End.Offset,
			"line":   span.End.Line,
			"column": span.End.Column,
		},
	}
}

func serveJSONToValue(input any) (opfor.Value, error) {
	switch value := input.(type) {
	case nil:
		return opfor.Null(), nil
	case bool:
		return opfor.Bool(value), nil
	case string:
		return opfor.String(value), nil
	case json.Number:
		if integer, err := strconv.ParseInt(string(value), 10, 64); err == nil {
			if integer >= math.MinInt32 && integer <= math.MaxInt32 {
				return opfor.Int(int32(integer)), nil
			}
			return opfor.Long(integer), nil
		}
		number, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return opfor.Null(), fmt.Errorf("invalid number %q", value)
		}
		return opfor.Double(number), nil
	case []any:
		values := make([]opfor.Value, len(value))
		for index, item := range value {
			converted, err := serveJSONToValue(item)
			if err != nil {
				return opfor.Null(), err
			}
			values[index] = converted
		}
		return opfor.ArrayValue(opfor.NewArray(values...)), nil
	case map[string]any:
		if tag, tagged := value[serveValueTag].(string); tagged {
			switch tag {
			case "binary":
				encoded, ok := value["base64"].(string)
				if !ok {
					return opfor.Null(), errors.New("binary value requires a base64 string")
				}
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return opfor.Null(), fmt.Errorf("invalid binary base64: %w", err)
				}
				return opfor.BinaryString(decoded), nil
			default:
				return opfor.Null(), fmt.Errorf("unsupported tagged value %q", tag)
			}
		}
		hash := opfor.NewHash()
		for key, item := range value {
			converted, err := serveJSONToValue(item)
			if err != nil {
				return opfor.Null(), err
			}
			hash.Set(key, converted)
		}
		return opfor.HashValue(hash), nil
	default:
		return opfor.Null(), fmt.Errorf("unsupported JSON value %T", input)
	}
}

func serveValuesToJSON(values []opfor.Value) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = serveValueToJSON(value, make(map[any]struct{}))
	}
	return result
}

func serveValueToJSON(value opfor.Value, seen map[any]struct{}) any {
	switch value.Kind() {
	case opfor.KindNull:
		return nil
	case opfor.KindInt:
		return value.Int32()
	case opfor.KindLong:
		return value.Int64()
	case opfor.KindDouble:
		number := value.Float64()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return map[string]any{serveValueTag: "double", "value": value.String()}
		}
		return number
	case opfor.KindString:
		if value.IsBinaryString() {
			data, _ := value.Bytes()
			return map[string]any{serveValueTag: "binary", "base64": base64.StdEncoding.EncodeToString(data)}
		}
		return value.String()
	case opfor.KindArray:
		array, _ := value.Array()
		if _, cycle := seen[array]; cycle {
			return map[string]any{serveValueTag: "cycle", "value": value.Describe()}
		}
		seen[array] = struct{}{}
		defer delete(seen, array)
		values := array.Values()
		result := make([]any, len(values))
		for index, item := range values {
			result[index] = serveValueToJSON(item, seen)
		}
		return result
	case opfor.KindHash:
		hash, _ := value.Hash()
		if _, cycle := seen[hash]; cycle {
			return map[string]any{serveValueTag: "cycle", "value": value.Describe()}
		}
		seen[hash] = struct{}{}
		defer delete(seen, hash)
		result := make(map[string]any)
		for _, key := range hash.KeyValues() {
			item, ok := hash.GetValue(key)
			if ok {
				result[key.String()] = serveValueToJSON(item, seen)
			}
		}
		return result
	default:
		return map[string]any{serveValueTag: "scalar", "kind": value.Kind().String(), "value": value.String()}
	}
}
