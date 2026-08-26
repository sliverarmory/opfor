# Opt-in official Sleep 2.1 producer for phase-three executable graphs.
#
# Arguments:
#   1. writeObject destination for a closure yielded inside an inline block
#   2. writeObject destination for the odd.sl portable static-object closure
#   3. writeObject destination for a yielded recursive writeobj.sl closure

inline phase3_inline
{
   println("inline before");
   yield "first";
   println("inline after");
}

sub phase3_outer
{
   println("outer before");
   phase3_inline();
   println("outer after");
}

$inline = lambda(&phase3_outer);
[$inline];
$inline_handle = openf(">" . @ARGV[0]);
writeObject($inline_handle, $inline);
closef($inline_handle);

$odd = {
   local('$handle');
   $handle = [SleepUtils getIOHandle: $null, [System out]];
   println($handle, "test passed!");
   closef($handle);
};
$odd_handle = openf(">" . @ARGV[1]);
writeObject($odd_handle, $odd);
closef($odd_handle);

$number = 7;
$writeobj = {
   this('$rv $fact');
   $fact = { return iff($1 == 0, 1, $1 * [$this: $1 - 1]); };
   yield;
   $rv = [$fact: $number];
   yield;
   return $rv;
};
[$writeobj];
$writeobj_handle = openf(">" . @ARGV[2]);
writeObject($writeobj_handle, $writeobj);
closef($writeobj_handle);
