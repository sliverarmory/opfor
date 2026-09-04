# Interpreter performance experiments, 2026-09-04

Branch: `feat/interpreter-performance`. Baseline: `main` at `6a8a9ae5832953429c9066ce13f63261e7d8ee56`. The candidate comprises the interpreter changes described below, tested on an Apple M5 Max, macOS `darwin/arm64`, Go 1.27.1, and OpenJDK 26.0.2.1.

The first pass reduces allocations and improves several execution workloads. Five alternating Go benchmark passes measured **16.9% lower regex time**, **11.4% lower bracket-call time**, **4.4% lower multiargument-call time**, and **4.1–5.6% lower array-append time**. Arithmetic and indexing throughput are essentially unchanged, despite eliminating most numeric boxing allocations. The warmed JVM remains faster on all seven comparison workloads; these changes do not close the overall execution gap.

## Changes retained

- Store 32-bit integers, 64-bit integers, and floating-point bits directly in `Value`. Preserve signed conversions, NaN payloads, signed zero, taint, identity rules, and serialization. Field ordering keeps `Value` at 80 bytes on 64-bit Go and WASM; it grows from 40 to 44 bytes on 386/ARM. No `unsafe` representation tricks are used.
- Build call arguments directly in their final slice. Preserve right-to-left comma-term evaluation, left-to-right omitted-comma evaluation, named pairs, and live scalar references.
- Allocate named-call error classification state only when an error exists. Format closure trace descriptions only when tracing is enabled, including changes to debug flags during a call.
- Share the root array cache with its already-detached initial storage. Keep the input-slice defensive copy and independent sublist caches.
- Reuse ordinary text directly as its canonical string spelling and count UTF-16 length without temporary buffers. Explicit binary provenance, invalid UTF-8, and lone surrogates retain the exact-unit path.

Cancellation checks, instruction/resource limits, lifecycle leases, and host dispatch behavior were retained. No public API or dependency changes were needed.

## Go execution measurements

Each row is the median of five separate process runs with `-benchtime=300ms -count=1`, alternating baseline/candidate order. No test suites or compilation processes were intentionally run alongside these measurements. `ns/op`, `B/op`, and allocations are per complete benchmark function call, not per loop iteration. Negative time changes favor the branch. Sub-percent differences are not treated as throughput wins; no statistical significance claim is made.

| Workload | Main ns/op | Branch ns/op | Time change | Main → branch B/op | Main → branch allocs/op |
|---|---:|---:|---:|---:|---:|
| MultiArgumentCalls | 2,275,391 | 2,174,562 | -4.4% | 3,978,306 → 3,421,015 | 40,654 → 31,079 |
| BracketClosureCalls | 1,823,433 | 1,616,039 | -11.4% | 2,245,337 → 2,055,013 | 32,473 → 23,079 |
| ArithmeticLoop/unmetered | 723,514 | 727,828 | +0.6% | 11,873 → 4,993 | 1,801 → 79 |
| ArithmeticLoop/metered | 725,317 | 729,756 | +0.6% | 11,937 → 5,057 | 1,803 → 81 |
| FunctionCalls/taint-disabled | 1,743,665 | 1,729,556 | -0.8% | 2,578,967 → 2,525,003 | 34,569 → 27,079 |
| FunctionCalls/taint-enabled | 1,792,245 | 1,767,402 | -1.4% | 2,658,970 → 2,605,005 | 35,569 → 28,079 |
| NativeCalls/taint-disabled | 1,439,440 | 1,414,439 | -1.7% | 675,208 → 629,252 | 18,571 → 12,081 |
| NativeCalls/taint-enabled | 1,523,641 | 1,506,659 | -1.1% | 835,210 → 789,251 | 20,571 → 14,081 |
| ArrayIndexRead | 981,406 | 983,842 | +0.2% | 14,363 → 7,538 | 1,785 → 97 |
| ArrayAppend/1000 | 1,568,639 | 1,499,534 | -4.4% | 1,365,273 → 997,292 | 20,861 → 14,110 |
| ArrayAppend/2000 | 3,132,998 | 3,005,016 | -4.1% | 2,740,191 → 2,001,546 | 41,863 → 28,112 |
| ArrayAppend/10000 | 16,110,665 | 15,200,453 | -5.6% | 13,755,932 → 10,051,989 | 209,868 → 140,117 |
| ForeachArray | 2,054,158 | 1,963,478 | -4.4% | 1,363,491 → 988,677 | 22,566 → 14,100 |
| StringASCII | 228,810 | 230,555 | +0.8% | 184,065 → 175,953 | 600 → 592 |
| RegexRepeatedPattern | 1,608,233 | 1,336,691 | -16.9% | 418,991 → 117,027 | 9,569 → 2,079 |
| LiteralLoop | 2,312,486 | 2,249,462 | -2.7% | 689,227 → 621,257 | 20,826 → 12,081 |
| ClosureLiteralLoop | 2,112,463 | 2,112,619 | +0.0% | 2,827,857 → 2,821,008 | 31,788 → 30,079 |

Arithmetic allocation count falls **95.6%** (1,801 → 79); array indexing falls **94.6%** (1,785 → 97). This improves allocation pressure even where elapsed time does not improve. The new multiargument and bracket-call workloads run unchanged against both implementations.

## Official Sleep 2.1 JVM comparison

The existing `sleepcompare` harness used identical sources and checked the expected final results in both runtimes. It excludes process startup, Java helper compilation, runtime creation, and script loading from execution timing. The official JAR was downloaded from `https://sleep.dashnine.org/download/sleep.jar` and authenticated against the repository pin:

```text
0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1
```

Three comparison processes per Go variant used 25 samples, 200 warmup operations, 30 execution calls per sample, and 15 compilations per sample. Baseline/candidate process order alternated. Main and branch columns below are medians of their three reported medians; the JVM column pools the six reported JVM medians into one common reference. JVM ranges show the minimum and maximum of those six medians. The ratio divides the branch median by that common JVM reference.

| Execution workload | Main ns/op | Branch ns/op | JVM ns/op | JVM range | Branch / JVM |
|---|---:|---:|---:|---:|---:|
| arithmetic-loop | 751,452 | 755,315 | 260,713 | 237,150–305,940 | 2.90x |
| function-calls | 1,994,606 | 1,755,427 | 430,822 | 394,381–489,737 | 4.07x |
| array-index | 1,097,238 | 1,045,719 | 351,528 | 330,125–404,370 | 2.97x |
| array-append | 1,651,493 | 1,652,433 | 244,832 | 217,541–266,584 | 6.75x |
| foreach-array | 2,207,668 | 2,326,811 | 406,975 | 379,431–416,518 | 5.72x |
| string-concat | 254,194 | 301,106 | 97,520 | 90,847–100,354 | 3.09x |
| regex-match | 1,810,376 | 1,499,820 | 401,766 | 368,675–454,051 | 3.73x |

These process-level comparisons were noticeably noisier than the Go benchmark runs. For example, branch foreach medians were 1.98, 6.25, and 2.33 ms; string-concat medians were 0.229, 0.314, and 0.301 ms. The apparent foreach/string regressions and the larger function-call improvement in this table are not corroborated by the five-pass Go measurements. Keep the full results rather than selecting the most favorable run. The defensible conclusion is that the JVM lead remains substantial, with a consistent improvement to regex execution.

| Compilation workload | Main ns/op | Branch ns/op | JVM ns/op | Branch / JVM |
|---|---:|---:|---:|---:|
| arithmetic-loop | 9,547 | 8,827 | 104,038 | 0.08x |
| function-calls | 11,663 | 12,000 | 70,669 | 0.17x |
| array-index | 17,763 | 15,463 | 62,447 | 0.25x |
| array-append | 10,016 | 11,236 | 70,144 | 0.16x |
| foreach-array | 13,327 | 13,538 | 39,460 | 0.34x |
| string-concat | 11,672 | 20,627 | 67,780 | 0.30x |
| regex-match | 14,783 | 15,641 | 80,090 | 0.20x |

OPFOR compilation remains faster in this harness. Compilation was not the optimization target, and its short samples do not justify precise claims about changes between Go variants.

The harness also has fixed runtime/workload order inside each process, a shared JVM across workloads, operation-count warmup without JIT stabilization checks, and only a final-result check. `foreach-array` includes construction through 1,000 pushes, so it does not isolate traversal. Raw retained measurements are in the [sample appendix](interpreter-performance-2026-09-04-samples.md).

## Validation

- `go vet ./...`: passed.
- `go test ./... -count=1 -timeout=300s`: passed.
- `go test -race ./... -count=1 -timeout=420s`: passed.
- `GOTOOLCHAIN=go1.24.0 CGO_ENABLED=0 go test ./... -count=1 -timeout=300s`: passed on the minimum supported Go version.
- Strict official Sleep differential suite with `OPFOR_REQUIRE_SLEEP_JAR=1`, the pinned JAR, OpenJDK 26, and `TZ=UTC`: passed.
- All `BenchmarkSleep` workloads with `-benchtime=1x`: passed.
- Interpreter test cross-compilation for `linux/386` and `wasip1/wasm`: passed. These targets were not executed or benchmarked.
- Added focused numeric edge/serialization/allocation, string provenance/allocation, effectful argument/reference, and dynamic tracing tests; existing alias, sublist, continuation, warning, and tracing tests passed.

An exploratory WASI CLI build is blocked by the existing `github.com/muesli/termenv` terminal dependency. This does not affect the successful WASI interpreter test cross-build and was left outside this change.

## Reproduction

Use an isolated `GOCACHE` if the default cache is unavailable. Build the current comparison binary with `go build -o /tmp/opfor-performance/sleepcompare-candidate ./internal/cmd/sleepcompare`; build the same command from the baseline revision for the control. On this machine the Homebrew Java tools must be specified explicitly because `/usr/bin/javac` cannot locate a registered JDK.

```sh
/tmp/opfor-performance/sleepcompare-candidate \
  -jar /tmp/opfor-performance/sleep-2.1.jar \
  -java /opt/homebrew/opt/openjdk/bin/java \
  -javac /opt/homebrew/opt/openjdk/bin/javac \
  -samples 25 -warmup 200 -execute-iterations 30 -compile-iterations 15
```

For the Go comparison, the baseline binary was built with a Go overlay restoring all six modified production files to the exact baseline revision while retaining the new benchmark file. The branch binary used the working tree. Both ran from `internal/opfor` so fixture paths remained identical:

```sh
mkdir -p /tmp/opfor-performance
go test -c -o /tmp/opfor-performance/candidate.test ./internal/opfor
cd internal/opfor
BINARY=/tmp/opfor-performance/candidate.test
"$BINARY" -test.run='^$' \
  -test.bench='^BenchmarkSleep(ArithmeticLoop|FunctionCalls|NativeCalls|ArrayIndexRead|ArrayAppend|ForeachArray|StringASCII|RegexRepeatedPattern|LiteralLoop|ClosureLiteralLoop|MultiArgumentCalls|BracketClosureCalls)$' \
  -test.benchtime=300ms -test.count=1 -test.timeout=180s
```

Repeat five times for each binary, alternating which binary runs first. Build a control from the baseline revision with the new `call_performance_test.go` copied into it, or use an equivalent Go overlay; the additional tests need not run when measuring benchmarks. Baseline/candidate binaries, profiles, original logs, and overlay files from this session are retained locally under `/tmp/opfor-performance/`; they are not source artifacts.

## Next experiments, in priority order

1. **Compile expression evaluation into a dedicated instruction stream.** The current bytecode covers control flow but stores expressions as AST operands (`internal/bytecode/bytecode.go`). Arithmetic repeatedly traverses `eval`/`evalValue` and scope lookup. An immutable expression plan is the largest structural opportunity; preserve reverse operand evaluation, provider effects, inline yield/callcc boundaries, warning spans, and metering semantics. Benchmark a guarded prototype against the AST fallback before replacing it.
2. **Reduce call-frame and context allocation without pooling escaping state.** Calls still allocate scope cells, argument arrays, fibers, and lifecycle contexts. Closures, references, continuations, and host-retained values constrain reuse. Investigate ownership-specific allocation and immutable context lookup shortcuts while retaining per-call generation admission, cancellation, and drain accounting. Removing lifecycle checks was reviewed and deferred.
3. **Measure hash and traversal costs separately.** Add hash lookup/collision workloads and a traversal-only foreach workload. Array append and builtin call overhead dominate the current combined foreach fixture; scope locks and repeated hash metadata/bucket reconstruction deserve dedicated profiles.
4. **Strengthen JVM measurement before larger performance claims.** Retain individual timing samples in a machine-readable report, add configurable workload/runtime order and independent JVM forks, and use time-based warmup or JIT stabilization evidence. Measure 32-bit execution separately before claiming an overall memory improvement there.
