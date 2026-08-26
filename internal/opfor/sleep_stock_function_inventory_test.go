package opfor

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// sleepOfficialStaticBridgeFunctions is the complete set of non-empty `&name`
// keys registered with literal Hashtable.put calls by the seven global bridge
// classes loaded in ScriptLoader at Cobalt-Strike/sleep commit
// 60ac3ff9dacc3e7b5a6c58be201c5830afbda398. The BasicNumbers loop-installed
// functions are listed separately below. Catalog presence is only a namespace
// completeness assertion across native defaults and evaluator-owned
// intrinsics; focused source/JAR tests prove behavior.
const sleepOfficialStaticBridgeFunctions = `
% @ acquire add addAll allocate array asc available bread bwrite byteAt cast
casti charAt chdir checkError checksum chr clear closef compile_closure concat
connect consume copy createNewFile cwd debug deleteFile digest double eval exec
exit expr filter find flatten fork formatDate formatNumber function getConsole
getCurrentDirectory getFileName getFileParent getFileProper getStackTrace global
hash include indexOf inline int invoke join keys lambda lastModified lc left let
lindexOf listRoots listen local lof long ls map mark matched matches mid mkdir
newInstance not ohash ohasha openf pack parseDate parseNumber pop popl print
printAll printEOF printf println profile push pushl putAll rand read readAll
readAsObject readObject readb readc readln reduce release remove removeAll
removeAt rename replace replaceAt reset retainAll reverse right scalar search
semaphore setEncoding setField setLastModified setMissPolicy setReadOnly
setRemovalPolicy setf shift size sizeof skip sleep sort sorta sortd sortn splice
split srand strlen strrep subarray sublist substr systemProperties taint this
ticks tr typeOf uc uint unpack untaint use values wait warn watch writeAsObject
writeObject writeb
`

const sleepOfficialLoopBridgeFunctions = `
abs acos asin atan atan2 ceil cos log round sin sqrt tan radians degrees exp
floor sum
`

func TestOfficialSleepStockFunctionNamespaceIsComplete(t *testing.T) {
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })

	names := strings.Fields(sleepOfficialStaticBridgeFunctions + sleepOfficialLoopBridgeFunctions)
	if len(names) != 177 {
		t.Fatalf("official Sleep function manifest contains %d names, want 177", len(names))
	}
	seen := make(map[string]struct{}, len(names))
	var duplicates []string
	var missing []string
	for _, name := range names {
		if _, exists := seen[name]; exists {
			duplicates = append(duplicates, name)
			continue
		}
		seen[name] = struct{}{}
		if !runtimeInstance.hasStockFunction(name) && !overridableSpecialCall(name) {
			missing = append(missing, name)
		}
	}
	sort.Strings(duplicates)
	sort.Strings(missing)
	if len(duplicates) != 0 || len(missing) != 0 {
		t.Fatalf("official Sleep stock namespace duplicates=%v missing=%v", duplicates, missing)
	}
}
