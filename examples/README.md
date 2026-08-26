# Stock Sleep examples

These scripts use only the Sleep language and its stock built-ins. They do not
depend on Aggressor Script, Cobalt Strike, or application-provided host
functions.

Build OPFOR, then check or run any example from the repository root:

```sh
make
./opfor check examples/01-hello.sl
./opfor run examples/01-hello.sl operator
```

| Script | Demonstrates |
| --- | --- |
| `01-hello.sl` | Script arguments and string output |
| `02-functions.sl` | Subroutines and recursion |
| `03-control-flow.sl` | Loops, conditionals, and arithmetic |
| `04-collections.sl` | Arrays, hashes, sorting, and `foreach` |
| `05-closures.sl` | Closures with captured state |
| `06-functional.sl` | `map`, `filter`, and `reduce` |
