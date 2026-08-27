sub consume_context_shape
{
   local('$handle $closure');
   $handle = openf($2);
   $closure = readObject($handle);
   closef($handle);
   println($1 . "=" . [$closure]);
}

consume_context_shape("try", @ARGV[0]);
consume_context_shape("nested-foreach", @ARGV[1]);
consume_context_shape("inline-foreach", @ARGV[2]);
consume_context_shape("foreach-tail", @ARGV[3]);
consume_context_shape("nested-try", @ARGV[4]);
consume_context_shape("try-foreach", @ARGV[5]);
