# Opt-in official Sleep 2.1 producer for a yielded foreach closure.
#
# Arguments:
#   1. writeObject destination for the suspended closure

$foreach = {
   local('$item');
   foreach $item (@("a", "b", "c"))
   {
      yield $item;
   }
};

[$foreach];
$handle = openf(">" . @ARGV[0]);
writeObject($handle, $foreach);
closef($handle);
