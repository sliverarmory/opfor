# Opt-in official Sleep 2.1 consumer for an OPFOR-produced yielded foreach
# closure. SleepClosure does not serialize the iterator metadata, so invoking
# the restored closure is expected to warn and return the empty scalar.
#
# Arguments:
#   1. writeObject Scalar containing the suspended closure

debug(7 | 34);
global('$handle $foreach');
$handle = openf(@ARGV[0]);
$foreach = readObject($handle);
closef($handle);

println("before");
[$foreach];
println("after");
