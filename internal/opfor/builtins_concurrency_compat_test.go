package opfor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sleepConcurrencyProbe = `sub invalid_acquire {
    acquire("x");
    println("acquire-tail");
}
invalid_acquire();
println("acquire-resume");
sub invalid_release {
    release(7);
    println("release-tail");
}
invalid_release();
println("release-resume");
$named = semaphore(x => 5);
println("named-sem=" . $named);
$sem = semaphore(1);
sub named_acquire {
    acquire(x => $sem);
    println("named-acquire-tail");
}
named_acquire();
println("after-named-acquire=" . $sem);
sub named_release {
    release(x => $sem);
    println("named-release-tail");
}
named_release();
println("after-named-release=" . $sem);
$take = function("&acquire");
$give = function("&release");
$make = function("&semaphore");
setf("&take", $take);
setf("&give", $give);
setf("&make", $make);
println("before=" . $sem);
println("take=" . take($sem));
println("after-take=" . $sem);
println("give=" . give($sem));
println("after-give=" . $sem);
println("make=" . make(4));
println("octal=" . semaphore("010"));
println("hex=" . semaphore("0x10"));
println("float=" . semaphore("1.5"));
$surface = semaphore(3);
println("count=" . [$surface getCount]);
println("isa=" . iff($surface isa ^sleep.bridges.Semaphore, "yes", "no"));
println("str=" . [$surface toString]);
[$surface P];
println("after-p=" . [$surface getCount]);
[$surface V];
println("after-v=" . [$surface getCount]);
try {
    acquire("try");
    println("try-tail");
}
catch $problem {
    println("caught=" . $problem);
}
println("try-resume");
`

const sleepConcurrencyOutput = `Warning: attempted an invalid cast: class java.lang.String cannot be cast to class sleep.bridges.Semaphore (java.lang.String is in module java.base of loader 'bootstrap'; sleep.bridges.Semaphore is in unnamed module of loader 'app') at opfor-concurrency-probe.sl:2
acquire-resume
Warning: attempted an invalid cast: class java.lang.Integer cannot be cast to class sleep.bridges.Semaphore (java.lang.Integer is in module java.base of loader 'bootstrap'; sleep.bridges.Semaphore is in unnamed module of loader 'app') at opfor-concurrency-probe.sl:8
release-resume
named-sem=[Semaphore: 0]
Warning: attempted an invalid cast: class sleep.bridges.KeyValuePair cannot be cast to class sleep.bridges.Semaphore (sleep.bridges.KeyValuePair and sleep.bridges.Semaphore are in unnamed module of loader 'app') at opfor-concurrency-probe.sl:17
after-named-acquire=[Semaphore: 1]
Warning: attempted an invalid cast: class sleep.bridges.KeyValuePair cannot be cast to class sleep.bridges.Semaphore (sleep.bridges.KeyValuePair and sleep.bridges.Semaphore are in unnamed module of loader 'app') at opfor-concurrency-probe.sl:23
after-named-release=[Semaphore: 1]
before=[Semaphore: 1]
take=
after-take=[Semaphore: 1]
give=
after-give=[Semaphore: 1]
make=
octal=[Semaphore: 10]
hex=[Semaphore: 0]
float=[Semaphore: 0]
count=3
isa=yes
str=[Semaphore: 3]
after-p=2
after-v=3
Warning: attempted an invalid cast: class java.lang.String cannot be cast to class sleep.bridges.Semaphore (java.lang.String is in module java.base of loader 'bootstrap'; sleep.bridges.Semaphore is in unnamed module of loader 'app') at opfor-concurrency-probe.sl:52
try-resume
`

func TestSleepConcurrencyCompatibility(t *testing.T) {
	if got := runSleepConcurrencyProbe(t); !bytes.Equal(got, []byte(sleepConcurrencyOutput)) {
		t.Fatalf("output mismatch\nwant:\n%sgot:\n%s", sleepConcurrencyOutput, got)
	}
}

func TestSleepConcurrencyOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, "opfor-concurrency-probe.sl")
	if err := os.WriteFile(path, []byte(sleepConcurrencyProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep concurrency probe: %v\n%s", err, want)
	}
	if got := runSleepConcurrencyProbe(t); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep concurrency output mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

func runSleepConcurrencyProbe(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeInstance.Eval(context.Background(), "opfor-concurrency-probe.sl", sleepConcurrencyProbe); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
