# OPFOR v0.1.0-alpha.2 vs official Sleep 2.1

This snapshot was collected on 2026-08-26 on an Apple M5 Max (`darwin/arm64`)
with Go 1.27.0 and OpenJDK 26.0.2.1. It uses the pinned official Sleep 2.1 JAR
and the repository's `bench-sleep-compare` target.

```console
make bench-sleep-compare OPFOR_SLEEP_JAR=/path/to/sleep-2.1.jar \
  SLEEP_COMPARE_FLAGS='-samples 25 -warmup 50 -execute-iterations 30 -compile-iterations 15'
```

Each entry is the median nanoseconds per operation from 25 samples after 50
warmup operations. Execution samples contain 30 function calls; compilation
samples contain 15 compilations. Lower values are better. The ratio is OPFOR
divided by Java Sleep, so a ratio below 1 favors OPFOR and a ratio above 1
favors Java Sleep.

## Execution

| Workload | OPFOR ns/op | Java Sleep ns/op | OPFOR / Java |
|---|---:|---:|---:|
| arithmetic-loop | 687695 | 217331 | 3.16x |
| function-calls | 1609600 | 390437 | 4.12x |
| array-index | 932447 | 303476 | 3.07x |
| array-append | 1466848 | 201265 | 7.29x |
| foreach-array | 1904658 | 341968 | 5.57x |
| string-concat | 212672 | 80729 | 2.63x |
| regex-match | 1547938 | 339277 | 4.56x |

## Compilation

| Workload | OPFOR ns/op | Java Sleep ns/op | OPFOR / Java |
|---|---:|---:|---:|
| arithmetic-loop | 14913 | 84005 | 0.18x |
| function-calls | 10702 | 61766 | 0.17x |
| array-index | 13977 | 60461 | 0.23x |
| array-append | 8988 | 65333 | 0.14x |
| foreach-array | 12455 | 33155 | 0.38x |
| string-concat | 9944 | 64738 | 0.15x |
| regex-match | 12108 | 66986 | 0.18x |

In this run OPFOR compiled all seven workloads faster, while the warmed Java
interpreter executed all seven workloads faster. Array append, foreach, and
regular-expression matching retain the largest execution gaps. Plain string
concatenation is 2.63x Java after avoiding eager UTF-16 provenance
materialization, down from 10.32x in the earlier alpha.2 snapshot.

These are local measurements rather than portable performance guarantees. The
runner excludes Java process startup, Java helper compilation, runtime creation,
and script loading from the measured execution intervals. It verifies that both
interpreters return the expected result before reporting a comparison.
