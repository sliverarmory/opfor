package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	osexec "os/exec"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func TestPortableJavaRandomNextBytesOpenJDKVector(t *testing.T) {
	t.Parallel()

	values := make([]Value, 8)
	for index := range values {
		values[index] = Int(0)
	}
	bytes := newPortableJavaArray(portableArrayType("byte"), []int{len(values)}, values)
	random := newPortableJavaRandom(0)
	result := invokePortableRandom(t, random, "nextBytes", ObjectValue(bytes))
	if !result.IsNull() {
		t.Fatalf("nextBytes result = %s, want null", result.Describe())
	}
	if got, want := sleepStringLowBytes(bytes.toSleepValue()), []byte{0x60, 0xb4, 0x20, 0xbb, 0x38, 0x51, 0xd9, 0xd4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nextBytes = % x, want % x", got, want)
	}
}

func TestPortableJavaRandomNextBytesLengthsAndStateConsumption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		length int
		want   []byte
	}{
		{length: 0, want: []byte{}},
		{length: 1, want: []byte{0x60}},
		{length: 3, want: []byte{0x60, 0xb4, 0x20}},
		{length: 5, want: []byte{0x60, 0xb4, 0x20, 0xbb, 0x38}},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.length), func(t *testing.T) {
			values := make([]Value, test.length)
			for index := range values {
				values[index] = Int(0)
			}
			bytes := newPortableJavaArray(portableArrayType("byte"), []int{test.length}, values)
			random := newPortableJavaRandom(0)
			invokePortableRandom(t, random, "nextBytes", ObjectValue(bytes))
			if got := sleepStringLowBytes(bytes.toSleepValue()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("nextBytes length %d = % x, want % x", test.length, got, test.want)
			}

			gotNext := invokePortableRandom(t, random, "nextInt")
			oracle := newPortableJavaRandom(0)
			if test.length != 0 {
				for consumed := 0; consumed < (test.length+3)/4; consumed++ {
					invokePortableRandom(t, oracle, "nextInt")
				}
			}
			wantNext := invokePortableRandom(t, oracle, "nextInt")
			if gotNext.Int32() != wantNext.Int32() {
				t.Fatalf("state after nextBytes length %d = %d, want %d", test.length, gotNext.Int32(), wantNext.Int32())
			}
		})
	}
}

func TestPortableJavaRandomNextBytesMutatesScriptByteArray(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-next-bytes.sl", `
$random = [new Random: 0L];
$bytes = cast("abcdefgh", "b");
[$random nextBytes: $bytes];
return scalar($bytes);
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sleepStringLowBytes(value), []byte{0x60, 0xb4, 0x20, 0xbb, 0x38, 0x51, 0xd9, 0xd4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("script nextBytes = % x, want % x", got, want)
	}
}

func TestPortableJavaRandomNextGaussianOpenJDKVectorAndCacheReset(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	first := invokePortableRandom(t, random, "nextGaussian")
	second := invokePortableRandom(t, random, "nextGaussian")
	if got, want := math.Float64bits(first.Float64()), math.Float64bits(0.8025330637390305); got != want {
		t.Fatalf("first nextGaussian = %.17g (%#x), want %.17g (%#x)", first.Float64(), got, 0.8025330637390305, want)
	}
	if got, want := math.Float64bits(second.Float64()), math.Float64bits(-0.9015460884175122); got != want {
		t.Fatalf("cached nextGaussian = %.17g (%#x), want %.17g (%#x)", second.Float64(), got, -0.9015460884175122, want)
	}

	if result := invokePortableRandom(t, random, "setSeed", Long(0)); !result.IsNull() {
		t.Fatalf("setSeed result = %s, want null", result.Describe())
	}
	replayed := invokePortableRandom(t, random, "nextGaussian")
	if math.Float64bits(replayed.Float64()) != math.Float64bits(first.Float64()) {
		t.Fatalf("nextGaussian after setSeed = %.17g, want replay %.17g", replayed.Float64(), first.Float64())
	}
}

func TestPortableJavaRandomGeneratorDistributionOpenJDKVectors(t *testing.T) {
	t.Parallel()

	vectors := []struct {
		seed        int64
		gaussian    float64
		exponential float64
		nextInt     int32
	}{
		{seed: 0, gaussian: -0.8751785398387515, exponential: 0.2308196630189923, nextInt: -1557280266},
		{seed: 1, gaussian: 1.0602577447059807, exponential: 0.5907070956022284, nextInt: 892128508},
		{seed: 2, gaussian: 1.7722230568253508, exponential: 3.558403221619518, nextInt: 2133836778},
		{seed: 3, gaussian: 0.29336365875896453, exponential: 0.032091981069249625, nextInt: 288278256},
		{seed: 7, gaussian: -1.5500510069141828, exponential: 2.1069415606801822, nextInt: 1495978761},
		{seed: 42, gaussian: 1.7199596972239914, exponential: 2.2341828857227215, nextInt: 1325939940},
		{seed: 123456789, gaussian: -4.417221304948319, exponential: 0.5068732607270126, nextInt: 1677212580},
		{seed: -1, gaussian: 4.2202378537836385, exponential: 0.016013690937172982, nextInt: -1451336087},
	}
	for _, vector := range vectors {
		t.Run(strconv.FormatInt(vector.seed, 10), func(t *testing.T) {
			random := newPortableJavaRandom(vector.seed)
			gaussian := invokePortableRandom(t, random, "nextGaussian", Double(2.5), Double(3))
			if got, want := math.Float64bits(gaussian.Float64()), math.Float64bits(vector.gaussian); got != want {
				t.Fatalf("nextGaussian(2.5, 3) = %.17g (%#x), want %.17g (%#x)", gaussian.Float64(), got, vector.gaussian, want)
			}
			exponential := invokePortableRandom(t, random, "nextExponential")
			if got, want := math.Float64bits(exponential.Float64()), math.Float64bits(vector.exponential); got != want {
				t.Fatalf("nextExponential = %.17g (%#x), want %.17g (%#x)", exponential.Float64(), got, vector.exponential, want)
			}
			if got := invokePortableRandom(t, random, "nextInt").Int32(); got != vector.nextInt {
				t.Fatalf("nextInt after distributions = %d, want %d", got, vector.nextInt)
			}
		})
	}
}

func TestPortableJavaRandomGeneratorDistributionSlowPathsOpenJDKVectors(t *testing.T) {
	t.Parallel()

	gaussianVectors := []struct {
		seed    int64
		value   float64
		nextInt int32
	}{
		{seed: 130, value: -0.14155142846364843, nextInt: -626927751},
		{seed: 146, value: -3.5080158393608016, nextInt: 1449772189},
		{seed: 354, value: -0.9477728871519981, nextInt: 1163836631},
		{seed: 58848, value: -3.874165290782437, nextInt: 1849177066},
		{seed: 112815, value: 3.9681003750341812, nextInt: 1113664822},
	}
	for _, vector := range gaussianVectors {
		t.Run("gaussian-"+strconv.FormatInt(vector.seed, 10), func(t *testing.T) {
			random := newPortableJavaRandom(vector.seed)
			got := invokePortableRandom(t, random, "nextGaussian", Double(0), Double(1))
			if gotBits, wantBits := math.Float64bits(got.Float64()), math.Float64bits(vector.value); gotBits != wantBits {
				t.Fatalf("nextGaussian(0, 1) = %.17g (%#x), want %.17g (%#x)", got.Float64(), gotBits, vector.value, wantBits)
			}
			if gotNext := invokePortableRandom(t, random, "nextInt").Int32(); gotNext != vector.nextInt {
				t.Fatalf("nextInt after Gaussian slow path = %d, want %d", gotNext, vector.nextInt)
			}
		})
	}

	exponentialVectors := []struct {
		seed    int64
		value   float64
		nextInt int32
	}{
		{seed: 162, value: 1.010217109536949, nextInt: -485360334},
		{seed: 130, value: 0.061099096767893386, nextInt: -626927751},
		{seed: 146, value: 8.595596863741516, nextInt: 1449772189},
		{seed: 354, value: 0.7454161868329514, nextInt: -768495167},
	}
	for _, vector := range exponentialVectors {
		t.Run("exponential-"+strconv.FormatInt(vector.seed, 10), func(t *testing.T) {
			random := newPortableJavaRandom(vector.seed)
			got := invokePortableRandom(t, random, "nextExponential")
			if gotBits, wantBits := math.Float64bits(got.Float64()), math.Float64bits(vector.value); gotBits != wantBits {
				t.Fatalf("nextExponential = %.17g (%#x), want %.17g (%#x)", got.Float64(), gotBits, vector.value, wantBits)
			}
			if gotNext := invokePortableRandom(t, random, "nextInt").Int32(); gotNext != vector.nextInt {
				t.Fatalf("nextInt after exponential slow path = %d, want %d", gotNext, vector.nextInt)
			}
		})
	}
}

func TestPortableJavaRandomGeneratorZigguratTableIntegrity(t *testing.T) {
	t.Parallel()

	if got, want := len(portableExponentialX), 253; got != want {
		t.Fatalf("exponential X length = %d, want %d", got, want)
	}
	if got, want := len(portableExponentialY), 253; got != want {
		t.Fatalf("exponential Y length = %d, want %d", got, want)
	}
	if got, want := len(portableNormalX), 254; got != want {
		t.Fatalf("normal X length = %d, want %d", got, want)
	}
	if got, want := len(portableNormalY), 254; got != want {
		t.Fatalf("normal Y length = %d, want %d", got, want)
	}
	for name, length := range map[string]int{
		"exponential alias map":       len(portableExponentialAliasMap),
		"exponential alias threshold": len(portableExponentialAliasThreshold),
		"normal alias map":            len(portableNormalAliasMap),
		"normal alias threshold":      len(portableNormalAliasThreshold),
	} {
		if length != 256 {
			t.Fatalf("%s length = %d, want 256", name, length)
		}
	}

	hash := sha256.New()
	writeUint64 := func(value uint64) {
		var encoded [8]byte
		binary.LittleEndian.PutUint64(encoded[:], value)
		_, _ = hash.Write(encoded[:])
	}
	_, _ = hash.Write(portableExponentialAliasMap[:])
	for _, value := range portableExponentialAliasThreshold {
		writeUint64(uint64(value))
	}
	for _, values := range [][]float64{portableExponentialX[:], portableExponentialY[:]} {
		for _, value := range values {
			writeUint64(math.Float64bits(value))
		}
	}
	_, _ = hash.Write(portableNormalAliasMap[:])
	for _, value := range portableNormalAliasThreshold {
		writeUint64(uint64(value))
	}
	for _, values := range [][]float64{portableNormalX[:], portableNormalY[:]} {
		for _, value := range values {
			writeUint64(math.Float64bits(value))
		}
	}
	const want = "3699bba2aad5dcc9e4d65cd28229ae5f0faa2451d4d8be9de654c6c06d492d51"
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != want {
		t.Fatalf("ziggurat table digest = %s, want %s", got, want)
	}
}

func TestPortableJavaRandomGeneratorDistributionsOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for RandomGenerator distribution verification")
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

	source := `
$r = [new Random: 0L];
$g = [$r nextGaussian: 2.5, 3.0]; $e = [$r nextExponential]; $i = [$r nextInt];
println("0|$g|$e|$i");
$r = [new Random: -1L];
$g = [$r nextGaussian: 2.5, 3.0]; $e = [$r nextExponential]; $i = [$r nextInt];
println("-1|$g|$e|$i");
$r = [new Random: 146L];
$g = [$r nextGaussian: 0.0, 1.0]; $i = [$r nextInt];
println("g146|$g|$i");
$r = [new Random: 146L];
$e = [$r nextExponential]; $i = [$r nextInt];
println("e146|$e|$i");
$r = [new Random: 58848L];
$g = [$r nextGaussian: 0.0, 1.0]; $i = [$r nextInt];
println("g58848|$g|$i");
$r = [new Random: 112815L];
$g = [$r nextGaussian: 0.0, 1.0]; $i = [$r nextInt];
println("g112815|$g|$i");
`
	command := osexec.Command(java, "-Dfile.encoding=UTF-8", "-jar", jar, "-e", source)
	want, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep RandomGenerator probe: %v\n%s", err, want)
	}

	var got bytes.Buffer
	runtimeInstance, err := New(WithStdout(&got), WithStderr(&got))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), "random-generator-distributions.sl", source); err != nil {
		t.Fatalf("pure-Go RandomGenerator probe: %v\n%s", err, got.String())
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("official RandomGenerator distribution mismatch\nwant:\n%s\ngot:\n%s", want, got.Bytes())
	}
}

func TestPortableJavaRandomGeneratorDistributionArgumentsAndState(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	value, handled, err := random.invoke(ObjectInvocation{
		Op:      ObjectInvoke,
		Message: "nextGaussian",
		Arguments: []Argument{
			{Value: Double(1)},
			{Value: Double(-1)},
		},
	})
	var exception *portableJavaException
	if !handled || !value.IsNull() || !errors.As(err, &exception) {
		t.Fatalf("negative standard deviation = (%s, %v, %v), want portable exception", value.Describe(), handled, err)
	}
	if exception.class != "java.lang.IllegalArgumentException" || exception.message != "standard deviation must be non-negative" {
		t.Fatalf("negative standard deviation exception = %s: %q", exception.class, exception.message)
	}
	if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1155484576 {
		t.Fatalf("invalid Gaussian consumed state: nextInt = %d", got)
	}

	random = newPortableJavaRandom(0)
	zero := invokePortableRandom(t, random, "nextGaussian", Double(9), Double(0))
	if got, want := math.Float64bits(zero.Float64()), math.Float64bits(9); got != want {
		t.Fatalf("zero-deviation Gaussian = %.17g (%#x), want 9 (%#x)", zero.Float64(), got, want)
	}
	oracle := newPortableJavaRandom(0)
	invokePortableRandom(t, oracle, "nextLong")
	if got, want := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); got != want {
		t.Fatalf("zero-deviation state = %d, want %d", got, want)
	}

	random = newPortableJavaRandom(0)
	nan := invokePortableRandom(t, random, "nextGaussian", Double(1), Double(math.NaN()))
	if !math.IsNaN(nan.Float64()) {
		t.Fatalf("NaN-deviation Gaussian = %s, want NaN", nan.Describe())
	}
	oracle = newPortableJavaRandom(0)
	invokePortableRandom(t, oracle, "nextLong")
	if got, want := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); got != want {
		t.Fatalf("NaN-deviation state = %d, want %d", got, want)
	}

	negativeZero := math.Copysign(0, -1)
	tests := []struct {
		name              string
		mean              float64
		standardDeviation float64
		want              float64
		wantNaN           bool
	}{
		{name: "negative zero deviation", mean: 0, standardDeviation: negativeZero, want: 0},
		{name: "negative zero mean", mean: negativeZero, standardDeviation: 0, want: negativeZero},
		{name: "positive infinite deviation", mean: 1, standardDeviation: math.Inf(1), want: math.Inf(-1)},
		{name: "positive infinite mean", mean: math.Inf(1), standardDeviation: 1, want: math.Inf(1)},
		{name: "negative infinite mean", mean: math.Inf(-1), standardDeviation: 1, want: math.Inf(-1)},
		{name: "NaN mean", mean: math.NaN(), standardDeviation: 1, wantNaN: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			random := newPortableJavaRandom(0)
			got := invokePortableRandom(
				t,
				random,
				"nextGaussian",
				Double(test.mean),
				Double(test.standardDeviation),
			).Float64()
			if test.wantNaN {
				if !math.IsNaN(got) {
					t.Fatalf("Gaussian = %.17g, want NaN", got)
				}
			} else if gotBits, wantBits := math.Float64bits(got), math.Float64bits(test.want); gotBits != wantBits {
				t.Fatalf("Gaussian = %.17g (%#x), want %.17g (%#x)", got, gotBits, test.want, wantBits)
			}
			oracle := newPortableJavaRandom(0)
			invokePortableRandom(t, oracle, "nextLong")
			if gotNext, wantNext := invokePortableRandom(t, random, "nextInt").Int32(), invokePortableRandom(t, oracle, "nextInt").Int32(); gotNext != wantNext {
				t.Fatalf("post-Gaussian state = %d, want %d", gotNext, wantNext)
			}
		})
	}
}

func TestPortableJavaRandomGeneratorGaussianDoesNotUseClassicCache(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	first := invokePortableRandom(t, random, "nextGaussian")
	invokePortableRandom(t, random, "nextGaussian", Double(0), Double(1))
	cached := invokePortableRandom(t, random, "nextGaussian")
	if got, want := math.Float64bits(first.Float64()), math.Float64bits(0.8025330637390305); got != want {
		t.Fatalf("classic first Gaussian = %.17g, want %.17g", first.Float64(), 0.8025330637390305)
	}
	if got, want := math.Float64bits(cached.Float64()), math.Float64bits(-0.9015460884175122); got != want {
		t.Fatalf("classic cached Gaussian after generator overload = %.17g, want %.17g", cached.Float64(), -0.9015460884175122)
	}
}

func TestPortableJavaRandomNextBytesNullUsesJavaSoftError(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtimeInstance.Eval(context.Background(), "random-next-bytes-null.sl", `
$random = [new Random: 0L];
$result = [$random nextBytes: $null];
$error = checkError();
return @($result, [[$error getClass] getName], [$error getMessage]);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	if got, want := argvValueStrings(array.Values()), []string{"", "java.lang.NullPointerException", "Cannot read the array length because bytes is null"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nextBytes null soft error = %q, want %q", got, want)
	}
}

func TestPortableJavaRandomNewMethodSignatureRejection(t *testing.T) {
	t.Parallel()

	byteArray := ObjectValue(newPortableJavaArray(portableArrayType("byte"), []int{1}, []Value{Int(0)}))
	intArray := ObjectValue(newPortableJavaArray(portableArrayType("int"), []int{1}, []Value{Int(0)}))
	tests := []struct {
		name      string
		message   string
		arguments []Value
	}{
		{name: "nextBytes omitted", message: "nextBytes"},
		{name: "nextBytes extra", message: "nextBytes", arguments: []Value{byteArray, byteArray}},
		{name: "nextBytes wrong array", message: "nextBytes", arguments: []Value{intArray}},
		{name: "nextGaussian one argument", message: "nextGaussian", arguments: []Value{Double(1)}},
		{name: "nextGaussian three arguments", message: "nextGaussian", arguments: []Value{Double(1), Double(2), Double(3)}},
		{name: "nextExponential extra", message: "nextExponential", arguments: []Value{Double(1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arguments := make([]Argument, len(test.arguments))
			for index, value := range test.arguments {
				arguments[index] = Argument{Value: value}
			}
			result, handled, err := newPortableJavaRandom(0).invoke(ObjectInvocation{
				Op: ObjectInvoke, Message: test.message, Arguments: arguments,
			})
			if err != nil || !handled || !result.IsNull() {
				t.Fatalf("%s rejection = (%s, handled %v, %v), want (null, true, nil)", test.name, result.Describe(), handled, err)
			}
		})
	}
}

func TestPortableJavaRandomConcurrentGeneratorDistributionStateTransitions(t *testing.T) {
	t.Parallel()

	const workers = 8
	const calls = 50
	random := newPortableJavaRandom(7)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range calls {
				gaussian, handled, err := random.invoke(ObjectInvocation{
					Op:      ObjectInvoke,
					Message: "nextGaussian",
					Arguments: []Argument{
						{Value: Double(2)},
						{Value: Double(3)},
					},
				})
				if err != nil || !handled || gaussian.Kind() != KindDouble {
					t.Errorf("concurrent nextGaussian(mean,stddev) = (%s, %v, %v)", gaussian.Describe(), handled, err)
					return
				}
				exponential, handled, err := random.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "nextExponential"})
				if err != nil || !handled || exponential.Kind() != KindDouble || exponential.Float64() < 0 {
					t.Errorf("concurrent nextExponential = (%s, %v, %v)", exponential.Describe(), handled, err)
					return
				}
			}
		}()
	}
	wait.Wait()
}

func TestPortableJavaRandomConcurrentGaussianStateTransitions(t *testing.T) {
	t.Parallel()

	const workers = 8
	const calls = 100
	random := newPortableJavaRandom(7)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range calls {
				value, handled, err := random.invoke(ObjectInvocation{Op: ObjectInvoke, Message: "nextGaussian"})
				if err != nil || !handled || value.Kind() != KindDouble {
					t.Errorf("concurrent nextGaussian = (%s, %v, %v)", value.Describe(), handled, err)
					return
				}
			}
		}()
	}
	wait.Wait()

	oracle := newPortableJavaRandom(7)
	for range workers * calls {
		invokePortableRandom(t, oracle, "nextGaussian")
	}
	got := invokePortableRandom(t, random, "nextGaussian")
	want := invokePortableRandom(t, oracle, "nextGaussian")
	if math.Float64bits(got.Float64()) != math.Float64bits(want.Float64()) {
		t.Fatalf("post-concurrency nextGaussian = %.17g, want %.17g", got.Float64(), want.Float64())
	}
}

func TestPortableJavaRandomGeneratorBoundedOverloadsOpenJDKVectors(t *testing.T) {
	t.Parallel()

	intVectors := []struct {
		name string
		args []Value
		want int32
	}{
		{name: "range", args: []Value{Int(-50), Int(75)}, want: 60},
		{name: "power range", args: []Value{Int(-16), Int(16)}, want: -16},
		{name: "overflow range", args: []Value{Int(math.MinInt32), Int(math.MaxInt32)}, want: -1155484576},
		{name: "rejection range", args: []Value{Int(0), Int(1073741825)}, want: 516548029},
	}
	for _, vector := range intVectors {
		t.Run("nextInt "+vector.name, func(t *testing.T) {
			got := invokePortableRandom(t, newPortableJavaRandom(0), "nextInt", vector.args...)
			if got.Int32() != vector.want {
				t.Fatalf("nextInt%v = %d, want %d", vector.args, got.Int32(), vector.want)
			}
		})
	}

	longVectors := []struct {
		name string
		args []Value
		want int64
	}{
		{name: "bound", args: []Value{Long(1000)}, want: 860},
		{name: "power bound", args: []Value{Long(1024)}, want: 312},
		{name: "range", args: []Value{Long(-50), Long(75)}, want: 60},
		{name: "power range", args: []Value{Long(-512), Long(512)}, want: -200},
		{name: "overflow range", args: []Value{Long(math.MinInt64), Long(math.MaxInt64)}, want: -4962768465676381896},
		{name: "rejection bound", args: []Value{Long(4611686018427387905)}, want: 2218556890522892383},
	}
	for _, vector := range longVectors {
		t.Run("nextLong "+vector.name, func(t *testing.T) {
			got := invokePortableRandom(t, newPortableJavaRandom(0), "nextLong", vector.args...)
			if got.Int64() != vector.want {
				t.Fatalf("nextLong%v = %d, want %d", vector.args, got.Int64(), vector.want)
			}
		})
	}

	floatVectors := []struct {
		name string
		args []Value
		want uint32
	}{
		{name: "bound", args: []Value{Double(10)}, want: 0x40e9e8e1},
		{name: "range", args: []Value{Double(-5), Double(10)}, want: 0x40bedd52},
		{name: "overflow range", args: []Value{Double(-math.MaxFloat32), Double(math.MaxFloat32)}, want: 0x7eec82ce},
		{name: "smallest bound correction", args: []Value{Double(math.SmallestNonzeroFloat32)}, want: 0},
		{name: "adjacent range correction", args: []Value{Double(1), Double(float64(math.Nextafter32(1, 2)))}, want: 0x3f800000},
	}
	for _, vector := range floatVectors {
		t.Run("nextFloat "+vector.name, func(t *testing.T) {
			got := invokePortableRandom(t, newPortableJavaRandom(0), "nextFloat", vector.args...)
			if bits := math.Float32bits(float32(got.Float64())); bits != vector.want {
				t.Fatalf("nextFloat%v = %.9g (%08x), want bits %08x", vector.args, got.Float64(), bits, vector.want)
			}
		})
	}

	doubleVectors := []struct {
		name string
		args []Value
		want uint64
	}{
		{name: "bound", args: []Value{Double(10)}, want: 0x401d3d1c32507d2b},
		{name: "range", args: []Value{Double(-5), Double(10)}, want: 0x4017dbaa4b78bbc0},
		{name: "overflow range", args: []Value{Double(-math.MaxFloat64), Double(math.MaxFloat64)}, want: 0x7fdd905a3a9b2a22},
		{name: "smallest bound correction", args: []Value{Double(math.SmallestNonzeroFloat64)}, want: 0},
		{name: "adjacent range correction", args: []Value{Double(1), Double(math.Nextafter(1, 2))}, want: 0x3ff0000000000000},
	}
	for _, vector := range doubleVectors {
		t.Run("nextDouble "+vector.name, func(t *testing.T) {
			got := invokePortableRandom(t, newPortableJavaRandom(0), "nextDouble", vector.args...)
			if bits := math.Float64bits(got.Float64()); bits != vector.want {
				t.Fatalf("nextDouble%v = %.17g (%016x), want bits %016x", vector.args, got.Float64(), bits, vector.want)
			}
		})
	}
}

func TestPortableJavaRandomGeneratorRejectionConsumesOpenJDKState(t *testing.T) {
	t.Parallel()

	intRandom := newPortableJavaRandom(0)
	if got := invokePortableRandom(t, intRandom, "nextInt", Int(0), Int(1073741825)).Int32(); got != 516548029 {
		t.Fatalf("rejection nextInt = %d, want 516548029", got)
	}
	if got := invokePortableRandom(t, intRandom, "nextInt").Int32(); got != -1690734402 {
		t.Fatalf("nextInt after rejection = %d, want -1690734402", got)
	}

	longRandom := newPortableJavaRandom(0)
	if got := invokePortableRandom(t, longRandom, "nextLong", Long(4611686018427387905)).Int64(); got != 2218556890522892383 {
		t.Fatalf("rejection nextLong = %d, want 2218556890522892383", got)
	}
	if got := invokePortableRandom(t, longRandom, "nextInt").Int32(); got != -1557280266 {
		t.Fatalf("nextInt after long rejection = %d, want -1557280266", got)
	}
}

func TestPortableJavaRandomGeneratorOverloadsPreserveOpenJDKStateConsumption(t *testing.T) {
	t.Parallel()

	random := newPortableJavaRandom(0)
	if got := invokePortableRandom(t, random, "nextInt", Int(-50), Int(75)).Int32(); got != 60 {
		t.Fatalf("sequential nextInt = %d, want 60", got)
	}
	if got := invokePortableRandom(t, random, "nextLong", Long(1000)).Int64(); got != 637 {
		t.Fatalf("sequential nextLong = %d, want 637", got)
	}
	if got := math.Float32bits(float32(invokePortableRandom(t, random, "nextFloat", Double(-5), Double(10)).Float64())); got != 0x40830bb2 {
		t.Fatalf("sequential nextFloat bits = %08x, want 40830bb2", got)
	}
	if got := math.Float64bits(invokePortableRandom(t, random, "nextDouble", Double(-5), Double(10)).Float64()); got != 0x40123ebb4da2c112 {
		t.Fatalf("sequential nextDouble bits = %016x, want 40123ebb4da2c112", got)
	}
	if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1930858313 {
		t.Fatalf("nextInt after overload sequence = %d, want -1930858313", got)
	}
}

func TestPortableJavaRandomGeneratorInvalidRangesUseSoftErrorsWithoutConsumingState(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-generator-errors.sl", `
$random = [new Random: 0L];
[$random nextInt: 4, 4];
$int_range = checkError();
[$random nextLong: 0L];
$long_bound = checkError();
[$random nextLong: 4L, 4L];
$long_range = checkError();
[$random nextFloat: 0.0];
$float_bound = checkError();
[$random nextFloat: 1.0, 1.0];
$float_range = checkError();
[$random nextDouble: 0.0];
$double_bound = checkError();
[$random nextDouble: 1.0, 1.0];
$double_range = checkError();
return @(
	[$int_range getMessage], [$long_bound getMessage], [$long_range getMessage],
	[$float_bound getMessage], [$float_range getMessage],
	[$double_bound getMessage], [$double_range getMessage],
	[$random nextInt]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{
		"bound must be greater than origin",
		"bound must be positive",
		"bound must be greater than origin",
		"bound must be finite and positive",
		"bound must be greater than origin",
		"bound must be finite and positive",
		"bound must be greater than origin",
		"-1155484576",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("RandomGenerator soft errors/state = %q, want %q", got, want)
	}
}

func TestPortableJavaRandomGeneratorNonFiniteArgumentsUseExactJavaErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		message   string
		arguments []Value
		want      string
	}{
		{name: "float positive infinity bound", message: "nextFloat", arguments: []Value{Double(math.Inf(1))}, want: "bound must be finite and positive"},
		{name: "float NaN bound", message: "nextFloat", arguments: []Value{Double(math.NaN())}, want: "bound must be finite and positive"},
		{name: "float negative infinity origin", message: "nextFloat", arguments: []Value{Double(math.Inf(-1)), Double(1)}, want: "bound must be greater than origin"},
		{name: "double positive infinity bound", message: "nextDouble", arguments: []Value{Double(math.Inf(1))}, want: "bound must be finite and positive"},
		{name: "double NaN bound", message: "nextDouble", arguments: []Value{Double(math.NaN())}, want: "bound must be finite and positive"},
		{name: "double positive infinity range bound", message: "nextDouble", arguments: []Value{Double(1), Double(math.Inf(1))}, want: "bound must be greater than origin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			random := newPortableJavaRandom(0)
			arguments := make([]Argument, len(test.arguments))
			for index, value := range test.arguments {
				arguments[index] = Argument{Value: value}
			}
			value, handled, err := random.invoke(ObjectInvocation{
				Op: ObjectInvoke, Message: test.message, Arguments: arguments,
			})
			var exception *portableJavaException
			if !handled || !value.IsNull() || !errors.As(err, &exception) {
				t.Fatalf("%s = (%s, handled %v, %v), want portable exception", test.message, value.Describe(), handled, err)
			}
			if exception.class != "java.lang.IllegalArgumentException" || exception.message != test.want {
				t.Fatalf("exception = %s: %q, want IllegalArgumentException: %q", exception.class, exception.message, test.want)
			}
			if got := invokePortableRandom(t, random, "nextInt").Int32(); got != -1155484576 {
				t.Fatalf("nextInt after invalid argument = %d, want unchanged state -1155484576", got)
			}
		})
	}
}

func TestPortableJavaRandomGeneratorTypeAndFromFactory(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "random-generator-related.sl", `
$random = [new Random: 0L];
$same = [Random from: $random];
[Random from: $null];
$error = checkError();
return @(
	$random isa ^java.util.random.RandomGenerator,
	[$random isDeprecated],
	[$same nextInt],
	[$random nextInt],
	[[$error getClass] getName],
	[$error getMessage]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	// Sleep evaluates array-expression arguments from right to left, so the
	// second source expression consumes the first value from the shared object.
	if got, want := argvValueStrings(array.Values()), []string{"1", "0", "-723955400", "-1155484576", "java.lang.NullPointerException", ""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RandomGenerator type/from = %q, want %q", got, want)
	}
}
