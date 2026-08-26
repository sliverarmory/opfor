# Opt-in official Sleep 2.1 consumer for phase-three OPFOR graphs.
#
# Arguments:
#   1. writeObject Scalar containing a yielded local-stack closure
#   2. writeObject Scalar containing a yielded inline closure
#   3. writeObject Scalar containing the odd.sl static-object closure
#   4. writeObject Scalar containing a captured callcc continuation
#   5. writeObject Scalar containing a yielded recursive writeobj.sl closure

$local_handle = openf(@ARGV[0]);
$local = readObject($local_handle);
closef($local_handle);
println([$local]);

$inline_handle = openf(@ARGV[1]);
$inline = readObject($inline_handle);
closef($inline_handle);
[$inline];

$continuation_handle = openf(@ARGV[3]);
$continuation = readObject($continuation_handle);
closef($continuation_handle);
println([$continuation: "tail"]);

$writeobj_handle = openf(@ARGV[4]);
$writeobj = readObject($writeobj_handle);
closef($writeobj_handle);
$number = 7;
[$writeobj];
println([$writeobj]);

$odd_handle = openf(@ARGV[2]);
$odd = readObject($odd_handle);
closef($odd_handle);
[$odd];
