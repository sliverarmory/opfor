# Opt-in official Sleep 2.1 consumer for the seven streams produced by
# produce_dynamic_sources.sl.

sub load_dynamic
{
   local('$handle $closure');
   $handle = openf($1);
   $closure = readObject($handle);
   closef($handle);
   return $closure;
}

$eval = load_dynamic(@ARGV[0]);
$result = [$eval];
println("eval=" . $result);

$two = load_dynamic(@ARGV[1]);
$result = [$two];
println("two=" . $result);

$outer = load_dynamic(@ARGV[2]);
$result = [$outer];
println("outer=" . $result);

$eval_inline = load_dynamic(@ARGV[3]);
$result = [$eval_inline];
println("eval-inline=" . $result);

$expr_inline = load_dynamic(@ARGV[4]);
$result = [$expr_inline];
println("expr-inline=" . $result);

$included = load_dynamic(@ARGV[5]);
$result = [$included];
println("include=" . $result);

$dynamic_foreach = load_dynamic(@ARGV[6]);
$result = [$dynamic_foreach];
println("foreach=" . $result);
