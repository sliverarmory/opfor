# Opt-in Go-producer -> official Sleep 2.1 consumer harness.
#
# Arguments:
#   1. concatenated writeObject Scalar streams
#   2. concatenated writeAsObject raw Java streams

$handle = openf(@ARGV[0]);
println(readObject($handle));
println(readObject($handle));
println(readObject($handle));
%hash = readObject($handle);
println(%hash["a"] . "," . %hash["b"] . "," . %hash["c"]);
println(readObject($handle));

$raw = openf(@ARGV[1]);
$value = readAsObject($raw); println([$value getClass] . ":" . $value);
$value = readAsObject($raw); println([$value getClass] . ":" . $value);
$value = readAsObject($raw); println([$value getClass] . ":" . $value);
$value = readAsObject($raw); println([$value getClass] . ":" . $value);
$value = readAsObject($raw); println([$value getClass] . ":" . $value);
