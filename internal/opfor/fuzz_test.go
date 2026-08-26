package opfor

import "testing"

func FuzzCompile(f *testing.F) {
	seeds := []string{
		`println("hello");`,
		`sub twice { return $1 * 2; } return twice(21);`,
		`on ready { println($1); }`,
		`@a = @(1, 2, 3); foreach $i => $v (@a) { @a[$i] = $v + 1; }`,
		`try { throw "boom"; } catch $error { warn($error); }`,
		`$object = [new Example: "value"]; [$object method: 1, 2];`,
		`"unterminated`,
		"\x00\xff\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, code string) {
		if len(code) > 64<<10 {
			t.Skip()
		}
		_, _ = CompileString("<fuzz>", code, WithCompatibilityWarnings())
	})
}
