# Opt-in Go-producer -> official Sleep 2.1 closure consumer harness.
#
# Arguments:
#   1. writeObject Scalar containing an unsuspended closure
#   2. writeAsObject raw unsuspended closure

$handle = openf(@ARGV[0]);
$closure = readObject($handle);
closef($handle);

println([$closure: "tail"]);
println($closure['$captured']);
$self = $closure['$this'];
println([$self: "again"]);

$raw = openf(@ARGV[1]);
$raw_closure = readAsObject($raw);
closef($raw);
println([$raw_closure: "raw"]);
