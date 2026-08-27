package opfor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const sleepNumericLiteralProbeName = "sleep-numeric-literals.sl"

const sleepNumericPredicateContextProbeName = "sleep-numeric-predicate-context.sl"

const sleepNumericSpecialConstructorProbeName = "sleep-numeric-special-constructor.sl"

const sleepNumericSpecialConstructorProbe = `println(NaN@(2));
println(Infinity%(a => 2));
println("done");
`

const sleepNumericSpecialConstructorProbeOutput = `Warning: Attempted to call non-existent function &NaN@ at sleep-numeric-special-constructor.sl:1

Warning: Attempted to call non-existent function &Infinity% at sleep-numeric-special-constructor.sl:2

done
`

const sleepNumericPredicateContextProbe = `if (-NaNfoo 1) { println("nan"); }
if (-InfinityD 1) { println("infinity"); }
if (-predicate 1) { println("ordinary"); }
if ((-grouped 1)) { println("grouped"); }
if (true && -logicalright 1) { println("right"); }
if (-logicalleft 1 && true) { println("left"); }
if (!-negated 1) { println("negated"); }
println("done");
`

const sleepNumericPredicateContextProbeOutput = `Warning: Attempted to use non-existent predicate: -NaNfoo at sleep-numeric-predicate-context.sl:1
Warning: Attempted to use non-existent predicate: -InfinityD at sleep-numeric-predicate-context.sl:2
Warning: Attempted to use non-existent predicate: -predicate at sleep-numeric-predicate-context.sl:3
Warning: Attempted to use non-existent predicate: -grouped at sleep-numeric-predicate-context.sl:4
Warning: Attempted to use non-existent predicate: -logicalright at sleep-numeric-predicate-context.sl:5
Warning: Attempted to use non-existent predicate: -logicalleft at sleep-numeric-predicate-context.sl:6
Warning: Attempted to use non-existent predicate: -negated at sleep-numeric-predicate-context.sl:7
negated
done
`

const sleepNumericLiteralProbe = `println(1F);
println(1f);
println(1.25F);
println(1e2f);
println(42L);
println(0x1D);
println(0x1F);
println(NaN);
println(Infinity);
println(+NaN);
println(-NaN);
println(+Infinity);
println(-Infinity);
println(0x1p0);
println(0X1P2);
println(0x1p+2);
println(0x1p-2);
println(0x1.p2);
println(0x1.8p1);
println(0x1p2F);
println(0x1p2f);
println(0x1p2D);
println(0x1p2d);
sub NaN { return "nan-function"; }
sub Infinity { return "infinity-function"; }
println(NaN());
println(Infinity());
println(09);
println(2147483648);
println(-2147483648);
println(-2147483649);
println(-0x80000000);
println(020000000000);
println(-020000000000);
println(-020000000001);
println(9223372036854775807L);
println(-9223372036854775808L);
println(١٢);
println(１２);
println(0x١٢);
println(0xＦ);
println(0xｆ);
println(٠٩);
println(0٧٧);
println(1e9999);
println(-1e9999);
println(1e-9999);
println(0x1p999999);
println(0x1p-999999);
println(0x1F."x");
println(42L."x");
println(1f."x");
println(1e2D."x");
println(1..2);
println(-0xＦ."x");
println(+0xＦ."x");
`

const sleepNumericLiteralProbeOutput = `1.0
1.0
1.25
100.0
42
29
31
NaN
Infinity
NaN
NaN
Infinity
-Infinity
1.0
4.0
4.0
0.25
4.0
3.0
4.0
4.0
4.0
4.0
nan-function
infinity-function
9.0
2.147483648E9
-2147483648
-2.147483649E9
-2147483648
2.0E10
-2147483648
-2.0000000001E10
9223372036854775807
-9223372036854775808
12
12
18
15
15
9
63
Infinity
-Infinity
0.0
Infinity
0.0
31x
42x
1.0x
100.0x
1.02
-15x
15x
`

func TestSleepNumericLiteralSuffixCompatibility(t *testing.T) {
	if got := runSleepNumericLiteralProbe(t); got != sleepNumericLiteralProbeOutput {
		t.Fatalf("numeric literal output mismatch\nwant:\n%sgot:\n%s", sleepNumericLiteralProbeOutput, got)
	}
}

func TestSleepNumericSpecialPredicateContextCompatibility(t *testing.T) {
	if got := runSleepNumericSource(t, sleepNumericPredicateContextProbeName, sleepNumericPredicateContextProbe); got != sleepNumericPredicateContextProbeOutput {
		t.Fatalf("numeric predicate-context output mismatch\nwant:\n%sgot:\n%s", sleepNumericPredicateContextProbeOutput, got)
	}
}

func TestSleepNumericSpecialConstructorCallCompatibility(t *testing.T) {
	if got := runSleepNumericSource(t, sleepNumericSpecialConstructorProbeName, sleepNumericSpecialConstructorProbe); got != sleepNumericSpecialConstructorProbeOutput {
		t.Fatalf("numeric special-constructor output mismatch\nwant:\n%sgot:\n%s", sleepNumericSpecialConstructorProbeOutput, got)
	}
}

func TestSleepRejectsNonReferenceNumericLiteralForms(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "lowercase decimal long", source: `println(42l);`},
		{name: "lowercase hexadecimal long", source: `println(0xffl);`},
		{name: "leading dot double", source: `println(.25);`},
		{name: "lowercase nan", source: `println(nan);`},
		{name: "uppercase nan", source: `println(NAN);`},
		{name: "lowercase infinity", source: `println(infinity);`},
		{name: "uppercase infinity", source: `println(INFINITY);`},
		{name: "nan suffix", source: `println(NaNF);`},
		{name: "infinity suffix", source: `println(InfinityD);`},
		{name: "hex fraction without exponent", source: `println(0x1.0);`},
		{name: "hex exponent without digits", source: `println(0x1p);`},
		{name: "hex signed exponent without digits", source: `println(0x1p+);`},
		{name: "hex missing integer significand", source: `println(0x.8p1);`},
		{name: "hex long suffix", source: `println(0x1.2p3L);`},
		{name: "positive hexadecimal integer overflow", source: `println(0x80000000);`},
		{name: "positive hexadecimal long overflow", source: `println(0x8000000000000000L);`},
		{name: "decimal long overflow", source: `println(9223372036854775808L);`},
		{name: "negative decimal long overflow", source: `println(-9223372036854775809L);`},
		{name: "Unicode integer overflow cannot become double", source: `println(٢١٤٧٤٨٣٦٤٨);`},
		{name: "supplementary decimal digit", source: `println(𝟙𝟚);`},
		{name: "supplementary hexadecimal digit", source: `println(0x𝟙);`},
		{name: "invalid mixed Unicode octal", source: `println(0٧٨);`},
		{name: "Unicode fraction digit", source: `println(1.٢);`},
		{name: "Unicode exponent digit", source: `println(1e٢);`},
		{name: "Unicode suffixed double", source: `println(١٢D);`},
		{name: "Go binary prefix", source: `println(0b10);`},
		{name: "Go octal prefix", source: `println(0o10);`},
		{name: "Go decimal separator", source: `println(1_0);`},
		{name: "Go hexadecimal separator", source: `println(0x1_0);`},
		{name: "Go infinity alias", source: `println(Inf);`},
		{name: "detached negative number", source: `println(- 1);`},
		{name: "detached positive number", source: `println(+ 1);`},
		{name: "grouped unary number", source: `println(-(1));`},
		{name: "unary scalar", source: `$value = 1; println(-$value);`},
		{name: "decimal dotted adjacency", source: `println(1.2."x");`},
		{name: "exponent dotted adjacency", source: `println(1e2."x");`},
		{name: "hexadecimal double dotted adjacency", source: `println(0x1p2."x");`},
		{name: "hexadecimal integer dotted adjacency", source: `println(0x12."x");`},
		{name: "signed hexadecimal integer dotted adjacency", source: `println(-0x12."x");`},
		{name: "positive signed hexadecimal integer dotted adjacency", source: `println(+0x12."x");`},
		{name: "signed fullwidth hexadecimal dotted adjacency", source: `println(-0xＦ９."x");`},
		{name: "positive signed fullwidth hexadecimal dotted adjacency", source: `println(+0xＦ９."x");`},
		{name: "signed hexadecimal double dotted adjacency", source: `println(-0x1p2."x");`},
		{name: "positive signed hexadecimal double dotted adjacency", source: `println(+0x1p2."x");`},
		{name: "signed exponent dotted adjacency", source: `println(-1e2."x");`},
		{name: "positive signed exponent dotted adjacency", source: `println(+1e2."x");`},
		{name: "signed malformed hexadecimal exponent", source: `println(-0x1p "x");`},
		{name: "signed malformed hexadecimal fraction", source: `println(-0x1.0 "x");`},
		{name: "adjacent ASCII numeric function", source: `println(1ticks());`},
		{name: "adjacent Arabic numeric function", source: `println(١ticks());`},
		{name: "adjacent fullwidth numeric function", source: `println(１２ticks());`},
		{name: "adjacent hexadecimal numeric function", source: `println(0x1ticks());`},
		{name: "adjacent fullwidth hexadecimal function", source: `println(0xＦticks());`},
		{name: "adjacent signed numeric function", source: `println(-1ticks());`},
		{name: "adjacent positive numeric function", source: `println(+1ticks());`},
		{name: "adjacent long suffix function", source: `println(1Lticks());`},
		{name: "adjacent decimal double function", source: `println(1.2ticks());`},
		{name: "adjacent exponent function", source: `println(1e2ticks());`},
		{name: "adjacent hexadecimal double function", source: `println(0x1p2ticks());`},
		{name: "adjacent boolean keyword", source: `println(1true);`},
		{name: "adjacent signed boolean keyword", source: `println(-1true);`},
		{name: "adjacent exponent boolean keyword", source: `println(1e2true);`},
		{name: "adjacent hexadecimal boolean keyword", source: `println(0x1true);`},
		{name: "adjacent hexadecimal double boolean keyword", source: `println(0x1p2true);`},
		{name: "adjacent fullwidth boolean keyword", source: `println(１２true);`},
		{name: "adjacent fullwidth hexadecimal boolean keyword", source: `println(0xＦtrue);`},
		{name: "adjacent float suffix function", source: `println(1Ffoo());`},
		{name: "adjacent double suffix function", source: `println(1Dfoo());`},
		{name: "adjacent scalar sigil", source: `$foo = 2; println(1$foo);`},
		{name: "adjacent array sigil", source: `@foo = @(2); println(1@foo);`},
		{name: "adjacent hash sigil", source: `%foo = %("key" => 2); println(1%foo);`},
		{name: "adjacent function sigil", source: `println(1&println);`},
		{name: "adjacent class sigil", source: `println(1^String);`},
		{name: "adjacent reference sigil", source: `$foo = 2; println(1\$foo);`},
		{name: "adjacent array constructor", source: `println(1@(2));`},
		{name: "adjacent hash constructor", source: `println(1%("key" => 2));`},
		{name: "adjacent nan scalar", source: `$foo = 2; println(NaN$foo);`},
		{name: "adjacent infinity array", source: `@foo = @(2); println(Infinity@foo);`},
		{name: "adjacent nan hash", source: `%foo = %("key" => 2); println(NaN%foo);`},
		{name: "adjacent infinity function", source: `println(Infinity&println);`},
		{name: "adjacent nan class", source: `println(NaN^String);`},
		{name: "adjacent infinity reference", source: `$foo = 2; println(Infinity\$foo);`},
		{name: "adjacent signed nan scalar", source: `$foo = 2; println(-NaN$foo);`},
		{name: "adjacent signed infinity array", source: `@foo = @(2); println(+Infinity@foo);`},
		{name: "adjacent nan word", source: `println(NaNfoo);`},
		{name: "adjacent infinity word", source: `println(Infinityfoo);`},
		{name: "signed malformed nan suffix", source: `println(-NaNF "x");`},
		{name: "signed malformed nan tail", source: `println(-NaNfoo "x");`},
		{name: "signed malformed infinity suffix", source: `println(-InfinityD "x");`},
		{name: "signed malformed infinity tail", source: `println(-Infinityfoo "x");`},
		{name: "positive malformed nan suffix", source: `println(+NaNF "x");`},
		{name: "negated malformed nan suffix", source: `println(!-NaNF "x");`},
		{name: "malformed special in conditional call argument", source: `if (println(-NaNfoo "x")) { println("yes"); } println("done");`},
		{name: "malformed special in conditional array", source: `if (@(-NaNfoo "x")) { println("yes"); } println("done");`},
		{name: "malformed special in conditional closure", source: `if ({ println(-NaNfoo "x"); }) { println("yes"); } println("done");`},
		{name: "malformed special in conditional index", source: `$a = @("z"); if ($a[-NaNfoo "x"]) { println("yes"); } println("done");`},
		{name: "custom predicate in value call", source: `println(-foo 1);`},
		{name: "builtin predicate in value call", source: `println(-isnumber 1);`},
		{name: "negated predicate in value call", source: `println(!-foo 1);`},
		{name: "predicate in arithmetic right operand", source: `if (1 + -foo 1) { println("yes"); }`},
		{name: "predicate in arithmetic left operand", source: `if (-foo 1 + 1) { println("yes"); }`},
		{name: "predicate in comparison left operand", source: `if ((-foo 1) == true) { println("yes"); }`},
		{name: "predicate in comparison right operand", source: `if (true == (-foo 1)) { println("yes"); }`},
		{name: "grouped predicate under negation", source: `if (!(-foo 1)) { println("yes"); }`},
		{name: "predicate in assignment", source: `$value = -foo 1;`},
		{name: "predicate in hash value", source: `$value = %("key" => -foo 1);`},
		{name: "boolean negation in value call", source: `println(!true);`},
		{name: "word not in predicate", source: `if (not true) { println("yes"); }`},
		{name: "bitwise not in value call", source: `println(~1);`},
		{name: "bitwise not in predicate", source: `if (~1) { println("yes"); }`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileString(test.name+".sl", test.source)
			var compileError *CompileError
			if !errors.As(err, &compileError) || len(compileError.Diagnostics) == 0 {
				t.Fatalf("CompileString error = %v, want source diagnostic", err)
			}
			found := false
			for _, diagnostic := range compileError.Diagnostics {
				if diagnostic.Message == "Unknown expression" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("diagnostics = %+v, want Unknown expression", compileError.Diagnostics)
			}
		})
	}
}

func TestSleepNumericIdentifierWhitespaceRemainsSeparate(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`sub ticks { return "tick"; } println(1 ticks());`,
		`sub ticks { return "tick"; } println(١ ticks());`,
		`sub ticks { return "tick"; } println(0x1 ticks());`,
		`println(1 true);`,
		`sub NaN { return "nan"; } sub Infinity { return "infinity"; } println(NaN()); println(Infinity());`,
		`println(NaN@(2));`,
		`println(Infinity%("key" => 2));`,
		`println(-NaN "x"); println(-Infinity "x");`,
		`if (-predicate 1) { println("predicate"); }`,
		`$foo = 2; println(1 $foo);`,
		`@foo = @(2); println(1 @foo);`,
		`%foo = %("key" => 2); println(1 %foo);`,
		`println(1 &println);`,
		`println(1 ^String);`,
		`$foo = 2; println(1 \$foo);`,
		`println(1 @(2));`,
		`println(1 %("key" => 2));`,
		`println(1 "text");`,
		`println(1 { return 2; });`,
		`println(1());`,
		`println(not(15));`,
		`$foo = 2; println(NaN $foo);`,
		`@foo = @(2); println(Infinity @foo);`,
		`println(NaN %("key" => 2));`,
		`$foo = 2; println(-NaN $foo);`,
		`@foo = @(2); println(+Infinity @foo);`,
		`sub NaN { return "nan"; } sub Infinity { return "infinity"; } println(NaN()); println(Infinity());`,
	} {
		if _, err := CompileString("numeric-identifier-space.sl", source); err != nil {
			t.Fatalf("CompileString(%q) = %v, want accepted separate arguments", source, err)
		}
	}
}

func TestSleepDigitTerminatingDotRemainsPartOfNumber(t *testing.T) {
	t.Parallel()

	for _, source := range []string{`println(42."x");`, `println(1."x");`} {
		var output bytes.Buffer
		runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
		if err != nil {
			t.Fatal(err)
		}
		_, evalErr := runtimeInstance.Eval(context.Background(), "digit-dot.sl", source)
		closeErr := runtimeInstance.Close(context.Background())
		if evalErr != nil || closeErr != nil {
			t.Fatalf("Eval(%q) = (%v, close %v), want accepted extra-argument warning", source, evalErr, closeErr)
		}
		if !bytes.Contains(output.Bytes(), []byte("Warning: expected I/O handle argument")) {
			t.Fatalf("Eval(%q) output = %q, want BasicIO extra-argument warning", source, output.String())
		}
	}
}

func TestSleepNumericLiteralOfficialJARDifferential(t *testing.T) {
	jar, java := officialSleepDifferentialTools(t)

	directory := t.TempDir()
	path := filepath.Join(directory, sleepNumericLiteralProbeName)
	if err := os.WriteFile(path, []byte(sleepNumericLiteralProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := officialSleepJavaCommand(java, "-jar", jar, path).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep numeric literal probe: %v\n%s", err, want)
	}
	if got := []byte(runSleepNumericLiteralProbe(t)); !bytes.Equal(got, want) {
		t.Fatalf("official Sleep numeric literal output mismatch\nwant:\n%sgot:\n%s", want, got)
	}

	predicatePath := filepath.Join(directory, sleepNumericPredicateContextProbeName)
	if err := os.WriteFile(predicatePath, []byte(sleepNumericPredicateContextProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	predicateWant, err := officialSleepJavaCommand(java, "-jar", jar, predicatePath).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep numeric predicate-context probe: %v\n%s", err, predicateWant)
	}
	if got := []byte(runSleepNumericSource(t, sleepNumericPredicateContextProbeName, sleepNumericPredicateContextProbe)); !bytes.Equal(got, predicateWant) {
		t.Fatalf("official Sleep numeric predicate-context output mismatch\nwant:\n%sgot:\n%s", predicateWant, got)
	}

	constructorPath := filepath.Join(directory, sleepNumericSpecialConstructorProbeName)
	if err := os.WriteFile(constructorPath, []byte(sleepNumericSpecialConstructorProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	constructorWant, err := officialSleepJavaCommand(java, "-jar", jar, constructorPath).CombinedOutput()
	if err != nil {
		t.Fatalf("official Sleep numeric special-constructor probe: %v\n%s", err, constructorWant)
	}
	if got := []byte(runSleepNumericSource(t, sleepNumericSpecialConstructorProbeName, sleepNumericSpecialConstructorProbe)); !bytes.Equal(got, constructorWant) {
		t.Fatalf("official Sleep numeric special-constructor output mismatch\nwant:\n%sgot:\n%s", constructorWant, got)
	}

	for _, invalid := range []string{
		`println(42l);`, `println(0xffl);`, `println(.25);`,
		`println(nan);`, `println(NAN);`, `println(infinity);`, `println(INFINITY);`,
		`println(NaNF);`, `println(InfinityD);`, `println(0x1.0);`,
		`println(0x1p);`, `println(0x1p+);`, `println(0x.8p1);`, `println(0x1.2p3L);`,
		`println(0x80000000);`, `println(0x8000000000000000L);`,
		`println(9223372036854775808L);`, `println(-9223372036854775809L);`,
		`println(٢١٤٧٤٨٣٦٤٨);`, `println(𝟙𝟚);`, `println(0x𝟙);`, `println(0٧٨);`,
		`println(1.٢);`, `println(1e٢);`, `println(١٢D);`,
		`println(0b10);`, `println(0o10);`, `println(1_0);`, `println(0x1_0);`, `println(Inf);`,
		`println(- 1);`, `println(+ 1);`, `println(-(1));`, `$value = 1; println(-$value);`,
		`println(1.2."x");`, `println(1e2."x");`, `println(0x1p2."x");`, `println(0x12."x");`,
		`println(-0x12."x");`, `println(+0x12."x");`,
		`println(-0xＦ９."x");`, `println(+0xＦ９."x");`,
		`println(-0x1p2."x");`, `println(+0x1p2."x");`,
		`println(-1e2."x");`, `println(+1e2."x");`,
		`println(-0x1p "x");`, `println(-0x1.0 "x");`,
		`println(1ticks());`, `println(١ticks());`, `println(１２ticks());`,
		`println(0x1ticks());`, `println(0xＦticks());`,
		`println(-1ticks());`, `println(+1ticks());`, `println(1Lticks());`,
		`println(1.2ticks());`, `println(1e2ticks());`, `println(0x1p2ticks());`,
		`println(1true);`, `println(-1true);`, `println(1e2true);`,
		`println(0x1true);`, `println(0x1p2true);`, `println(１２true);`, `println(0xＦtrue);`,
		`println(1Ffoo());`, `println(1Dfoo());`,
		`$foo = 2; println(1$foo);`, `@foo = @(2); println(1@foo);`,
		`%foo = %("key" => 2); println(1%foo);`, `println(1&println);`,
		`println(1^String);`, `$foo = 2; println(1\$foo);`,
		`println(1@(2));`, `println(1%("key" => 2));`,
		`$foo = 2; println(NaN$foo);`, `@foo = @(2); println(Infinity@foo);`,
		`%foo = %("key" => 2); println(NaN%foo);`, `println(Infinity&println);`,
		`println(NaN^String);`, `$foo = 2; println(Infinity\$foo);`,
		`$foo = 2; println(-NaN$foo);`, `@foo = @(2); println(+Infinity@foo);`,
		`println(NaNfoo);`, `println(Infinityfoo);`,
		`println(-NaNF "x");`, `println(-NaNfoo "x");`,
		`println(-InfinityD "x");`, `println(-Infinityfoo "x");`,
		`println(+NaNF "x");`, `println(!-NaNF "x");`,
		`if (println(-NaNfoo "x")) { println("yes"); } println("done");`,
		`if (@(-NaNfoo "x")) { println("yes"); } println("done");`,
		`if ({ println(-NaNfoo "x"); }) { println("yes"); } println("done");`,
		`$a = @("z"); if ($a[-NaNfoo "x"]) { println("yes"); } println("done");`,
		`println(-foo 1);`, `println(-isnumber 1);`, `println(!-foo 1);`,
		`if (1 + -foo 1) { println("yes"); }`, `if (-foo 1 + 1) { println("yes"); }`,
		`if ((-foo 1) == true) { println("yes"); }`, `if (true == (-foo 1)) { println("yes"); }`,
		`if (!(-foo 1)) { println("yes"); }`, `$value = -foo 1;`, `println(!true);`,
		`if (not true) { println("yes"); }`, `println(~1);`, `if (~1) { println("yes"); }`,
	} {
		output, runErr := officialSleepJavaCommand(java, "-jar", jar, "-e", invalid).CombinedOutput()
		if runErr != nil {
			t.Fatalf("official Sleep invalid numeric literal probe %q: %v\n%s", invalid, runErr, output)
		}
		if !bytes.Contains(output, []byte("Error: Unknown expression")) {
			t.Fatalf("official Sleep accepted invalid numeric literal %q:\n%s", invalid, output)
		}
	}

	for _, accepted := range []string{
		`$foo = 2; println(1 $foo);`,
		`@foo = @(2); println(1 @foo);`,
		`%foo = %("key" => 2); println(1 %foo);`,
		`println(1 &println);`,
		`println(1 ^String);`,
		`$foo = 2; println(1 \$foo);`,
		`println(1 @(2));`,
		`println(1 %("key" => 2));`,
		`println(1 "text");`,
		`println(1 { return 2; });`,
		`println(1());`,
		`println(not(15));`,
		`$foo = 2; println(NaN $foo);`,
		`@foo = @(2); println(Infinity @foo);`,
		`println(NaN %("key" => 2));`,
		`$foo = 2; println(-NaN $foo);`,
		`@foo = @(2); println(+Infinity @foo);`,
		`sub NaN { return "nan"; } sub Infinity { return "infinity"; } println(NaN()); println(Infinity());`,
		`println(NaN@(2));`,
		`println(Infinity%("key" => 2));`,
	} {
		output, runErr := officialSleepJavaCommand(java, "-jar", jar, "-e", accepted).CombinedOutput()
		if runErr != nil {
			t.Fatalf("official Sleep accepted numeric-separation probe %q: %v\n%s", accepted, runErr, output)
		}
		if bytes.Contains(output, []byte("Error: Unknown expression")) {
			t.Fatalf("official Sleep rejected numeric-separation control %q:\n%s", accepted, output)
		}
		if _, compileErr := CompileString("numeric-separation-control.sl", accepted); compileErr != nil {
			t.Fatalf("OPFOR rejected numeric-separation control %q: %v", accepted, compileErr)
		}
	}

	for _, warning := range []string{`println(42."x");`, `println(1."x");`} {
		output, runErr := officialSleepJavaCommand(java, "-jar", jar, "-e", warning).CombinedOutput()
		if runErr != nil {
			t.Fatalf("official Sleep digit-terminating dot probe %q: %v\n%s", warning, runErr, output)
		}
		if !bytes.Contains(output, []byte("Warning: expected I/O handle argument")) {
			t.Fatalf("official Sleep digit-terminating dot probe %q did not warn:\n%s", warning, output)
		}
	}
}

func runSleepNumericLiteralProbe(t *testing.T) string {
	t.Helper()
	return runSleepNumericSource(t, sleepNumericLiteralProbeName, sleepNumericLiteralProbe)
}

func runSleepNumericSource(t *testing.T, name, source string) string {
	t.Helper()
	var output bytes.Buffer
	runtimeInstance, err := New(WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	if _, err := runtimeInstance.Eval(context.Background(), name, source); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
