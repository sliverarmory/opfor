package opfor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type aggressorBeaconActionTestSpec struct {
	name             string
	kind             AggressorBeaconActionKind
	minimum          int
	maximum          int
	hasCallback      bool
	callbackIndex    int
	callbackRequired bool
}

var aggressorBeaconActionTestSpecs = []aggressorBeaconActionTestSpec{
	{name: "bcd", kind: AggressorBeaconActionChangeDirectory, minimum: 2, maximum: 2},
	{name: "bdownload", kind: AggressorBeaconActionDownload, minimum: 2, maximum: 2},
	{name: "bexecute", kind: AggressorBeaconActionExecute, minimum: 2, maximum: 2},
	{name: "bls", kind: AggressorBeaconActionListFiles, minimum: 1, maximum: 3, hasCallback: true, callbackIndex: 2},
	{name: "bpowershell", kind: AggressorBeaconActionPowerShell, minimum: 2, maximum: 4, hasCallback: true, callbackIndex: 3},
	{name: "bps", kind: AggressorBeaconActionListProcesses, minimum: 1, maximum: 2, hasCallback: true, callbackIndex: 1},
	{name: "bpwd", kind: AggressorBeaconActionPrintWorkingDirectory, minimum: 1, maximum: 1},
	{name: "brm", kind: AggressorBeaconActionRemove, minimum: 2, maximum: 2},
	{name: "bshell", kind: AggressorBeaconActionShell, minimum: 2, maximum: 2},
	{name: "bhashdump", kind: AggressorBeaconActionHashdump, minimum: 1, maximum: 4, hasCallback: true, callbackIndex: 3},
	{name: "bnet", kind: AggressorBeaconActionNet, minimum: 4, maximum: 7, hasCallback: true, callbackIndex: 6},
	{name: "bpowershell_import_clear", kind: AggressorBeaconActionPowerShellImportClear, minimum: 1, maximum: 1},
	{name: "bpowerpick", kind: AggressorBeaconActionPowerPick, minimum: 2, maximum: 5, hasCallback: true, callbackIndex: 4},
	{name: "bpsinject", kind: AggressorBeaconActionPowerShellInject, minimum: 4, maximum: 5, hasCallback: true, callbackIndex: 4},
	{name: "bmimikatz", kind: AggressorBeaconActionMimikatz, minimum: 2, maximum: 5, hasCallback: true, callbackIndex: 4},
	{name: "bmimikatz_small", kind: AggressorBeaconActionMimikatzSmall, minimum: 2, maximum: 5, hasCallback: true, callbackIndex: 4},
	{name: "bportscan", kind: AggressorBeaconActionPortscan, minimum: 5, maximum: 8, hasCallback: true, callbackIndex: 7},
	{name: "bdllspawn", kind: AggressorBeaconActionDLLSpawn, minimum: 6, maximum: 7, hasCallback: true, callbackIndex: 6},
	{name: "bexecute_assembly", kind: AggressorBeaconActionExecuteAssembly, minimum: 3, maximum: 5, hasCallback: true, callbackIndex: 4},
	{name: "binline_execute", kind: AggressorBeaconActionInlineExecute, minimum: 3, maximum: 4, hasCallback: true, callbackIndex: 3},
	{name: "bdllinject", kind: AggressorBeaconActionDLLInject, minimum: 3, maximum: 3},
	{name: "bread_pipe", kind: AggressorBeaconActionReadPipe, minimum: 7, maximum: 8, hasCallback: true, callbackIndex: 7},
	{name: "bcp", kind: AggressorBeaconActionCopy, minimum: 3, maximum: 3},
	{name: "bdrives", kind: AggressorBeaconActionListDrives, minimum: 1, maximum: 1},
	{name: "bmkdir", kind: AggressorBeaconActionMakeDirectory, minimum: 2, maximum: 2},
	{name: "bmv", kind: AggressorBeaconActionMove, minimum: 3, maximum: 3},
	{name: "btimestomp", kind: AggressorBeaconActionTimestomp, minimum: 3, maximum: 3},
	{name: "bupload", kind: AggressorBeaconActionUpload, minimum: 2, maximum: 2},
	{name: "bupload_raw", kind: AggressorBeaconActionUploadRaw, minimum: 3, maximum: 4},
	{name: "bargue_add", kind: AggressorBeaconActionArgumentSpoofAdd, minimum: 3, maximum: 3},
	{name: "bargue_list", kind: AggressorBeaconActionArgumentSpoofList, minimum: 1, maximum: 1},
	{name: "bargue_remove", kind: AggressorBeaconActionArgumentSpoofRemove, minimum: 2, maximum: 2},
	{name: "bbeacon_config", kind: AggressorBeaconActionConfigure, minimum: 2, maximum: 6},
	{name: "bbeacon_gate", kind: AggressorBeaconActionGate, minimum: 2, maximum: 2},
	{name: "bbeacon_interpreter", kind: AggressorBeaconActionInterpreter, minimum: 2, maximum: 4, hasCallback: true, callbackIndex: 3},
	{name: "bbeacon_interpreter_lint", kind: AggressorBeaconActionInterpreterLint, minimum: 2, maximum: 3, hasCallback: true, callbackIndex: 2},
	{name: "bblockdlls", kind: AggressorBeaconActionBlockDLLs, minimum: 2, maximum: 2},
	{name: "bbrowserpivot", kind: AggressorBeaconActionBrowserPivot, minimum: 3, maximum: 3},
	{name: "bbrowserpivot_stop", kind: AggressorBeaconActionBrowserPivotStop, minimum: 1, maximum: 1},
	{name: "bcancel", kind: AggressorBeaconActionCancelDownload, minimum: 2, maximum: 2},
	{name: "bcheckin", kind: AggressorBeaconActionCheckin, minimum: 1, maximum: 1},
	{name: "bclear", kind: AggressorBeaconActionClearTasks, minimum: 1, maximum: 1},
	{name: "bclipboard", kind: AggressorBeaconActionClipboard, minimum: 1, maximum: 1},
	{name: "bconnect", kind: AggressorBeaconActionConnect, minimum: 2, maximum: 3},
	{name: "bcovertvpn", kind: AggressorBeaconActionCovertVPN, minimum: 3, maximum: 4},
	{name: "bdata_store_list", kind: AggressorBeaconActionDataStoreList, minimum: 1, maximum: 1},
	{name: "bdata_store_load", kind: AggressorBeaconActionDataStoreLoad, minimum: 3, maximum: 4},
	{name: "bdata_store_unload", kind: AggressorBeaconActionDataStoreUnload, minimum: 2, maximum: 2},
	{name: "bdcsync", kind: AggressorBeaconActionDCSync, minimum: 2, maximum: 5},
	{name: "bdesktop", kind: AggressorBeaconActionDesktop, minimum: 1, maximum: 1},
	{name: "bdllload", kind: AggressorBeaconActionDLLLoad, minimum: 3, maximum: 3},
	{name: "bexit", kind: AggressorBeaconActionExit, minimum: 1, maximum: 1},
	{name: "bgetprivs", kind: AggressorBeaconActionEnablePrivileges, minimum: 2, maximum: 2},
	{name: "bgetsystem", kind: AggressorBeaconActionGetSystem, minimum: 1, maximum: 1},
	{name: "bgetuid", kind: AggressorBeaconActionGetUID, minimum: 1, maximum: 1},
	{name: "binject", kind: AggressorBeaconActionInject, minimum: 3, maximum: 4},
	{name: "binline_execute_pe", kind: AggressorBeaconActionInlineExecutePE, minimum: 3, maximum: 4, hasCallback: true, callbackIndex: 3},
	{name: "bipconfig", kind: AggressorBeaconActionIPConfig, minimum: 2, maximum: 2, hasCallback: true, callbackIndex: 1, callbackRequired: true},
	{name: "bjob_send_data", kind: AggressorBeaconActionJobSendData, minimum: 3, maximum: 3},
	{name: "bjobkill", kind: AggressorBeaconActionJobKill, minimum: 2, maximum: 2},
	{name: "bjobs", kind: AggressorBeaconActionJobs, minimum: 1, maximum: 1},
	{name: "bkerberos_ccache_use", kind: AggressorBeaconActionKerberosCCacheUse, minimum: 2, maximum: 2},
	{name: "bkerberos_ticket_purge", kind: AggressorBeaconActionKerberosTicketPurge, minimum: 1, maximum: 1},
	{name: "bkerberos_ticket_use", kind: AggressorBeaconActionKerberosTicketUse, minimum: 2, maximum: 2},
	{name: "bkeylogger", kind: AggressorBeaconActionKeylogger, minimum: 1, maximum: 3},
	{name: "bkill", kind: AggressorBeaconActionKill, minimum: 2, maximum: 2},
	{name: "blink", kind: AggressorBeaconActionLink, minimum: 2, maximum: 3},
	{name: "bloginuser", kind: AggressorBeaconActionLoginUser, minimum: 4, maximum: 4},
	{name: "blogonpasswords", kind: AggressorBeaconActionLogonPasswords, minimum: 1, maximum: 3},
	{name: "bmode", kind: AggressorBeaconActionMode, minimum: 2, maximum: 2},
	{name: "bnote", kind: AggressorBeaconActionNote, minimum: 2, maximum: 2},
	{name: "bpassthehash", kind: AggressorBeaconActionPassTheHash, minimum: 4, maximum: 6},
	{name: "bpause", kind: AggressorBeaconActionPause, minimum: 2, maximum: 2},
	{name: "bpowershell_import", kind: AggressorBeaconActionPowerShellImport, minimum: 2, maximum: 2},
	{name: "bppid", kind: AggressorBeaconActionParentPID, minimum: 2, maximum: 2},
	{name: "bprintscreen", kind: AggressorBeaconActionPrintScreen, minimum: 1, maximum: 3},
	{name: "bpsexec", kind: AggressorBeaconActionPSExec, minimum: 4, maximum: 5},
	{name: "bpsexec_command", kind: AggressorBeaconActionPSExecCommand, minimum: 4, maximum: 4},
	{name: "breg_queryv", kind: AggressorBeaconActionRegistryQueryValue, minimum: 4, maximum: 4},
	{name: "brev2self", kind: AggressorBeaconActionRevertToSelf, minimum: 1, maximum: 1},
	{name: "brportfwd", kind: AggressorBeaconActionReversePortForward, minimum: 4, maximum: 4},
	{name: "brportfwd_local", kind: AggressorBeaconActionReversePortForwardLocal, minimum: 4, maximum: 4},
	{name: "brportfwd_stop", kind: AggressorBeaconActionReversePortForwardStop, minimum: 2, maximum: 2},
	{name: "brun", kind: AggressorBeaconActionRun, minimum: 2, maximum: 2},
	{name: "brunas", kind: AggressorBeaconActionRunAs, minimum: 5, maximum: 5},
	{name: "brunu", kind: AggressorBeaconActionRunUnder, minimum: 3, maximum: 3},
	{name: "bscreenshot", kind: AggressorBeaconActionScreenshot, minimum: 1, maximum: 3},
	{name: "bscreenwatch", kind: AggressorBeaconActionScreenwatch, minimum: 1, maximum: 3},
	{name: "bsetenv", kind: AggressorBeaconActionSetEnvironment, minimum: 3, maximum: 3},
	{name: "bshinject", kind: AggressorBeaconActionShellcodeInject, minimum: 4, maximum: 4},
	{name: "bshspawn", kind: AggressorBeaconActionShellcodeSpawn, minimum: 3, maximum: 3},
	{name: "bsleep", kind: AggressorBeaconActionSleep, minimum: 3, maximum: 3},
	{name: "bsleepu", kind: AggressorBeaconActionSleepUnified, minimum: 2, maximum: 2},
	{name: "bsocks", kind: AggressorBeaconActionSOCKS, minimum: 2, maximum: 7},
	{name: "bsocks_stop", kind: AggressorBeaconActionSOCKSStop, minimum: 1, maximum: 1},
	{name: "bspawn", kind: AggressorBeaconActionSpawn, minimum: 2, maximum: 3},
	{name: "bspawnas", kind: AggressorBeaconActionSpawnAs, minimum: 5, maximum: 5},
	{name: "bspawnto", kind: AggressorBeaconActionSpawnTo, minimum: 3, maximum: 3},
	{name: "bspawnu", kind: AggressorBeaconActionSpawnUnder, minimum: 3, maximum: 3},
	{name: "bspunnel", kind: AggressorBeaconActionSpawnTunnel, minimum: 5, maximum: 5},
	{name: "bspunnel_local", kind: AggressorBeaconActionSpawnTunnelLocal, minimum: 5, maximum: 5},
	{name: "bssh", kind: AggressorBeaconActionSSH, minimum: 5, maximum: 7},
	{name: "bssh_key", kind: AggressorBeaconActionSSHKey, minimum: 5, maximum: 7},
	{name: "bsteal_token", kind: AggressorBeaconActionStealToken, minimum: 2, maximum: 3},
	{name: "bsudo", kind: AggressorBeaconActionSudo, minimum: 3, maximum: 3},
	{name: "bsyscall_method", kind: AggressorBeaconActionSyscallMethod, minimum: 2, maximum: 2},
	{name: "btoken_store_remove", kind: AggressorBeaconActionTokenStoreRemove, minimum: 2, maximum: 2},
	{name: "btoken_store_remove_all", kind: AggressorBeaconActionTokenStoreRemoveAll, minimum: 1, maximum: 1},
	{name: "btoken_store_show", kind: AggressorBeaconActionTokenStoreShow, minimum: 1, maximum: 1},
	{name: "btoken_store_steal", kind: AggressorBeaconActionTokenStoreSteal, minimum: 3, maximum: 3},
	{name: "btoken_store_steal_and_use", kind: AggressorBeaconActionTokenStoreStealAndUse, minimum: 3, maximum: 3},
	{name: "btoken_store_use", kind: AggressorBeaconActionTokenStoreUse, minimum: 2, maximum: 2},
	{name: "bunlink", kind: AggressorBeaconActionUnlink, minimum: 2, maximum: 3},
	{name: "beacon_console_watermark", kind: AggressorBeaconActionConsoleWatermark, minimum: 2, maximum: 2},
	{name: "beacon_console_watermark_reset", kind: AggressorBeaconActionConsoleWatermarkReset, minimum: 1, maximum: 1},
	{name: "beacon_job_hide_output", kind: AggressorBeaconActionJobHideOutput, minimum: 3, maximum: 3},
	{name: "beacon_job_name", kind: AggressorBeaconActionJobName, minimum: 3, maximum: 3},
	{name: "beacon_link", kind: AggressorBeaconActionSmartLink, minimum: 3, maximum: 3},
	{name: "beacon_remove", kind: AggressorBeaconActionRemoveFromDisplay, minimum: 1, maximum: 1},
	{name: "beacon_stage_pipe", kind: AggressorBeaconActionStagePipe, minimum: 4, maximum: 4},
	{name: "beacon_stage_tcp", kind: AggressorBeaconActionStageTCP, minimum: 5, maximum: 5},
}

type recordingAggressorBeaconActionProvider struct {
	mu       sync.Mutex
	actions  []AggressorBeaconAction
	dispatch func(context.Context, AggressorBeaconAction) error
}

func (provider *recordingAggressorBeaconActionProvider) DispatchAggressorBeaconAction(
	ctx context.Context,
	action AggressorBeaconAction,
) error {
	provider.mu.Lock()
	provider.actions = append(provider.actions, action)
	dispatch := provider.dispatch
	provider.mu.Unlock()
	if dispatch == nil {
		return nil
	}
	return dispatch(ctx, action)
}

func (provider *recordingAggressorBeaconActionProvider) snapshot() []AggressorBeaconAction {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]AggressorBeaconAction(nil), provider.actions...)
}

type aggressorBeaconActionTestCallable func(context.Context, ...Value) (Value, error)

func (function aggressorBeaconActionTestCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return function(ctx, values...)
}

func TestAggressorBeaconActionFunctionSetAndDocumentedSpecs(t *testing.T) {
	t.Parallel()

	functions := (&Runtime{}).aggressorBeaconActionFunctions()
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	wantNames := make([]string, len(aggressorBeaconActionTestSpecs))
	for index, test := range aggressorBeaconActionTestSpecs {
		wantNames[index] = test.name
	}
	sort.Strings(wantNames)
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("Aggressor Beacon action names = %q, want %q", names, wantNames)
	}
	if len(aggressorBeaconActionSpecs) != len(aggressorBeaconActionTestSpecs) {
		t.Fatalf("Aggressor Beacon action specs = %d, want %d", len(aggressorBeaconActionSpecs), len(aggressorBeaconActionTestSpecs))
	}
	for _, test := range aggressorBeaconActionTestSpecs {
		spec, exists := aggressorBeaconActionSpecs[test.name]
		if !exists {
			t.Errorf("Aggressor Beacon action spec %q is missing", test.name)
			continue
		}
		want := aggressorBeaconActionSpec{
			kind:             test.kind,
			minimum:          test.minimum,
			maximum:          test.maximum,
			hasCallback:      test.hasCallback,
			callbackIndex:    test.callbackIndex,
			callbackRequired: test.callbackRequired,
		}
		if spec != want {
			t.Errorf("Aggressor Beacon action spec %q = %#v, want %#v", test.name, spec, want)
		}
		if string(test.kind) != test.name {
			t.Errorf("Aggressor Beacon action kind %q = %q, want stable function spelling", test.name, test.kind)
		}
	}
}

func TestAggressorBeaconActionArityRangesAndCallbackIndices(t *testing.T) {
	t.Parallel()

	for _, test := range aggressorBeaconActionTestSpecs {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var hostCalls atomic.Int32
			provider := &recordingAggressorBeaconActionProvider{}
			runtimeInstance, err := New(
				WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), errors.New("typed Beacon action reached Host")
				})),
				WithAggressorBeaconActionProvider(provider),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
			var callbackOwner *Script
			if test.callbackRequired {
				callbackOwner = aggressorBeaconActionTestOwner(t, runtimeInstance)
			}

			for count := test.minimum; count <= test.maximum; count++ {
				arguments := aggressorBeaconActionTestValues(test.name, count)
				wantState := AggressorCallbackOmitted
				if test.hasCallback && count == test.callbackIndex+1 {
					if test.callbackRequired {
						arguments[test.callbackIndex] = FunctionValue(aggressorBeaconActionTestCallable(func(context.Context, ...Value) (Value, error) {
							return Null(), nil
						}))
						wantState = AggressorCallbackCallable
					} else {
						arguments[test.callbackIndex] = Null()
						wantState = AggressorCallbackNull
					}
				}
				var result Value
				var invokeErr error
				if test.callbackRequired {
					result, invokeErr = runtimeInstance.aggressorBeaconAction(
						context.Background(),
						aggressorBeaconActionTestInvocation(runtimeInstance, callbackOwner.ID(), test.name, Span{}, arguments...),
					)
				} else {
					result, invokeErr = runtimeInstance.Invoke(context.Background(), test.name, arguments...)
				}
				if invokeErr != nil || !result.IsNull() {
					t.Errorf("arity %d = (%s, %v), want null/nil", count, result.Describe(), invokeErr)
					continue
				}
				actions := provider.snapshot()
				action := actions[len(actions)-1]
				if action.Name != test.name || action.Kind != test.kind || action.CallbackState != wantState {
					t.Errorf("arity %d action route/state = %q/%q/%v, want %q/%q/%v",
						count, action.Name, action.Kind, action.CallbackState, test.name, test.kind, wantState)
				}
				wantCallable := wantState == AggressorCallbackCallable
				if (action.Callback != nil) != wantCallable {
					t.Errorf("arity %d callback = %T, want callable=%v for state %v", count, action.Callback, wantCallable, wantState)
				}
				wantOrdinary := count - 1
				if wantState != AggressorCallbackOmitted {
					wantOrdinary--
				}
				if len(action.Arguments) != wantOrdinary {
					t.Errorf("arity %d ordinary arguments = %d, want %d", count, len(action.Arguments), wantOrdinary)
				}
			}

			validCalls := len(provider.snapshot())
			invalidCounts := []int{test.minimum - 1, test.maximum + 1}
			for _, count := range invalidCounts {
				if count < 0 {
					continue
				}
				result, invokeErr := runtimeInstance.Invoke(
					context.Background(), test.name, aggressorBeaconActionTestValues(test.name, count)...,
				)
				if invokeErr == nil || !result.IsNull() {
					t.Errorf("invalid arity %d = (%s, %v), want null/range error", count, result.Describe(), invokeErr)
					continue
				}
				wantError := fmt.Sprintf("expected %d to %d argument(s), received %d", test.minimum, test.maximum, count)
				if test.minimum == test.maximum {
					wantError = fmt.Sprintf("expected exactly %d argument(s), received %d", test.minimum, count)
				}
				if !strings.Contains(invokeErr.Error(), wantError) {
					t.Errorf("invalid arity %d error = %v, want %q", count, invokeErr, wantError)
				}
			}
			if got := len(provider.snapshot()); got != validCalls {
				t.Errorf("invalid arities added %d provider call(s)", got-validCalls)
			}

			if test.hasCallback {
				owner := aggressorBeaconActionTestOwner(t, runtimeInstance)
				callbackValue := FunctionValue(aggressorBeaconActionTestCallable(func(context.Context, ...Value) (Value, error) {
					return String("callback result"), nil
				}))
				arguments := aggressorBeaconActionTestValues(test.name+"-callable", test.maximum)
				arguments[test.callbackIndex] = callbackValue
				result, invokeErr := runtimeInstance.aggressorBeaconAction(
					context.Background(),
					aggressorBeaconActionTestInvocation(runtimeInstance, owner.ID(), test.name, Span{}, arguments...),
				)
				if invokeErr != nil || !result.IsNull() {
					t.Fatalf("callable callback = (%s, %v), want null/nil", result.Describe(), invokeErr)
				}
				actions := provider.snapshot()
				action := actions[len(actions)-1]
				if action.CallbackState != AggressorCallbackCallable || action.Callback == nil {
					t.Fatalf("callback state/capability = %v/%T, want Callable/non-nil", action.CallbackState, action.Callback)
				}
				if len(action.Arguments) != test.maximum-2 {
					t.Fatalf("callback at index %d was not excluded: ordinary arguments = %d, want %d",
						test.callbackIndex, len(action.Arguments), test.maximum-2)
				}

				badArguments := aggressorBeaconActionTestValues(test.name+"-invalid-callback", test.maximum)
				badArguments[test.callbackIndex] = String("not callable")
				before := len(provider.snapshot())
				result, invokeErr = runtimeInstance.aggressorBeaconAction(
					context.Background(),
					aggressorBeaconActionTestInvocation(runtimeInstance, owner.ID(), test.name, Span{}, badArguments...),
				)
				if !errors.Is(invokeErr, ErrInvalidCallable) || !result.IsNull() {
					t.Errorf("non-callable callback = (%s, %v), want null/ErrInvalidCallable", result.Describe(), invokeErr)
				}
				if got := len(provider.snapshot()); got != before {
					t.Errorf("non-callable callback reached provider %d time(s)", got-before)
				}
			}

			if got := hostCalls.Load(); got != 0 {
				t.Fatalf("configured provider route reached Host %d time(s)", got)
			}
		})
	}
}

func TestAggressorBIPConfigRequiresCallbackAndPreservesDocumentedABI(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorBeaconActionProvider{}
	runtimeInstance, err := New(WithAggressorBeaconActionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	program, err := CompileString("bipconfig-callback.cna", `
$calls = 0;
$seen_arity = 0;
$seen_bid = $null;
$seen_results = $null;
$seen_type = $null;
$ready = {
    $calls++;
    $seen_arity = size(@_);
    $seen_bid = $1;
    $seen_results = $2;
    $seen_type = $3["type"];
    return "accepted:" . $1;
};
sub issue { return bipconfig($1, $ready); }
sub issue_null { return bipconfig($1, $null); }
sub issue_scalar { return bipconfig($1, "not callable"); }
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}

	result, err := owner.Call(context.Background(), "issue", String("B-ipconfig"))
	if err != nil || !result.IsNull() {
		t.Fatalf("bipconfig = (%s, %v), want null/nil", result.Describe(), err)
	}
	actions := provider.snapshot()
	if len(actions) != 1 {
		t.Fatalf("bipconfig provider actions = %d, want one", len(actions))
	}
	action := actions[0]
	if action.Kind != AggressorBeaconActionIPConfig || action.Name != "bipconfig" ||
		action.RuntimeID != runtimeInstance.ID() || action.Script != owner.ID() ||
		action.Span.Source != "bipconfig-callback.cna" || action.Target.String() != "B-ipconfig" ||
		len(action.Arguments) != 0 || action.CallbackState != AggressorCallbackCallable || action.Callback == nil {
		t.Fatalf("bipconfig action = %#v", action)
	}

	information := NewOrderedHash()
	information.Set("type", String("output"))
	callbackResult, callbackErr := action.Callback.Invoke(
		context.Background(), action.Target, String("interface results"), HashValue(information),
	)
	if callbackErr != nil || callbackResult.String() != "accepted:B-ipconfig" {
		t.Fatalf("bipconfig callback = (%s, %v)", callbackResult.Describe(), callbackErr)
	}
	if owner.Get("$calls").Int32() != 1 || owner.Get("$seen_arity").Int32() != 3 ||
		owner.Get("$seen_bid").String() != "B-ipconfig" ||
		owner.Get("$seen_results").String() != "interface results" ||
		owner.Get("$seen_type").String() != "output" {
		t.Fatalf("bipconfig callback ABI = calls:%d arity:%d bid:%s results:%s type:%s",
			owner.Get("$calls").Int32(), owner.Get("$seen_arity").Int32(),
			owner.Get("$seen_bid").Describe(), owner.Get("$seen_results").Describe(),
			owner.Get("$seen_type").Describe())
	}

	for _, name := range []string{"issue_null", "issue_scalar"} {
		result, invokeErr := owner.Call(context.Background(), name, String("B-invalid"))
		if !result.IsNull() || !errors.Is(invokeErr, ErrInvalidCallable) ||
			!strings.Contains(invokeErr.Error(), "&bipconfig: argument 2 is not callable") {
			t.Errorf("%s = (%s, %v), want null/ErrInvalidCallable", name, result.Describe(), invokeErr)
		}
	}
	if got := len(provider.snapshot()); got != 1 {
		t.Fatalf("invalid bipconfig callbacks reached provider: actions = %d, want one", got)
	}

	if err := owner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	callbackResult, callbackErr = action.Callback.Invoke(
		context.Background(), String("B-after-unload"), String("ignored"), HashValue(NewHash()),
	)
	if !callbackResult.IsNull() || !errors.Is(callbackErr, ErrScriptUnloaded) {
		t.Fatalf("bipconfig callback after unload = (%s, %v), want null/ErrScriptUnloaded",
			callbackResult.Describe(), callbackErr)
	}
}

func TestAggressorBeaconActionRequestMetadataAndValueIdentity(t *testing.T) {
	t.Parallel()

	target := ArrayValue(NewArray(String("B-identity")))
	payloadHash := NewOrderedHash()
	payloadHash.Set("opaque", ObjectValue(&struct{ label string }{"payload"}))
	payload := HashValue(payloadHash)
	targetCell := NewCell(target)
	payloadCell := NewCell(payload)
	span := Span{Source: "beacon-action-identity.cna", Start: Position{Line: 17, Column: 9}}
	var captured AggressorBeaconAction
	provider := AggressorBeaconActionProviderFunc(func(_ context.Context, action AggressorBeaconAction) error {
		captured = action
		return nil
	})
	runtimeInstance, err := New(WithAggressorBeaconActionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	invocation := Invocation{
		Runtime: runtimeInstance,
		Script:  41,
		Name:    "bexecute",
		Span:    span,
		Arguments: []Argument{
			{Name: "$target", Reference: targetCell},
			{Name: "$payload", Reference: payloadCell},
		},
	}

	result, err := runtimeInstance.aggressorBeaconAction(context.Background(), invocation)
	if err != nil || !result.IsNull() {
		t.Fatalf("typed request = (%s, %v), want null/nil", result.Describe(), err)
	}
	if captured.Kind != AggressorBeaconActionExecute || captured.Name != invocation.Name ||
		captured.RuntimeID != runtimeInstance.ID() || captured.RuntimeID == 0 ||
		captured.Script != invocation.Script || captured.Span != span {
		t.Fatalf("request metadata = %#v", captured)
	}
	if !captured.Target.IdentityEqual(target) {
		t.Fatalf("Target = %s, want original compound identity %s", captured.Target.Describe(), target.Describe())
	}
	if len(captured.Arguments) != 1 || !captured.Arguments[0].IdentityEqual(payload) {
		t.Fatalf("Arguments = %v, want original payload identity", describeAggressorBeaconActionValues(captured.Arguments))
	}
	if captured.CallbackState != AggressorCallbackOmitted || captured.Callback != nil {
		t.Fatalf("non-callback request state = %v/%T, want Omitted/nil", captured.CallbackState, captured.Callback)
	}

	// The provider receives resolved Value snapshots, not the source Cells.
	targetCell.Set(String("new target"))
	payloadCell.Set(String("new payload"))
	invocation.Arguments[0] = Argument{Value: String("replacement")}
	invocation.Arguments[1] = Argument{Value: String("replacement")}
	if !captured.Target.IdentityEqual(target) || !captured.Arguments[0].IdentityEqual(payload) {
		t.Fatal("retained request was rewritten through the original Invocation or Cells")
	}
}

func TestAggressorBeaconFilesystemActionsKeepArrayTargetAndUploadInputsOpaque(t *testing.T) {
	t.Parallel()

	target := ArrayValue(NewArray(String("B-one"), String("B-two")))
	nonexistent := filepath.Join(t.TempDir(), "OPFOR-must-not-read-this-file.bin")
	raw := BinaryString([]byte{0x00, 0xff, 'M', 'Z'})
	localMetadata := ObjectValue(&struct{ source string }{source: "importer-owned"})
	provider := &recordingAggressorBeaconActionProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("typed Beacon filesystem action reached Host")
		})),
		WithAggressorBeaconActionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	if result, err := runtimeInstance.Invoke(context.Background(), "bupload", target, String(nonexistent)); err != nil || !result.IsNull() {
		t.Fatalf("bupload nonexistent local path = (%s, %v), want null/nil importer dispatch", result.Describe(), err)
	}
	if result, err := runtimeInstance.Invoke(
		context.Background(), "bupload_raw", target, String(`C:\Temp\payload.bin`), raw, localMetadata,
	); err != nil || !result.IsNull() {
		t.Fatalf("bupload_raw = (%s, %v), want null/nil", result.Describe(), err)
	}

	actions := provider.snapshot()
	if len(actions) != 2 || hostCalls.Load() != 0 {
		t.Fatalf("filesystem provider/Host calls = %d/%d, want 2/0", len(actions), hostCalls.Load())
	}
	for index, action := range actions {
		if !action.Target.IdentityEqual(target) {
			t.Errorf("action %d Target = %s, want one unflattened array identity", index, action.Target.Describe())
		}
		if _, ok := action.Target.Array(); !ok {
			t.Errorf("action %d Target = %s, want array", index, action.Target.Describe())
		}
		if action.CallbackState != AggressorCallbackOmitted || action.Callback != nil {
			t.Errorf("action %d callback state = %v/%T, want omitted/nil", index, action.CallbackState, action.Callback)
		}
	}
	if upload := actions[0]; upload.Kind != AggressorBeaconActionUpload || upload.Name != "bupload" || len(upload.Arguments) != 1 || upload.Arguments[0].String() != nonexistent {
		t.Fatalf("bupload request = %#v", upload)
	}
	if upload := actions[1]; upload.Kind != AggressorBeaconActionUploadRaw || upload.Name != "bupload_raw" || len(upload.Arguments) != 3 ||
		upload.Arguments[0].String() != `C:\Temp\payload.bin` || !upload.Arguments[1].IdentityEqual(raw) || !upload.Arguments[2].IdentityEqual(localMetadata) {
		t.Fatalf("bupload_raw request = %#v", upload)
	}
}

func TestAggressorBeaconFilesystemActionsFallbackToHostExactlyOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []Value
	}{
		{name: "bcp", arguments: []Value{String("B-1"), String("source"), String("destination")}},
		{name: "bdrives", arguments: []Value{String("B-1")}},
		{name: "bmkdir", arguments: []Value{String("B-1"), String("folder")}},
		{name: "bmv", arguments: []Value{String("B-1"), String("source"), String("destination")}},
		{name: "btimestomp", arguments: []Value{String("B-1"), String("target"), String("source")}},
		{name: "bupload", arguments: []Value{String("B-1"), String("local")}},
		{name: "bupload_raw", arguments: []Value{String("B-1"), String("remote"), BinaryString([]byte("raw"))}},
		{name: "bupload_raw", arguments: []Value{String("B-1"), String("remote"), BinaryString([]byte("raw")), String("local")}},
	}
	var calls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		calls.Add(1)
		return String(fmt.Sprintf("host:%s:%d", invocation.Name, len(invocation.Arguments))), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	for _, test := range tests {
		result, invokeErr := runtimeInstance.Invoke(context.Background(), test.name, test.arguments...)
		want := fmt.Sprintf("host:%s:%d", test.name, len(test.arguments))
		if invokeErr != nil || result.String() != want {
			t.Errorf("%s/%d Host fallback = (%s, %v), want %q", test.name, len(test.arguments), result.Describe(), invokeErr, want)
		}
	}
	if calls.Load() != int32(len(tests)) {
		t.Fatalf("Host fallback calls = %d, want %d", calls.Load(), len(tests))
	}
}

func TestAggressorBeaconActionCallbackStatesMultiShotAndUnload(t *testing.T) {
	t.Parallel()

	provider := &recordingAggressorBeaconActionProvider{}
	runtimeInstance, err := New(WithAggressorBeaconActionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("beacon-action-callbacks.cna", `
$calls = 0;
$action_callback = {
    $calls++;
    return $1;
};
sub issue_omitted { return bps("B-omitted"); }
sub issue_null { return bps("B-null", $null); }
sub issue_callable { return bps("B-callable", $action_callback); }
`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"issue_omitted", "issue_null", "issue_callable"} {
		result, callErr := owner.Call(context.Background(), name)
		if callErr != nil || !result.IsNull() {
			t.Fatalf("%s = (%s, %v), want null/nil", name, result.Describe(), callErr)
		}
	}

	actions := provider.snapshot()
	if len(actions) != 3 {
		t.Fatalf("callback-state provider calls = %d, want three", len(actions))
	}
	wantStates := []AggressorCallbackState{
		AggressorCallbackOmitted,
		AggressorCallbackNull,
		AggressorCallbackCallable,
	}
	for index, action := range actions {
		if action.Kind != AggressorBeaconActionListProcesses || action.Name != "bps" ||
			action.RuntimeID != runtimeInstance.ID() || action.Script != owner.ID() || action.Span.Source != "beacon-action-callbacks.cna" {
			t.Errorf("callback-state action %d metadata = %#v", index, action)
		}
		if action.CallbackState != wantStates[index] {
			t.Errorf("callback-state action %d = %v, want %v", index, action.CallbackState, wantStates[index])
		}
		if (index < 2) != (action.Callback == nil) {
			t.Errorf("callback-state action %d capability = %T", index, action.Callback)
		}
		if len(action.Arguments) != 0 {
			t.Errorf("callback-state action %d ordinary arguments = %v, want empty", index, describeAggressorBeaconActionValues(action.Arguments))
		}
	}

	retained := actions[2].Callback
	first := ArrayValue(NewArray(String("first")))
	secondHash := NewOrderedHash()
	secondHash.Set("call", Int(2))
	second := HashValue(secondHash)
	for index, argument := range []Value{first, second} {
		result, invokeErr := retained.Invoke(context.Background(), argument)
		if invokeErr != nil || !result.IdentityEqual(argument) {
			t.Fatalf("retained callback call %d = (%s, %v), want identical %s/nil",
				index, result.Describe(), invokeErr, argument.Describe())
		}
	}
	if got := owner.Get("$calls").Int32(); got != 2 {
		t.Fatalf("multi-shot callback calls = %d, want 2", got)
	}
	if err := owner.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := retained.Invoke(context.Background(), String("after unload"))
	if !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("callback after owner unload = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
}

func TestAggressorBeaconActionProviderErrorIsAuthoritative(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Beacon task rejected")
	var providerCalls atomic.Int32
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return String("Host result"), nil
		})),
		WithAggressorBeaconActionProvider(AggressorBeaconActionProviderFunc(func(_ context.Context, action AggressorBeaconAction) error {
			providerCalls.Add(1)
			if action.Name != "bexecute" || action.Kind != AggressorBeaconActionExecute {
				return fmt.Errorf("unexpected action %#v", action)
			}
			return wantErr
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	result, err := runtimeInstance.Invoke(context.Background(), "bexecute", String("B-error"), String("whoami"))
	if !errors.Is(err, wantErr) || !result.IsNull() {
		t.Fatalf("provider error = (%s, %v), want null/%v", result.Describe(), err, wantErr)
	}
	if providerCalls.Load() != 1 || hostCalls.Load() != 0 {
		t.Fatalf("provider/Host calls = %d/%d, want one/zero", providerCalls.Load(), hostCalls.Load())
	}
}

func TestAggressorBeaconActionConfiguredProviderHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	var providerCalls atomic.Int32
	cancelDuringProvider := context.CancelFunc(func() {})
	runtimeInstance, err := New(WithAggressorBeaconActionProvider(AggressorBeaconActionProviderFunc(func(
		ctx context.Context,
		_ AggressorBeaconAction,
	) error {
		providerCalls.Add(1)
		cancelDuringProvider()
		if !errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("provider context error after cancellation = %v", ctx.Err())
		}
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	preCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := runtimeInstance.Invoke(preCanceled, "bexecute", String("B-pre-canceled"), String("whoami"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() || providerCalls.Load() != 0 {
		t.Fatalf("pre-canceled action = (%s, %v), provider calls %d; want null/context.Canceled/zero",
			result.Describe(), err, providerCalls.Load())
	}

	canceledDuring, cancelDuring := context.WithCancel(context.Background())
	cancelDuringProvider = cancelDuring
	result, err = runtimeInstance.Invoke(canceledDuring, "bexecute", String("B-cancel-during"), String("whoami"))
	if !errors.Is(err, context.Canceled) || !result.IsNull() {
		t.Fatalf("provider-canceled action = (%s, %v), want null/context.Canceled", result.Describe(), err)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls = %d, want only the cancel-during call", providerCalls.Load())
	}
}

func TestAggressorBeaconActionCallbackRejectsRuntimeClose(t *testing.T) {
	t.Parallel()

	var retained Callable
	runtimeInstance, err := New(WithAggressorBeaconActionProvider(AggressorBeaconActionProviderFunc(func(
		_ context.Context,
		action AggressorBeaconAction,
	) error {
		retained = action.Callback
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	program, err := CompileString("beacon-action-runtime-close.cna", `bps("B-close", { return $1; });`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Load(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if retained == nil {
		t.Fatal("provider did not receive callable callback")
	}
	if err := runtimeInstance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := retained.Invoke(context.Background(), String("after close"))
	if !errors.Is(err, ErrScriptUnloaded) || !result.IsNull() {
		t.Fatalf("callback after Runtime.Close = (%s, %v), want null/ErrScriptUnloaded", result.Describe(), err)
	}
}

func TestAggressorBeaconActionUnsetProviderPreservesExactHostInvocationResultAndError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Host-owned Beacon result")
	wantResult := HashValue(NewHash())
	targetCell := NewCell(String("B-before"))
	callbackCell := NewCell(String("not-callable-and-still-Host-owned"))
	span := Span{Source: "beacon-action-host.cna", Start: Position{Line: 23, Column: 5}}
	original := Invocation{
		Script: 73,
		Name:   "bps",
		Span:   span,
		Arguments: []Argument{
			{Name: "$bid", Reference: targetCell},
			{Name: "&callback", Reference: callbackCell},
		},
	}
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		if !invocation.Arguments[0].Set(String("B-mutated-by-Host")) {
			return Null(), errors.New("Host lost target reference")
		}
		if !invocation.Arguments[1].Set(String("callback-mutated-by-Host")) {
			return Null(), errors.New("Host lost callback reference")
		}
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	original.Runtime = runtimeInstance

	result, err := runtimeInstance.aggressorBeaconAction(context.Background(), original)
	if !errors.Is(err, wantErr) || !result.IdentityEqual(wantResult) {
		t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), err, wantErr)
	}
	if hostCalls.Load() != 1 || captured.Runtime != original.Runtime || captured.Script != original.Script ||
		captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != 2 {
		t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
	}
	if captured.Arguments[0].Name != "$bid" || captured.Arguments[0].Reference != targetCell ||
		captured.Arguments[1].Name != "&callback" || captured.Arguments[1].Reference != callbackCell {
		t.Fatalf("Host did not receive the original reference-bearing arguments: %#v", captured.Arguments)
	}
	if got := targetCell.Get().String(); got != "B-mutated-by-Host" {
		t.Fatalf("Host target mutation = %q", got)
	}
	if got := callbackCell.Get().String(); got != "callback-mutated-by-Host" {
		t.Fatalf("Host callback-shaped mutation = %q", got)
	}
}

func TestAggressorBeaconActionUnsetProviderPreservesEveryRawInvocation(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Host-owned result")
	var wantResult Value
	var captured Invocation
	var hostCalls atomic.Int32
	runtimeInstance, err := New(WithHost(HostFunc(func(_ context.Context, invocation Invocation) (Value, error) {
		hostCalls.Add(1)
		captured = invocation
		if !invocation.Arguments[0].Set(String("Host-mutated:" + invocation.Name)) {
			return Null(), errors.New("Host lost the target reference")
		}
		return wantResult, wantErr
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	for _, test := range aggressorBeaconActionTestSpecs {
		test := test
		t.Run(test.name, func(t *testing.T) {
			cells := make([]*Cell, test.maximum)
			arguments := make([]Argument, test.maximum)
			for index := range arguments {
				cells[index] = NewCell(String(fmt.Sprintf("%s-value-%d", test.name, index)))
				arguments[index] = Argument{
					Name:      fmt.Sprintf("$argument_%d", index),
					Reference: cells[index],
				}
			}
			span := Span{Source: test.name + "-host.cna", Start: Position{Line: 7, Column: 3}}
			original := Invocation{
				Runtime:   runtimeInstance,
				Script:    91,
				Name:      test.name,
				Span:      span,
				Arguments: arguments,
			}
			wantResult = HashValue(NewOrderedHash())
			before := hostCalls.Load()
			result, invokeErr := runtimeInstance.aggressorBeaconAction(context.Background(), original)
			if !errors.Is(invokeErr, wantErr) || !result.IdentityEqual(wantResult) {
				t.Fatalf("Host fallback = (%s, %v), want identical result/%v", result.Describe(), invokeErr, wantErr)
			}
			if hostCalls.Load() != before+1 || captured.Runtime != original.Runtime || captured.Script != original.Script ||
				captured.Name != original.Name || captured.Span != original.Span || len(captured.Arguments) != len(original.Arguments) {
				t.Fatalf("captured Host invocation/calls = %#v/%d", captured, hostCalls.Load())
			}
			for index := range original.Arguments {
				if captured.Arguments[index].Name != original.Arguments[index].Name ||
					captured.Arguments[index].Reference != cells[index] {
					t.Errorf("argument %d was not the original reference: %#v", index, captured.Arguments[index])
				}
			}
			if got := cells[0].Get().String(); got != "Host-mutated:"+test.name {
				t.Errorf("target mutation = %q", got)
			}
		})
	}
}

func TestAggressorBeaconActionWithFunctionOverridesEveryNameInBothOptionOrders(t *testing.T) {
	for _, test := range aggressorBeaconActionTestSpecs {
		test := test
		for _, overrideFirst := range []bool{false, true} {
			overrideFirst := overrideFirst
			t.Run(fmt.Sprintf("%s/override-first=%v", test.name, overrideFirst), func(t *testing.T) {
				var providerCalls atomic.Int32
				var hostCalls atomic.Int32
				providerOption := WithAggressorBeaconActionProvider(AggressorBeaconActionProviderFunc(func(context.Context, AggressorBeaconAction) error {
					providerCalls.Add(1)
					return nil
				}))
				overrideOption := WithFunction(test.name, func(_ context.Context, invocation Invocation) (Value, error) {
					return String("override:" + invocation.Name), nil
				})
				hostOption := WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
					hostCalls.Add(1)
					return Null(), nil
				}))
				options := []Option{hostOption, providerOption, overrideOption}
				if overrideFirst {
					options = []Option{hostOption, overrideOption, providerOption}
				}
				runtimeInstance, err := New(options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

				// Zero arguments is invalid for every stock action. Success therefore
				// proves the importer override won before native arity validation.
				result, err := runtimeInstance.Invoke(context.Background(), test.name)
				if err != nil || result.String() != "override:"+test.name {
					t.Fatalf("override = (%s, %v)", result.Describe(), err)
				}
				if providerCalls.Load() != 0 || hostCalls.Load() != 0 {
					t.Fatalf("override provider/Host calls = %d/%d, want zero/zero", providerCalls.Load(), hostCalls.Load())
				}
			})
		}
	}
}

type typedNilAggressorBeaconActionProvider struct{}

func (*typedNilAggressorBeaconActionProvider) DispatchAggressorBeaconAction(context.Context, AggressorBeaconAction) error {
	panic("typed-nil Aggressor Beacon action provider was invoked")
}

func TestAggressorBeaconActionRejectsTypedNilProvidersAndNilFunctionAdapters(t *testing.T) {
	t.Parallel()

	const want = "opfor: Aggressor Beacon action provider is nil"
	var pointer *typedNilAggressorBeaconActionProvider
	var function AggressorBeaconActionProviderFunc
	for _, test := range []struct {
		name     string
		provider AggressorBeaconActionProvider
	}{
		{name: "typed-nil pointer", provider: pointer},
		{name: "nil function adapter", provider: function},
	} {
		_, err := New(WithAggressorBeaconActionProvider(test.provider))
		if err == nil || err.Error() != want {
			t.Errorf("%s error = %v, want %q", test.name, err, want)
		}
	}
	if err := function.DispatchAggressorBeaconAction(context.Background(), AggressorBeaconAction{}); err == nil || err.Error() != want {
		t.Fatalf("direct nil function adapter error = %v, want %q", err, want)
	}
}

func TestAggressorBeaconActionProviderSupportsConcurrentCalls(t *testing.T) {
	t.Parallel()

	const concurrentCalls = 24
	entered := make(chan struct{}, concurrentCalls)
	release := make(chan struct{})
	provider := &recordingAggressorBeaconActionProvider{
		dispatch: func(_ context.Context, _ AggressorBeaconAction) error {
			entered <- struct{}{}
			<-release
			return nil
		},
	}
	runtimeInstance, err := New(WithAggressorBeaconActionProvider(provider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	targets := make([]*Array, concurrentCalls)
	arguments := make([]*Hash, concurrentCalls)
	results := make(chan error, concurrentCalls)
	var group sync.WaitGroup
	for index := 0; index < concurrentCalls; index++ {
		index := index
		targets[index] = NewArray(Int(int32(index)))
		arguments[index] = NewOrderedHash()
		arguments[index].Set("call", Int(int32(index)))
		group.Add(1)
		go func() {
			defer group.Done()
			result, invokeErr := runtimeInstance.Invoke(
				context.Background(), "bexecute", ArrayValue(targets[index]), HashValue(arguments[index]),
			)
			if invokeErr == nil && !result.IsNull() {
				invokeErr = fmt.Errorf("result = %s, want null", result.Describe())
			}
			results <- invokeErr
		}()
	}
	for index := 0; index < concurrentCalls; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatalf("only %d/%d provider calls entered concurrently", index, concurrentCalls)
		}
	}
	close(release)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}

	actions := provider.snapshot()
	if len(actions) != concurrentCalls {
		t.Fatalf("concurrent provider calls = %d, want %d", len(actions), concurrentCalls)
	}
	wantTargets := make(map[*Array]struct{}, concurrentCalls)
	wantArguments := make(map[*Hash]struct{}, concurrentCalls)
	for index := range targets {
		wantTargets[targets[index]] = struct{}{}
		wantArguments[arguments[index]] = struct{}{}
	}
	for index, action := range actions {
		if action.Name != "bexecute" || action.Kind != AggressorBeaconActionExecute ||
			action.RuntimeID != runtimeInstance.ID() || action.Script != 0 || action.Span != (Span{}) ||
			action.CallbackState != AggressorCallbackOmitted || action.Callback != nil {
			t.Errorf("concurrent action %d metadata = %#v", index, action)
		}
		target, ok := action.Target.Array()
		if !ok {
			t.Errorf("concurrent action %d Target = %s, want array", index, action.Target.Describe())
		} else if _, exists := wantTargets[target]; !exists {
			t.Errorf("concurrent action %d Target identity was not supplied by a caller", index)
		} else {
			delete(wantTargets, target)
		}
		if len(action.Arguments) != 1 {
			t.Errorf("concurrent action %d Arguments = %v, want one hash", index, describeAggressorBeaconActionValues(action.Arguments))
			continue
		}
		argument, ok := action.Arguments[0].Hash()
		if !ok {
			t.Errorf("concurrent action %d argument = %s, want hash", index, action.Arguments[0].Describe())
		} else if _, exists := wantArguments[argument]; !exists {
			t.Errorf("concurrent action %d argument identity was not supplied by a caller", index)
		} else {
			delete(wantArguments, argument)
		}
	}
	if len(wantTargets) != 0 || len(wantArguments) != 0 {
		t.Fatalf("concurrent provider lost %d Target and %d Argument identities", len(wantTargets), len(wantArguments))
	}
}

func TestPortableScriptLoaderInheritsAggressorBeaconActionProvider(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child-beacon-action.cna")
	if err := os.WriteFile(childPath, []byte(`bexecute("child-target", "child-command");`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("parent-beacon-action.cna", fmt.Sprintf(`
import sleep.runtime.ScriptLoader;
$loader = [new ScriptLoader];
$child = [$loader loadScript: %q];
bexecute("parent-target", "parent-command");
[$child runScript];
`, filepath.ToSlash(childPath)))
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingAggressorBeaconActionProvider{}
	var hostCalls atomic.Int32
	runtimeInstance, err := New(
		WithHost(HostFunc(func(context.Context, Invocation) (Value, error) {
			hostCalls.Add(1)
			return Null(), errors.New("ScriptLoader Beacon action route reached Host")
		})),
		WithAggressorBeaconActionProvider(provider),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Execute(context.Background(), program); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 0 {
		t.Fatalf("inherited provider actions reached Host %d time(s)", hostCalls.Load())
	}

	actions := provider.snapshot()
	if len(actions) != 2 {
		t.Fatalf("parent/child provider actions = %d, want two", len(actions))
	}
	if actions[0].Target.String() != "parent-target" || len(actions[0].Arguments) != 1 || actions[0].Arguments[0].String() != "parent-command" ||
		actions[1].Target.String() != "child-target" || len(actions[1].Arguments) != 1 || actions[1].Arguments[0].String() != "child-command" {
		t.Fatalf("parent/child provider action values = %#v", actions)
	}
	if actions[0].RuntimeID != runtimeInstance.ID() || actions[0].RuntimeID == 0 ||
		actions[1].RuntimeID == 0 || actions[1].RuntimeID == actions[0].RuntimeID {
		t.Fatalf("parent/child RuntimeIDs = %d/%d, parent want %d", actions[0].RuntimeID, actions[1].RuntimeID, runtimeInstance.ID())
	}
	if actions[0].Script != 1 || actions[1].Script != 1 ||
		actions[0].Span.Source != "parent-beacon-action.cna" || actions[1].Span.Source != filepath.ToSlash(childPath) {
		t.Fatalf("parent/child provenance = %#v", actions)
	}
	for index, action := range actions {
		if action.Name != "bexecute" || action.Kind != AggressorBeaconActionExecute ||
			action.CallbackState != AggressorCallbackOmitted || action.Callback != nil {
			t.Errorf("parent/child action %d route/state = %#v", index, action)
		}
	}
}

func aggressorBeaconActionTestOwner(t *testing.T, runtimeInstance *Runtime) *Script {
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

func aggressorBeaconActionTestInvocation(
	runtimeInstance *Runtime,
	scriptID ScriptID,
	name string,
	span Span,
	values ...Value,
) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{
		Runtime:   runtimeInstance,
		Script:    scriptID,
		Name:      name,
		Span:      span,
		Arguments: arguments,
	}
}

func aggressorBeaconActionTestValues(prefix string, count int) []Value {
	values := make([]Value, count)
	for index := range values {
		values[index] = String(fmt.Sprintf("%s-argument-%d", prefix, index))
	}
	return values
}

func describeAggressorBeaconActionValues(values []Value) []string {
	described := make([]string, len(values))
	for index, value := range values {
		described[index] = value.Describe()
	}
	return described
}
