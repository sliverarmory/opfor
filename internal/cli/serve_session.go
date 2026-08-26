package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sliverarmory/opfor"
)

// serveSession owns the controller-visible lifecycle layered over one OPFOR
// Runtime. Runtime remains authoritative for execution and unload; this index
// only supplies stable path lookup, reload templates, and the legacy primary
// script used by untargeted call requests.
type serveSession struct {
	runtime        persistentScriptRuntime
	dependencies   dependencies
	maxSourceBytes uint64
	scripts        []*serveScript
	primary        opfor.ScriptID
	primarySet     bool
}

type serveScript struct {
	script         *opfor.Script
	path           string
	normalizedPath string
	argumentsJSON  json.RawMessage
}

func newServeSession(runtime persistentScriptRuntime, dependencies dependencies, maxSourceBytes ...uint64) *serveSession {
	var limit uint64
	if len(maxSourceBytes) != 0 {
		limit = maxSourceBytes[0]
	}
	return &serveSession{runtime: runtime, dependencies: dependencies, maxSourceBytes: limit}
}

func (session *serveSession) adoptStartup(script *opfor.Script, path string, argumentsJSON json.RawMessage) error {
	if session == nil || script == nil {
		return errors.New("serve startup script is unavailable")
	}
	normalized, err := normalizeServePath(path)
	if err != nil {
		return err
	}
	entry := &serveScript{
		script:         script,
		path:           path,
		normalizedPath: normalized,
		argumentsJSON:  cloneServeJSON(argumentsJSON),
	}
	session.scripts = append(session.scripts, entry)
	session.primary = script.ID()
	session.primarySet = true
	return nil
}

func (session *serveSession) load(ctx context.Context, path string, argumentsJSON json.RawMessage) (any, error) {
	program, normalized, err := session.compile(path)
	if err != nil {
		return nil, err
	}
	values, err := serveArgumentsFromJSON(argumentsJSON)
	if err != nil {
		return nil, err
	}
	script, err := session.runtime.Load(ctx, program, values...)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	entry := &serveScript{
		script:         script,
		path:           path,
		normalizedPath: normalized,
		argumentsJSON:  cloneServeJSON(argumentsJSON),
	}
	session.reconcile()
	session.scripts = append(session.scripts, entry)
	if !session.primarySet {
		session.primary = script.ID()
		session.primarySet = true
	}
	return session.scriptJSON(entry), nil
}

func (session *serveSession) reload(ctx context.Context, request serveRequest) (any, bool, error) {
	entry, err := session.resolve(request.Script, request.Path, true)
	if err != nil {
		return nil, false, err
	}

	// Reading and compiling the candidate is deliberately non-destructive. A
	// controller can repair the file and retry while the old callbacks remain
	// live. Argument validation likewise happens before unload.
	program, normalized, err := session.compile(entry.path)
	if err != nil {
		return nil, false, err
	}
	argumentsJSON := entry.argumentsJSON
	if request.argsSet {
		argumentsJSON = serveRequestArgumentsJSON(request)
	}
	values, err := serveArgumentsFromJSON(argumentsJSON)
	if err != nil {
		return nil, false, err
	}

	oldID := entry.script.ID()
	wasPrimary := session.primary == oldID
	unloadErr := entry.script.Unload(ctx)
	session.reconcile()
	if unloadErr != nil {
		return nil, false, fmt.Errorf("unload script %d before reload: %w", oldID, unloadErr)
	}

	// The old script is now irrevocably gone. Runtime.Load may execute
	// importer-visible top-level effects before returning an error, so neither
	// the old script nor an attempted replacement is synthesized as rollback.
	script, loadErr := session.runtime.Load(ctx, program, values...)
	if loadErr != nil {
		return nil, false, fmt.Errorf("load %s: %w", entry.path, loadErr)
	}
	replacement := &serveScript{
		script:         script,
		path:           entry.path,
		normalizedPath: normalized,
		argumentsJSON:  cloneServeJSON(argumentsJSON),
	}
	session.scripts = append(session.scripts, replacement)
	sort.SliceStable(session.scripts, func(i, j int) bool {
		return session.scripts[i].script.ID() < session.scripts[j].script.ID()
	})
	if wasPrimary {
		session.primary = script.ID()
	}
	return session.scriptJSON(replacement), false, nil
}

func (session *serveSession) unload(ctx context.Context, request serveRequest) (any, bool, error) {
	entry, err := session.resolve(request.Script, request.Path, true)
	if err != nil {
		return nil, false, err
	}
	// Snapshot before unload so the response uses the same metadata contract as
	// load/reload/list, including whether this was the primary script.
	result := session.scriptJSON(entry)
	if err := entry.script.Unload(ctx); err != nil {
		session.reconcile()
		return nil, false, fmt.Errorf("unload script %d: %w", entry.script.ID(), err)
	}
	session.reconcile()
	return result, false, nil
}

func (session *serveSession) list() []any {
	session.reconcile()
	result := make([]any, len(session.scripts))
	for index, entry := range session.scripts {
		result[index] = session.scriptJSON(entry)
	}
	return result
}

func (session *serveSession) resolveCallTarget(request serveRequest) (*opfor.Script, error) {
	entry, err := session.resolve(request.Script, request.Path, false)
	if err != nil {
		return nil, err
	}
	return entry.script, nil
}

func (session *serveSession) resolve(id uint64, path string, lifecycle bool) (*serveScript, error) {
	session.reconcile()
	if id != 0 {
		for _, entry := range session.scripts {
			if uint64(entry.script.ID()) == id {
				return entry, nil
			}
		}
		return nil, fmt.Errorf("script %d is not loaded", id)
	}
	if path != "" {
		return session.resolvePath(path)
	}
	if session.primary != 0 {
		for _, entry := range session.scripts {
			if entry.script.ID() == session.primary {
				return entry, nil
			}
		}
		session.primary = 0
	}
	if lifecycle {
		return nil, errors.New("no primary script is loaded; supply script or path")
	}
	// Preserve the pre-multi-script call error for a zero-startup service and
	// for a service whose primary script was unloaded.
	return nil, errors.New("script function calls are unavailable")
}

func (session *serveSession) resolvePath(path string) (*serveScript, error) {
	exact := make([]*serveScript, 0, 1)
	for _, entry := range session.scripts {
		if entry.path == path {
			exact = append(exact, entry)
		}
	}
	if len(exact) != 0 {
		return oneServePathMatch(path, exact)
	}
	normalized, err := normalizeServePath(path)
	if err != nil {
		return nil, err
	}
	matches := make([]*serveScript, 0, 1)
	for _, entry := range session.scripts {
		if entry.normalizedPath == normalized {
			matches = append(matches, entry)
		}
	}
	return oneServePathMatch(path, matches)
}

func oneServePathMatch(path string, matches []*serveScript) (*serveScript, error) {
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("script path %q is not loaded", path)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for index, entry := range matches {
			ids[index] = fmt.Sprint(entry.script.ID())
		}
		return nil, fmt.Errorf(
			"script path %q is ambiguous; matching script IDs: %s",
			path,
			strings.Join(ids, ", "),
		)
	}
}

func (session *serveSession) reconcile() {
	if session == nil || len(session.scripts) == 0 {
		return
	}
	active := session.scripts[:0]
	for _, entry := range session.scripts {
		if entry == nil || entry.script == nil || !entry.script.Active() {
			if entry != nil && entry.script != nil && entry.script.ID() == session.primary {
				session.primary = 0
			}
			continue
		}
		active = append(active, entry)
	}
	session.scripts = active
}

func (session *serveSession) compile(path string) (*opfor.Program, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", errors.New("script path is empty")
	}
	if path == "-" {
		return nil, "", errors.New("serve requires a filesystem script path; standard input is reserved for protocol requests")
	}
	if err := validateScriptPath(path); err != nil {
		return nil, "", err
	}
	normalized, err := normalizeServePath(path)
	if err != nil {
		return nil, "", err
	}
	program, err := compileFile(session.dependencies, path, strings.NewReader(""), session.maxSourceBytes)
	if err != nil {
		return nil, "", err
	}
	return program, normalized, nil
}

func (session *serveSession) scriptJSON(entry *serveScript) map[string]any {
	arguments := cloneServeJSON(entry.argumentsJSON)
	if len(arguments) == 0 {
		arguments = json.RawMessage("[]")
	}
	return map[string]any{
		"id":              uint64(entry.script.ID()),
		"path":            entry.path,
		"normalized_path": entry.normalizedPath,
		"primary":         entry.script.ID() == session.primary,
		"args":            arguments,
	}
}

func normalizeServePath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("normalize script path %q: %w", path, err)
	}
	return filepath.Clean(absolute), nil
}

func serveRequestArgumentsJSON(request serveRequest) json.RawMessage {
	if request.argsSet {
		return cloneServeJSON(request.argsJSON)
	}
	return json.RawMessage("[]")
}

func serveArgumentsFromJSON(argumentsJSON json.RawMessage) ([]opfor.Value, error) {
	if len(bytes.TrimSpace(argumentsJSON)) == 0 {
		argumentsJSON = json.RawMessage("[]")
	}
	decoder := json.NewDecoder(bytes.NewReader(argumentsJSON))
	decoder.UseNumber()
	var arguments []any
	if err := decoder.Decode(&arguments); err != nil {
		return nil, fmt.Errorf("invalid script argument template: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("invalid script argument template: multiple values")
		}
		return nil, fmt.Errorf("invalid script argument template: %w", err)
	}
	values := make([]opfor.Value, len(arguments))
	for index, argument := range arguments {
		value, err := serveJSONToValue(argument)
		if err != nil {
			return nil, fmt.Errorf("script argument %d: %w", index+1, err)
		}
		values[index] = value
	}
	return values, nil
}

func cloneServeJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
