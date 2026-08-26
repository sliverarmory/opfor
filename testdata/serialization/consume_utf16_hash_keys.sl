# Official Sleep 2.1 consumer/reproducer for OPFOR's exact UTF-16 hash keys.

$input = openf(@ARGV[0]);
$hash = readObject($input);
@keys = keys($hash);
println(size(@keys));
println(strlen(@keys[0]) . ":" . asc(@keys[0]) . ":" . $hash[@keys[0]]);
println(strlen(@keys[1]) . ":" . asc(@keys[1]) . ":" . asc(charAt(@keys[1], 1)) . ":" . $hash[@keys[1]]);

$output = openf(">" . @ARGV[1]);
writeObject($output, $hash);
closef($output);
