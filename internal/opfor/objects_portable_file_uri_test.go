package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPortableJavaFileURIURLPathSourceContract(t *testing.T) {
	root := filepath.ToSlash(t.TempDir())
	pathname := root + "/opfor uri#frag?.txt"
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	source := fmt.Sprintf(`
import java.io.File;
import java.net.URI;
import java.net.URL;
import java.nio.file.Path;
import java.nio.file.Watchable;
$file = [new File: %s];
$uri = [$file toURI];
$url = [$file toURL];
$path = [$file toPath];
$path_again = [$file toPath];
$back = [$path toFile];
return @(
    [[$uri getClass] getName], "$uri", [$uri toString], [$uri toASCIIString],
    [$uri getScheme], [$uri getRawSchemeSpecificPart], [$uri getSchemeSpecificPart],
    [$uri getRawPath], [$uri getPath], [$uri getAuthority], [$uri getHost], [$uri getPort],
    [$uri isAbsolute], [$uri isOpaque], [$uri equals: [$file toURI]], [$uri compareTo: [$file toURI]],
    $uri isa ^URI, $uri isa ^java.lang.Comparable, $uri isa ^java.io.Serializable,
    [[$url getClass] getName], "$url", [$url toString], [$url toExternalForm],
    [$url getProtocol], [$url getHost], [$url getAuthority], [$url getUserInfo],
    [$url getPort], [$url getDefaultPort], [$url getPath], [$url getFile], [$url getQuery], [$url getRef],
    [$url equals: [$file toURL]], [$url sameFile: [$file toURL]], $url isa ^URL, $url isa ^java.io.Serializable,
    [[$path getClass] getName], "$path", $path is $path_again,
    $path isa ^Path, $path isa ^Watchable, $path isa ^java.lang.Comparable, $path isa ^java.lang.Iterable,
    [$back getPath], [$back equals: $file], $back is $file
);
`, strconv.Quote(pathname))
	result, err := runtimeInstance.Eval(context.Background(), "file-uri-url-path.sl", source)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := result.Array()
	if !ok {
		t.Fatalf("result = %s, want array", result.Describe())
	}
	abstractPath := newPortableJavaFile(String(pathname)).pathValue().String()
	slashPath := abstractPath
	if goruntime.GOOS == "windows" {
		slashPath = strings.ReplaceAll(slashPath, `\`, "/")
	}
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	uriPath := slashPath
	if strings.HasPrefix(uriPath, "//") {
		uriPath = "//" + uriPath
	}
	rawPath := strings.NewReplacer("%", "%25", " ", "%20", "#", "%23", "?", "%3F").Replace(uriPath)
	urlPath := slashPath
	urlFile := urlPath
	urlRef := ""
	if index := strings.Index(urlFile, "#"); index >= 0 {
		urlRef = urlFile[index+1:]
		urlFile = urlFile[:index]
	}
	pathClass := "sun.nio.fs.UnixPath"
	if goruntime.GOOS == "windows" {
		pathClass = "sun.nio.fs.WindowsPath"
	}
	want := []string{
		"java.net.URI", "file:" + rawPath, "file:" + rawPath, "file:" + rawPath,
		"file", rawPath, uriPath, rawPath, uriPath, "", "", "-1", "1", "0", "1", "0",
		"1", "1", "1",
		"java.net.URL", "file:" + urlPath, "file:" + urlPath, "file:" + urlPath,
		"file", "", "", "", "-1", "-1", urlFile, urlFile, "", urlRef,
		"1", "1", "1", "1",
		pathClass, abstractPath, "1", "1", "1", "1", "1", abstractPath, "1", "",
	}
	if got := argvValueStrings(array.Values()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("File URI/URL/Path values = %#v\nwant %#v", got, want)
	}
}

func TestPortableJavaFileURIDirectoryAndInvalidURL(t *testing.T) {
	directory := newPortableJavaFile(String(t.TempDir()))
	uri, err := portableJavaURIFromFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	url, err := portableJavaURLFromFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(uri.String(), "/") || !strings.HasSuffix(url.String(), "/") {
		t.Fatalf("directory conversions = (%q, %q), want trailing slash", uri.String(), url.String())
	}
	invalid := newPortableJavaFile(sleepStringValueFromUnits([]uint16{'a', 0, 'b'}, nil))
	if _, err := portableJavaURLFromFile(invalid); err == nil || err.Error() != "java.net.MalformedURLException: Invalid file path" {
		t.Fatalf("invalid File.toURL error = %v", err)
	}
	invalidURI, err := portableJavaURIFromFile(invalid)
	if err != nil || !strings.Contains(invalidURI.String(), "%00") {
		t.Fatalf("invalid File.toURI = (%v, %q), want quoted NUL", err, invalidURI)
	}
}

func TestPortableJavaFileToPathProviderNormalization(t *testing.T) {
	for _, test := range []struct {
		name, goos, input, want string
	}{
		{name: "unix duplicate and trailing separators", goos: "linux", input: "//alpha///beta/", want: "/alpha/beta"},
		{name: "unix root", goos: "darwin", input: "////", want: "/"},
		{name: "unix relative trailing", goos: "linux", input: "alpha/", want: "alpha"},
		{name: "windows drive absolute", goos: "windows", input: `C:/alpha//beta/`, want: `C:\alpha\beta`},
		{name: "windows drive relative", goos: "windows", input: `C:alpha//beta`, want: `C:alpha\beta`},
		{name: "windows directory relative", goos: "windows", input: `/alpha//beta/`, want: `\alpha\beta`},
		{name: "windows UNC", goos: "windows", input: `\\server\\share\\alpha\\`, want: `\\server\share\alpha`},
		{name: "windows long absolute", goos: "windows", input: `\\?\C:\alpha\\beta`, want: `C:\alpha\beta`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := portableJavaNIOPathValueForGOOS(String(test.input), test.goos)
			if err != nil || got.String() != test.want {
				t.Fatalf("normalize(%q, %s) = (%q, %v), want %q", test.input, test.goos, got.String(), err, test.want)
			}
		})
	}
	for _, test := range []struct {
		goos, input, contains string
	}{
		{goos: "linux", input: "a\x00b", contains: "Nul character not allowed"},
		{goos: "windows", input: `C:\bad?name`, contains: "Illegal char <?>"},
		{goos: "windows", input: `C:\bad \\name`, contains: "Trailing char < >"},
		{goos: "windows", input: `\\server`, contains: "UNC path is missing sharename"},
	} {
		if _, err := portableJavaNIOPathValueForGOOS(String(test.input), test.goos); err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Errorf("normalize(%q, %s) error = %v, want %q", test.input, test.goos, err, test.contains)
		}
	}
}

func TestPortableJavaFileToPathCachesIdentityConcurrently(t *testing.T) {
	file := newPortableJavaFile(String("alpha/beta"))
	const goroutines = 32
	paths := make(chan *portableJavaPath, goroutines)
	var wait sync.WaitGroup
	for range goroutines {
		wait.Add(1)
		go func() {
			defer wait.Done()
			path, err := file.portableJavaPath()
			if err != nil {
				t.Errorf("toPath: %v", err)
				return
			}
			paths <- path
		}()
	}
	wait.Wait()
	close(paths)
	var first *portableJavaPath
	for path := range paths {
		if first == nil {
			first = path
		} else if path != first {
			t.Fatal("File.toPath returned distinct cached objects")
		}
	}
}

func TestPortableJavaFileURIImporterPrecedenceAndNonASCIIBoundary(t *testing.T) {
	defaultRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = defaultRuntime.Close(context.Background()) })
	unsupported, err := defaultRuntime.Eval(context.Background(), "uri-nonascii-default.sl", `
debug(0);
import java.io.File;
$file = [new File: "/tmp/" . chr(233)];
return [[$file toURI] toASCIIString];
`)
	if err != nil || !unsupported.IsNull() {
		t.Fatalf("default non-ASCII URI.toASCIIString = (%s, %v), want explicit unsupported empty scalar", unsupported.Describe(), err)
	}

	var overridden bool
	runtimeInstance, err := New(WithObjectHost(ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
		if invocation.Message == "toASCIIString" {
			if class, ok := portableObjectClass(invocation.Target); ok && class == portableJavaURIClass {
				overridden = true
				return String("importer-ascii"), nil
			}
		}
		return Null(), &UnsupportedError{Operation: "object operation", Name: invocation.Message, Span: invocation.Span}
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	result, err := runtimeInstance.Eval(context.Background(), "uri-importer.sl", `
import java.io.File;
$file = [new File: "/tmp/" . chr(233)];
return [[$file toURI] toASCIIString];
`)
	if err != nil || result.String() != "importer-ascii" || !overridden {
		t.Fatalf("importer URI override = (%s, %v, %t)", result.Describe(), err, overridden)
	}
}

func TestPortableJavaFileURIURLPathOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for File URI/URL/Path differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSleep21JARSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSleep21JARSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	root := filepath.ToSlash(t.TempDir())
	source := portableJavaFileURIProbeSource(root)
	var goOutput bytes.Buffer
	runtimeInstance, err := New(WithStdout(&goOutput), WithStderr(&goOutput))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "file-uri-differential.sl", source); err != nil {
		t.Fatalf("OPFOR probe: %v\n%s", err, goOutput.String())
	}
	command := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", source)
	javaOutput, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep File URI/URL/Path probe: %v\n%s", err, javaOutput)
	}
	if !bytes.Equal(goOutput.Bytes(), javaOutput) {
		t.Fatalf("official Sleep File URI/URL/Path mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput.Bytes())
	}
}

func portableJavaFileURIProbeSource(root string) string {
	return fmt.Sprintf(`
import java.io.File;
import java.net.URI;
import java.net.URL;
import java.nio.file.Path;
import java.nio.file.Watchable;
$target = %s . "/opfor uri" . chr(35) . "frag" . chr(63) . ".txt";
$file = [new File: $target];
$uri = [$file toURI];
$url = [$file toURL];
$path = [$file toPath];
println([[$uri getClass] getName]);
println($uri);
println([$uri toString]);
println([$uri toASCIIString]);
println([$uri getScheme]);
println([$uri getRawSchemeSpecificPart]);
println([$uri getSchemeSpecificPart]);
println([$uri getRawPath]);
println([$uri getPath]);
println([$uri getAuthority]);
println([$uri getHost]);
println([$uri getPort]);
println([$uri isAbsolute]);
println([$uri isOpaque]);
println([$uri equals: [$file toURI]]);
println([$uri compareTo: [$file toURI]]);
println([$uri hashCode]);
if ($uri isa ^URI) { println(1); } else { println(0); }
if ($uri isa ^java.lang.Comparable) { println(1); } else { println(0); }
if ($uri isa ^java.io.Serializable) { println(1); } else { println(0); }
println([[$url getClass] getName]);
println($url);
println([$url toString]);
println([$url toExternalForm]);
println([$url getProtocol]);
println([$url getHost]);
println([$url getAuthority]);
println([$url getUserInfo]);
println([$url getPort]);
println([$url getDefaultPort]);
println([$url getPath]);
println([$url getFile]);
println([$url getQuery]);
println([$url getRef]);
println([$url equals: [$file toURL]]);
println([$url sameFile: [$file toURL]]);
println([$url hashCode]);
if ($url isa ^URL) { println(1); } else { println(0); }
if ($url isa ^java.io.Serializable) { println(1); } else { println(0); }
println([[$path getClass] getName]);
println($path);
if ($path is [$file toPath]) { println(1); } else { println(0); }
if ($path isa ^Path) { println(1); } else { println(0); }
if ($path isa ^Watchable) { println(1); } else { println(0); }
if ($path isa ^java.lang.Comparable) { println(1); } else { println(0); }
if ($path isa ^java.lang.Iterable) { println(1); } else { println(0); }
$back = [$path toFile];
println([$back getPath]);
println([$back equals: $file]);
if ($back is $file) { println(1); } else { println(0); }
println([[$file toURI] toString]);
println([[$file toURL] toExternalForm]);
$unicode = [new File: %s . "/caf" . chr(233)];
println([$unicode toURI]);
println([[$unicode toURI] getRawPath]);
println([[$unicode toURI] getPath]);
$invalid = [new File: "a" . chr(0) . "b"];
$invalid_url = [$invalid toURL];
$url_error = checkError();
println([[$url_error getClass] getName]);
$invalid_path = [$invalid toPath];
$path_error = checkError();
println([[$path_error getClass] getName]);
println([[new File: %s] toURI]);
println([[new File: %s] toURL]);
`, strconv.Quote(root), strconv.Quote(root), strconv.Quote(root), strconv.Quote(root))
}
