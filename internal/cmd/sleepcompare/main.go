// Command sleepcompare measures equivalent Sleep workloads in OPFOR and the
// official Sleep 2.1 Java interpreter.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sliverarmory/opfor"
)

const officialSleepSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"

//go:embed JavaSleepBenchmark.java workloads/*.sl
var benchmarkAssets embed.FS

type configuration struct {
	jar               string
	java              string
	javac             string
	warmup            int
	samples           int
	executeIterations int
	compileIterations int
}

type workload struct {
	name     string
	file     string
	expected string
	source   string
}

type measurement struct {
	compileNS int64
	executeNS int64
	result    string
}

type comparison struct {
	workload workload
	opfor    measurement
	java     measurement
}

var benchmarkWorkloads = []workload{
	{name: "arithmetic-loop", file: "arithmetic-loop.sl", expected: "499500"},
	{name: "function-calls", file: "function-calls.sl", expected: "1000"},
	{name: "array-index", file: "array-index.sl", expected: "4500"},
	{name: "array-append", file: "array-append.sl", expected: "1000"},
	{name: "foreach-array", file: "foreach-array.sl", expected: "499500"},
	{name: "string-concat", file: "string-concat.sl", expected: "1250"},
	{name: "regex-match", file: "regex-match.sl", expected: "1000"},
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sleepcompare:", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	config, err := parseConfiguration(arguments)
	if err != nil {
		return err
	}
	if err := verifyOfficialSleepJAR(config.jar); err != nil {
		return err
	}
	javaPath, err := resolveTool(config.java, "java")
	if err != nil {
		return err
	}
	javacPath, err := resolveTool(config.javac, "javac")
	if err != nil {
		return err
	}
	workloads, err := loadWorkloads()
	if err != nil {
		return err
	}

	opforMeasurements := make(map[string]measurement, len(workloads))
	for _, item := range workloads {
		result, benchmarkErr := benchmarkOPFOR(item, config)
		if benchmarkErr != nil {
			return benchmarkErr
		}
		opforMeasurements[item.name] = result
	}
	javaMeasurements, javaVersion, err := benchmarkJava(workloads, config, javaPath, javacPath)
	if err != nil {
		return err
	}

	comparisons := make([]comparison, 0, len(workloads))
	for _, item := range workloads {
		opforResult := opforMeasurements[item.name]
		javaResult, ok := javaMeasurements[item.name]
		if !ok {
			return fmt.Errorf("Java benchmark did not report %q", item.name)
		}
		if opforResult.result != item.expected {
			return fmt.Errorf("OPFOR %s result %q, want %q", item.name, opforResult.result, item.expected)
		}
		if javaResult.result != item.expected {
			return fmt.Errorf("Java Sleep %s result %q, want %q", item.name, javaResult.result, item.expected)
		}
		comparisons = append(comparisons, comparison{workload: item, opfor: opforResult, java: javaResult})
	}
	writeReport(output, config, javaVersion, comparisons)
	return nil
}

func parseConfiguration(arguments []string) (configuration, error) {
	flags := flag.NewFlagSet("sleepcompare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := configuration{}
	flags.StringVar(&config.jar, "jar", os.Getenv("OPFOR_SLEEP_JAR"), "path to the official Sleep 2.1 JAR")
	flags.StringVar(&config.java, "java", os.Getenv("OPFOR_JAVA"), "Java executable (default: PATH)")
	flags.StringVar(&config.javac, "javac", os.Getenv("OPFOR_JAVAC"), "javac executable (default: PATH)")
	flags.IntVar(&config.warmup, "warmup", 25, "untimed execution and compilation warmup operations")
	flags.IntVar(&config.samples, "samples", 15, "timing samples per workload and phase")
	flags.IntVar(&config.executeIterations, "execute-iterations", 20, "function calls per execution sample")
	flags.IntVar(&config.compileIterations, "compile-iterations", 10, "compilations per compilation sample")
	if err := flags.Parse(arguments); err != nil {
		return configuration{}, err
	}
	if flags.NArg() != 0 {
		return configuration{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if config.jar == "" {
		return configuration{}, errors.New("-jar or OPFOR_SLEEP_JAR is required")
	}
	if config.warmup < 0 || config.samples < 1 || config.executeIterations < 1 || config.compileIterations < 1 {
		return configuration{}, errors.New("warmup must be non-negative and sample/iteration counts must be positive")
	}
	return config, nil
}

func resolveTool(configured, fallback string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	path, err := exec.LookPath(fallback)
	if err != nil {
		return "", fmt.Errorf("locate %s: %w", fallback, err)
	}
	return path, nil
}

func verifyOfficialSleepJAR(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read official Sleep JAR: %w", err)
	}
	digest := sha256.Sum256(contents)
	got := hex.EncodeToString(digest[:])
	if got != officialSleepSHA256 {
		return fmt.Errorf("official Sleep 2.1 JAR SHA-256 = %s, want %s", got, officialSleepSHA256)
	}
	return nil
}

func loadWorkloads() ([]workload, error) {
	result := make([]workload, len(benchmarkWorkloads))
	copy(result, benchmarkWorkloads)
	for index := range result {
		contents, err := benchmarkAssets.ReadFile(filepath.ToSlash(filepath.Join("workloads", result[index].file)))
		if err != nil {
			return nil, fmt.Errorf("read workload %s: %w", result[index].name, err)
		}
		result[index].source = string(contents)
	}
	return result, nil
}

func benchmarkOPFOR(item workload, config configuration) (measurement, error) {
	for index := 0; index < config.warmup; index++ {
		if _, err := opfor.CompileString(item.file, item.source); err != nil {
			return measurement{}, fmt.Errorf("warm up OPFOR compiler for %s: %w", item.name, err)
		}
	}
	compileSamples := make([]int64, config.samples)
	for sample := range compileSamples {
		started := time.Now()
		for iteration := 0; iteration < config.compileIterations; iteration++ {
			if _, err := opfor.CompileString(item.file, item.source); err != nil {
				return measurement{}, fmt.Errorf("compile %s with OPFOR: %w", item.name, err)
			}
		}
		compileSamples[sample] = time.Since(started).Nanoseconds() / int64(config.compileIterations)
	}

	program, err := opfor.CompileString(item.file, item.source)
	if err != nil {
		return measurement{}, fmt.Errorf("compile executable %s with OPFOR: %w", item.name, err)
	}
	runtimeInstance, err := opfor.New(opfor.WithStdout(io.Discard), opfor.WithStderr(io.Discard))
	if err != nil {
		return measurement{}, fmt.Errorf("create OPFOR runtime for %s: %w", item.name, err)
	}
	defer runtimeInstance.Close(context.Background())
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		return measurement{}, fmt.Errorf("load %s with OPFOR: %w", item.name, err)
	}
	ctx := context.Background()
	var value opfor.Value
	for index := 0; index < config.warmup; index++ {
		value, err = script.Call(ctx, "benchmark")
		if err != nil {
			return measurement{}, fmt.Errorf("warm up OPFOR execution for %s: %w", item.name, err)
		}
	}
	executeSamples := make([]int64, config.samples)
	for sample := range executeSamples {
		started := time.Now()
		for iteration := 0; iteration < config.executeIterations; iteration++ {
			value, err = script.Call(ctx, "benchmark")
			if err != nil {
				return measurement{}, fmt.Errorf("execute %s with OPFOR: %w", item.name, err)
			}
		}
		executeSamples[sample] = time.Since(started).Nanoseconds() / int64(config.executeIterations)
	}
	return measurement{compileNS: median(compileSamples), executeNS: median(executeSamples), result: value.String()}, nil
}

func benchmarkJava(workloads []workload, config configuration, javaPath, javacPath string) (map[string]measurement, string, error) {
	temporary, err := os.MkdirTemp("", "opfor-sleepcompare-")
	if err != nil {
		return nil, "", fmt.Errorf("create Java benchmark directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	classes := filepath.Join(temporary, "classes")
	workloadDirectory := filepath.Join(temporary, "workloads")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(workloadDirectory, 0o755); err != nil {
		return nil, "", err
	}
	helper, err := benchmarkAssets.ReadFile("JavaSleepBenchmark.java")
	if err != nil {
		return nil, "", err
	}
	helperPath := filepath.Join(temporary, "JavaSleepBenchmark.java")
	if err := os.WriteFile(helperPath, helper, 0o644); err != nil {
		return nil, "", err
	}
	for _, item := range workloads {
		if err := os.WriteFile(filepath.Join(workloadDirectory, item.file), []byte(item.source), 0o644); err != nil {
			return nil, "", err
		}
	}

	compileCommand := exec.Command(javacPath, "-encoding", "UTF-8", "-cp", config.jar, "-d", classes, helperPath)
	if output, err := compileCommand.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("compile Java benchmark helper: %w\n%s", err, output)
	}
	classPath := strings.Join([]string{classes, config.jar}, string(os.PathListSeparator))
	arguments := []string{
		"-Dfile.encoding=UTF-8",
		"-cp", classPath,
		"JavaSleepBenchmark",
		workloadDirectory,
		strconv.Itoa(config.warmup),
		strconv.Itoa(config.samples),
		strconv.Itoa(config.executeIterations),
		strconv.Itoa(config.compileIterations),
	}
	for _, item := range workloads {
		arguments = append(arguments, item.name+"="+item.file)
	}
	command := exec.Command(javaPath, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("run Java Sleep benchmarks: %w\n%s", err, output)
	}
	measurements, err := parseJavaMeasurements(string(output))
	if err != nil {
		return nil, "", err
	}
	versionOutput, versionErr := exec.Command(javaPath, "-version").CombinedOutput()
	javaVersion := firstLine(string(versionOutput))
	if versionErr != nil && javaVersion == "" {
		javaVersion = "unknown"
	}
	return measurements, javaVersion, nil
}

func parseJavaMeasurements(output string) (map[string]measurement, error) {
	measurements := make(map[string]measurement)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "RESULT\t") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return nil, fmt.Errorf("malformed Java benchmark result %q", line)
		}
		compileNS, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse Java compile measurement for %s: %w", fields[1], err)
		}
		executeNS, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse Java execution measurement for %s: %w", fields[1], err)
		}
		if _, exists := measurements[fields[1]]; exists {
			return nil, fmt.Errorf("duplicate Java benchmark result for %q", fields[1])
		}
		measurements[fields[1]] = measurement{compileNS: compileNS, executeNS: executeNS, result: fields[4]}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(measurements) == 0 {
		return nil, fmt.Errorf("Java benchmark emitted no results:\n%s", output)
	}
	return measurements, nil
}

func median(values []int64) int64 {
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[middle]
	}
	return ordered[middle-1] + (ordered[middle]-ordered[middle-1])/2
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	return strings.TrimSpace(line)
}

func writeReport(output io.Writer, config configuration, javaVersion string, comparisons []comparison) {
	fmt.Fprintln(output, "# OPFOR vs official Sleep 2.1")
	fmt.Fprintln(output)
	fmt.Fprintf(output, "Median nanoseconds per operation; lower is better. Ratio is OPFOR / Java Sleep.\n\n")
	fmt.Fprintf(output, "- Platform: `%s/%s`\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(output, "- Go: `%s`\n", runtime.Version())
	fmt.Fprintf(output, "- Java: `%s`\n", javaVersion)
	fmt.Fprintf(output, "- Samples: `%d`; warmup operations: `%d`\n", config.samples, config.warmup)
	fmt.Fprintf(output, "- Execution calls/sample: `%d`; compilations/sample: `%d`\n\n", config.executeIterations, config.compileIterations)
	writeMeasurementTable(output, "Execution", comparisons, func(value measurement) int64 { return value.executeNS })
	fmt.Fprintln(output)
	writeMeasurementTable(output, "Compilation", comparisons, func(value measurement) int64 { return value.compileNS })
}

func writeMeasurementTable(output io.Writer, title string, comparisons []comparison, selectMeasurement func(measurement) int64) {
	fmt.Fprintf(output, "## %s\n\n", title)
	fmt.Fprintln(output, "| Workload | OPFOR ns/op | Java Sleep ns/op | OPFOR / Java |")
	fmt.Fprintln(output, "|---|---:|---:|---:|")
	for _, item := range comparisons {
		opforNS := selectMeasurement(item.opfor)
		javaNS := selectMeasurement(item.java)
		fmt.Fprintf(output, "| %s | %d | %d | %.2fx |\n", item.workload.name, opforNS, javaNS, ratio(opforNS, javaNS))
	}
}

func ratio(opforNS, javaNS int64) float64 {
	if javaNS == 0 {
		return 0
	}
	return float64(opforNS) / float64(javaNS)
}
