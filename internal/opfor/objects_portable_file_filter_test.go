package opfor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func preparePortableJavaFileFilterFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, entry := range []struct {
		name      string
		directory bool
	}{
		{name: "c.keep"},
		{name: "sub", directory: true},
		{name: "a.keep"},
		{name: "b.drop"},
	} {
		path := filepath.Join(root, entry.name)
		if entry.directory {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(entry.name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func portableJavaFileFilterNames(t *testing.T, value Value) []string {
	t.Helper()
	array, ok := value.Array()
	if !ok || array == nil {
		t.Fatalf("filter result = %s, want array", value.Describe())
	}
	names := make([]string, 0, array.Len())
	for _, item := range array.Values() {
		if file, ok := portableJavaFileValue(item); ok {
			names = append(names, portableJavaFileNameValue(file.pathValue()).String())
		} else {
			names = append(names, item.String())
		}
	}
	return names
}

func portableJavaFileKeepNames(names []string) []string {
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, ".keep") {
			kept = append(kept, name)
		}
	}
	return kept
}

func TestPortableJavaFileFilterCallbackArgumentsOrderAndIdentity(t *testing.T) {
	root := preparePortableJavaFileFilterFixture(t)
	file := newPortableJavaFile(String(filepath.ToSlash(root)))

	unfiltered, handled, err := file.portableListContext(context.Background(), false, portableJavaFileFilterNone, nil)
	if err != nil || !handled {
		t.Fatalf("unfiltered list = (handled:%t, %v)", handled, err)
	}
	order := portableJavaFileFilterNames(t, unfiltered)

	var filenameSeen []string
	filenameParentsSame := true
	filenameCallable := CallableFunc(func(_ context.Context, values ...Value) (Value, error) {
		if len(values) != 2 {
			return Null(), fmt.Errorf("FilenameFilter arguments = %d, want 2", len(values))
		}
		parent, ok := values[0].Object()
		filenameParentsSame = filenameParentsSame && ok && parent == file
		filenameSeen = append(filenameSeen, values[1].String())
		return Bool(strings.HasSuffix(values[1].String(), ".keep")), nil
	})
	filenameInvocation := ObjectInvocation{
		Op:        ObjectInvoke,
		Target:    ObjectValue(file),
		Message:   "list",
		Arguments: []Argument{{Value: FunctionValue(filenameCallable)}},
	}
	filenameResult, handled, err := file.invokeContext(context.Background(), filenameInvocation)
	if err != nil || !handled {
		t.Fatalf("FilenameFilter list = (handled:%t, %v)", handled, err)
	}
	if !filenameParentsSame {
		t.Fatal("FilenameFilter did not receive the exact parent File object")
	}
	if !reflect.DeepEqual(filenameSeen, order) {
		t.Fatalf("FilenameFilter callback order = %q, want normalizedList order %q", filenameSeen, order)
	}
	if got, want := portableJavaFileFilterNames(t, filenameResult), portableJavaFileKeepNames(order); !reflect.DeepEqual(got, want) {
		t.Fatalf("FilenameFilter result order = %q, want %q", got, want)
	}

	var fileSeen []string
	var acceptedObjects []any
	fileCallable := CallableFunc(func(_ context.Context, values ...Value) (Value, error) {
		if len(values) != 1 {
			return Null(), fmt.Errorf("FileFilter arguments = %d, want 1", len(values))
		}
		child, ok := portableJavaFileValue(values[0])
		if !ok {
			return Null(), fmt.Errorf("FileFilter argument = %s, want File", values[0].Describe())
		}
		name := portableJavaFileNameValue(child.pathValue()).String()
		fileSeen = append(fileSeen, name)
		if strings.HasSuffix(name, ".keep") {
			object, _ := values[0].Object()
			acceptedObjects = append(acceptedObjects, object)
			return Int(1), nil
		}
		return Int(0), nil
	})
	fileInvocation := ObjectInvocation{
		Op:        ObjectInvoke,
		Target:    ObjectValue(file),
		Message:   "listFiles",
		Arguments: []Argument{{Value: FunctionValue(fileCallable)}},
	}
	fileResult, handled, err := file.invokeContext(context.Background(), fileInvocation)
	if err != nil || !handled {
		t.Fatalf("FileFilter listFiles = (handled:%t, %v)", handled, err)
	}
	if !reflect.DeepEqual(fileSeen, order) {
		t.Fatalf("FileFilter callback order = %q, want normalizedList order %q", fileSeen, order)
	}
	resultArray, ok := fileResult.Array()
	if !ok || resultArray.Len() != len(acceptedObjects) {
		t.Fatalf("FileFilter result = %s, want %d accepted Files", fileResult.Describe(), len(acceptedObjects))
	}
	for index, value := range resultArray.Values() {
		object, ok := value.Object()
		if !ok || object != acceptedObjects[index] {
			t.Errorf("FileFilter result[%d] did not retain the exact callback File object", index)
		}
	}
}

func TestPortableJavaFileFilterSleepProxyABIAndSoftError(t *testing.T) {
	root := preparePortableJavaFileFilterFixture(t)
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "file-filter-contract.sl", portableJavaFileFilterProbeSource(root)); err != nil {
		t.Fatalf("Eval: %v\n%s", err, output.String())
	}
	text := filepath.ToSlash(output.String())
	for _, want := range []string{
		"direct-list-calls=accept:2:same:",
		"direct-files-calls=accept:1:",
		"filename-list-calls=accept:2:same:",
		"filename-files-calls=accept:2:same:",
		"file-files-calls=accept:1:",
		"both-files-calls=accept:1:",
		"missing=NULL/0",
		"throw-result=NULL",
		"throw-class=java.lang.RuntimeException",
		"throw-message=boom",
		"filename-class=interface java.io.FilenameFilter",
		"file-class=interface java.io.FileFilter",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("probe output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Warning:") {
		t.Fatalf("filter probe produced an unexpected warning:\n%s", text)
	}
	stackResult, err := runtimeInstance.Eval(context.Background(), "file-filter-stack.sl", fmt.Sprintf(`
import java.io.*;
$dir = [new File: %s];
$filter = newInstance(^FilenameFilter, { throw "stack-boom"; });
[$dir list: $filter];
checkError();
return getStackTrace();
`, strconv.Quote(filepath.ToSlash(root))))
	if err != nil {
		t.Fatal(err)
	}
	stack, ok := stackResult.Array()
	if !ok || stack.Len() != 2 {
		t.Fatalf("filter soft-error stack = %s, want proxy and origin frames", stackResult.Describe())
	}
	frames := portableJavaFileFilterNames(t, stackResult)
	if !strings.Contains(frames[0], "<Java>:-1") || !strings.Contains(frames[0], "java.io.FilenameFilter.accept(java.io.File,java.lang.String)") || !strings.Contains(frames[1], "<origin of exception>") {
		t.Fatalf("filter soft-error frames = %q", frames)
	}
}

func TestPortableJavaFileFilterCancellationInstructionLimitAndConcurrentUse(t *testing.T) {
	root := preparePortableJavaFileFilterFixture(t)
	file := newPortableJavaFile(String(filepath.ToSlash(root)))

	cancelled, cancel := context.WithCancel(context.Background())
	calls := 0
	cancelling := FunctionValue(CallableFunc(func(context.Context, ...Value) (Value, error) {
		calls++
		cancel()
		return Int(1), nil
	}))
	_, handled, err := file.invokeContext(cancelled, ObjectInvocation{
		Op: ObjectInvoke, Message: "listFiles", Arguments: []Argument{{Value: cancelling}},
	})
	if !handled || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled FileFilter = (handled:%t, calls:%d, %v), want true/1/context.Canceled", handled, calls, err)
	}

	limitedRuntime, err := New(WithInstructionLimit(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limitedRuntime.Close(context.Background()) })
	limitedCalls := 0
	limited := FunctionValue(CallableFunc(func(context.Context, ...Value) (Value, error) {
		limitedCalls++
		return Int(1), nil
	}))
	_, handled, err = file.invokeContext(withExecutionMeter(context.Background(), limitedRuntime), ObjectInvocation{
		Runtime: limitedRuntime, Op: ObjectInvoke, Message: "listFiles", Arguments: []Argument{{Value: limited}},
	})
	if !handled || !errors.Is(err, ErrInstructionLimit) || limitedCalls > 1 {
		t.Fatalf("limited FileFilter = (handled:%t, calls:%d, %v), want ErrInstructionLimit before second callback", handled, limitedCalls, err)
	}

	const workers = 12
	var wait sync.WaitGroup
	var callbackCalls atomic.Int64
	errorsByWorker := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			filter := FunctionValue(CallableFunc(func(context.Context, ...Value) (Value, error) {
				callbackCalls.Add(1)
				return Int(1), nil
			}))
			value, handled, err := file.invokeContext(context.Background(), ObjectInvocation{
				Op: ObjectInvoke, Message: "listFiles", Arguments: []Argument{{Value: filter}},
			})
			if err != nil || !handled {
				errorsByWorker <- fmt.Errorf("handled:%t error:%w", handled, err)
				return
			}
			array, ok := value.Array()
			if !ok || array.Len() != 4 {
				errorsByWorker <- fmt.Errorf("result = %s, want four entries", value.Describe())
			}
		}()
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Error(err)
	}
	if got, want := callbackCalls.Load(), int64(workers*4); got != want {
		t.Errorf("concurrent callback calls = %d, want %d", got, want)
	}
}

func TestPortableJavaFileFilterObjectHostPrecedenceAndOpaqueBoundary(t *testing.T) {
	root := preparePortableJavaFileFilterFixture(t)
	callbackCalls := 0
	listCalls := 0
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Op == ObjectInvoke && invocation.Message == "list" && len(invocation.Arguments) == 1 {
			listCalls++
			return ArrayValue(NewArray(String("importer"))), nil
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if err := runtimeInstance.RegisterFunction("mark", func(context.Context, Invocation) (Value, error) {
		callbackCalls++
		return Int(1), nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := runtimeInstance.Eval(context.Background(), "file-filter-host.sl", fmt.Sprintf(`
import java.io.*;
$dir = [new File: %s];
$filter = { return mark(); };
return [$dir list: $filter];
`, strconv.Quote(filepath.ToSlash(root))))
	if err != nil {
		t.Fatal(err)
	}
	if got := portableJavaFileFilterNames(t, result); !reflect.DeepEqual(got, []string{"importer"}) {
		t.Fatalf("ObjectHost result = %q, want importer", got)
	}
	if listCalls != 1 || callbackCalls != 0 {
		t.Fatalf("ObjectHost precedence = (list calls:%d, callback calls:%d), want 1/0", listCalls, callbackCalls)
	}

	opaque := ObjectValue(&struct{ name string }{name: "JVM filter"})
	file := newPortableJavaFile(String(filepath.ToSlash(root)))
	_, handled, err := file.invokeContext(context.Background(), ObjectInvocation{
		Op: ObjectInvoke, Message: "list", Arguments: []Argument{{Value: opaque}},
	})
	if handled || err != nil {
		t.Fatalf("opaque JVM filter fallback = (handled:%t, %v), want importer-owned unhandled", handled, err)
	}

	boundaryErr := errors.New("filter callback importer failure")
	boundaryTarget := &struct{}{}
	boundaryObject := ObjectValue(boundaryTarget)
	boundaryCalls := 0
	boundaryRuntime, err := New(
		WithInitialGlobals(map[string]Value{"boundary_filter_object": boundaryObject}),
		WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			target, _ := invocation.Target.Object()
			if invocation.Op == ObjectInvoke && target == boundaryTarget && invocation.Message == "accept" {
				boundaryCalls++
				return Null(), boundaryErr
			}
			return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boundaryRuntime.Close(context.Background()) })
	_, err = boundaryRuntime.Eval(context.Background(), "file-filter-boundary-error.sl", fmt.Sprintf(`
import java.io.*;
$dir = [new File: %s];
[$dir list: { return [$boundary_filter_object accept]; }];
`, strconv.Quote(filepath.ToSlash(root))))
	if !errors.Is(err, boundaryErr) || boundaryCalls != 1 {
		t.Fatalf("nested ObjectHost callback error = (%v, calls:%d), want authoritative error/1", err, boundaryCalls)
	}
}

func TestPortableJavaFileFilterOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	root := preparePortableJavaFileFilterFixture(t)
	source := portableJavaFileFilterProbeSource(root)
	var goOutput bytes.Buffer
	runtimeInstance, err := New(WithStdout(&goOutput), WithStderr(&goOutput))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "file-filter-differential.sl", source); err != nil {
		t.Fatalf("pure-Go File filter probe: %v\n%s", err, goOutput.String())
	}
	command := officialSleepJavaCommand(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", source)
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep File filter probe: %v\n%s", err, javaOutput)
	}
	got := strings.ReplaceAll(goOutput.String(), "\r\n", "\n")
	want := strings.ReplaceAll(string(javaOutput), "\r\n", "\n")
	if got != want {
		t.Fatalf("official Sleep File filter mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func portableJavaFileFilterProbeSource(root string) string {
	return fmt.Sprintf(`
import java.io.*;

sub filter_names {
    local('$value @out $item');
    $value = $1;
    if ($value is $null) { return "NULL"; }
    foreach $item ($value) {
        if ($item isa ^File) { push(@out, [$item getName]); }
        else { push(@out, $item); }
    }
    return join(",", @out);
}

$dir = [new File: %s];
println("unfiltered=" . filter_names([$dir list]));

$direct_list_calls = "";
$direct_list = {
    $direct_list_calls .= $0 . ":" . size(@_) . ":";
    if ($1 is $dir) { $direct_list_calls .= "same:" . $2 . ";"; }
    else { $direct_list_calls .= "other:" . $2 . ";"; }
    return [$2 endsWith: ".keep"];
};
println("direct-list=" . filter_names([$dir list: $direct_list]));
println("direct-list-calls=" . $direct_list_calls);

$direct_files_calls = "";
$direct_files = {
    $direct_files_calls .= $0 . ":" . size(@_) . ":" . [$1 getName] . ";";
    return [[$1 getName] endsWith: ".keep"];
};
println("direct-files=" . filter_names([$dir listFiles: $direct_files]));
println("direct-files-calls=" . $direct_files_calls);

$filename_list_calls = "";
$filename = newInstance(^FilenameFilter, {
    $filename_list_calls .= $0 . ":" . size(@_) . ":";
    if ($1 is $dir) { $filename_list_calls .= "same:" . $2 . ";"; }
    else { $filename_list_calls .= "other:" . $2 . ";"; }
    return [$2 endsWith: ".keep"];
});
println("filename-list=" . filter_names([$dir list: $filename]));
println("filename-list-calls=" . $filename_list_calls);
$filename_list_calls = "";
println("filename-files=" . filter_names([$dir listFiles: $filename]));
println("filename-files-calls=" . $filename_list_calls);

$file_calls = "";
$file_filter = newInstance(^FileFilter, {
    $file_calls .= $0 . ":" . size(@_) . ":" . [$1 getName] . ";";
    return [[$1 getName] endsWith: ".keep"];
});
println("file-files=" . filter_names([$dir listFiles: $file_filter]));
println("file-files-calls=" . $file_calls);

$both_calls = "";
$both_filter = newInstance(@(^FilenameFilter, ^FileFilter), {
    $both_calls .= $0 . ":" . size(@_) . ":" . [$1 getName] . ";";
    return [[$1 getName] endsWith: ".keep"];
});
println("both-files=" . filter_names([$dir listFiles: $both_filter]));
println("both-files-calls=" . $both_calls);

$missing_calls = 0;
$missing = [new File: %s];
$missing_filter = newInstance(^FilenameFilter, { $missing_calls++; return 1; });
$missing_result = [$missing list: $missing_filter];
if ($missing_result is $null) { $missing_label = "NULL"; }
else { $missing_label = "NOTNULL"; }
println("missing=" . $missing_label . "/" . $missing_calls);

$coerce = newInstance(^FilenameFilter, {
    if ($2 eq "a.keep") { return "yes"; }
    if ($2 eq "b.drop") { return "1"; }
    if ($2 eq "c.keep") { return -1; }
    return 0.5;
});
println("coerce=" . filter_names([$dir list: $coerce]));

$throwing = newInstance(^FilenameFilter, {
    if ($2 eq "a.keep") { throw "boom"; }
    return 1;
});
$throw_result = [$dir list: $throwing];
$throw_error = checkError();
if ($throw_result is $null) { $throw_label = "NULL"; }
else { $throw_label = "NOTNULL"; }
println("throw-result=" . $throw_label);
println("throw-class=" . [[$throw_error getClass] getName]);
println("throw-message=" . [$throw_error getMessage]);
println("filename-class=" . ^FilenameFilter);
println("file-class=" . ^FileFilter);
`, strconv.Quote(filepath.ToSlash(root)), strconv.Quote(filepath.ToSlash(filepath.Join(root, "missing"))))
}
