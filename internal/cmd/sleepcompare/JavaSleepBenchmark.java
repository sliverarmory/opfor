import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.Hashtable;
import java.util.Stack;

import sleep.engine.Block;
import sleep.runtime.Scalar;
import sleep.runtime.ScriptInstance;
import sleep.runtime.ScriptLoader;

public final class JavaSleepBenchmark {
    private JavaSleepBenchmark() {}

    public static void main(String[] arguments) throws Exception {
        if (arguments.length < 6) {
            throw new IllegalArgumentException("expected workload directory, counts, and workload entries");
        }
        Path workloadDirectory = Path.of(arguments[0]);
        int warmup = Integer.parseInt(arguments[1]);
        int samples = Integer.parseInt(arguments[2]);
        int executeIterations = Integer.parseInt(arguments[3]);
        int compileIterations = Integer.parseInt(arguments[4]);
        for (int index = 5; index < arguments.length; index++) {
            String[] entry = arguments[index].split("=", 2);
            benchmark(entry[0], workloadDirectory.resolve(entry[1]), warmup, samples, executeIterations, compileIterations);
        }
    }

    private static void benchmark(
        String name,
        Path path,
        int warmup,
        int samples,
        int executeIterations,
        int compileIterations
    ) throws Exception {
        String source = Files.readString(path, StandardCharsets.UTF_8);
        ScriptLoader loader = new ScriptLoader();

        for (int index = 0; index < warmup; index++) {
            loader.compileScript(path.getFileName().toString(), source);
        }
        long[] compileSamples = new long[samples];
        for (int sample = 0; sample < samples; sample++) {
            long started = System.nanoTime();
            for (int iteration = 0; iteration < compileIterations; iteration++) {
                loader.compileScript(path.getFileName().toString(), source);
            }
            compileSamples[sample] = (System.nanoTime() - started) / compileIterations;
        }

        Block block = loader.compileScript(path.getFileName().toString(), source);
        ScriptInstance script = loader.loadScript(path.getFileName().toString(), block, new Hashtable());
        script.runScript();
        Scalar result = null;
        for (int index = 0; index < warmup; index++) {
            result = script.callFunction("&benchmark", new Stack());
        }
        long[] executeSamples = new long[samples];
        for (int sample = 0; sample < samples; sample++) {
            long started = System.nanoTime();
            for (int iteration = 0; iteration < executeIterations; iteration++) {
                result = script.callFunction("&benchmark", new Stack());
            }
            executeSamples[sample] = (System.nanoTime() - started) / executeIterations;
        }
        System.out.printf(
            "RESULT\t%s\t%d\t%d\t%s%n",
            name,
            median(compileSamples),
            median(executeSamples),
            result == null ? "null" : result.toString()
        );
    }

    private static long median(long[] values) {
        long[] ordered = values.clone();
        Arrays.sort(ordered);
        int middle = ordered.length / 2;
        if ((ordered.length & 1) == 1) {
            return ordered[middle];
        }
        return ordered[middle - 1] + (ordered[middle] - ordered[middle - 1]) / 2;
    }
}
