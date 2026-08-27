# OPFOR v0.1.0-alpha.2 vs official Sleep 2.1

This snapshot was collected on 2026-08-26 on an Apple M5 Max (`darwin/arm64`)
with Go 1.27.0 and OpenJDK 26.0.2.1. It uses the pinned official Sleep 2.1 JAR
and the repository's `bench-sleep-compare` target.

```console
make bench-sleep-compare OPFOR_SLEEP_JAR=/path/to/sleep-2.1.jar
```

Each entry is the median nanoseconds per operation from 15 samples after 25
warmup operations. Execution samples contain 20 function calls; compilation
samples contain 10 compilations. Lower values are better. The ratio is OPFOR
divided by Java Sleep, so a ratio below 1 favors OPFOR and a ratio above 1
favors Java Sleep.

## Execution

| Workload | OPFOR ns/op | Java Sleep ns/op | OPFOR / Java |
|---|---:|---:|---:|
| arithmetic-loop | 899502 | 234387 | 3.84x |
| function-calls | 1927287 | 429381 | 4.49x |
| array-index | 1172183 | 306016 | 3.83x |
| array-append | 1698804 | 204102 | 8.32x |
| foreach-array | 2190887 | 344960 | 6.35x |
| string-concat | 859135 | 83264 | 10.32x |
| regex-match | 2550654 | 335400 | 7.60x |

## Compilation

| Workload | OPFOR ns/op | Java Sleep ns/op | OPFOR / Java |
|---|---:|---:|---:|
| arithmetic-loop | 20883 | 134541 | 0.16x |
| function-calls | 10537 | 103187 | 0.10x |
| array-index | 15216 | 115129 | 0.13x |
| array-append | 9312 | 74187 | 0.13x |
| foreach-array | 13291 | 58733 | 0.23x |
| string-concat | 11212 | 71995 | 0.16x |
| regex-match | 18033 | 68375 | 0.26x |

In this run OPFOR compiled all seven workloads faster, while the warmed Java
interpreter executed all seven workloads faster. The largest execution gaps
were string concatenation, array append, and regular-expression matching,
which are concrete optimization targets for the next performance pass.

These are local measurements rather than portable performance guarantees. The
runner excludes Java process startup, Java helper compilation, runtime creation,
and script loading from the measured execution intervals. It verifies that both
interpreters return the expected result before reporting a comparison.
