# Generate later-phase SleepClosure interoperability fixtures with the same
# official Sleep 2.1 JAR documented in official-sleep-2.1/README.md. Run from
# testdata/serialization after generate_phase1.sl, passing this file on stdin
# so serialized Block.source values do not contain a machine-specific path:
#
#   java -jar /path/to/sleep.jar - < generate_closure_phases.sl

sub write_closure
{
   local('$handle');
   $handle = openf(">official-sleep-2.1/ $+ $1");
   writeObject($handle, $2);
   closef($handle);
}

$plain = lambda({ return "$captured:$1"; }, $captured => "seven");
write_closure("closure-unsuspended.ser", $plain);

$yielded = lambda({
   local('$state');
   $state = "retained";
   yield "first";
   return "$state:$1";
});
[$yielded];
write_closure("closure-yielded.ser", $yielded);

$locals = lambda({
   local('$value');
   $value = "outer";
   pushl();
   local('$value');
   $value = "inner";
   yield;
   popl();
   return $value;
});
[$locals];
write_closure("closure-local-stack.ser", $locals);

$foreach = lambda({
   local('$item');
   foreach $item (@("a", "b", "c"))
   {
      yield $item;
   }
});
[$foreach];
write_closure("closure-foreach.ser", $foreach);

global('$continuation_handle');
$continuation_handle = openf(">official-sleep-2.1/closure-callcc.ser");

inline capture_continuation
{
   callcc {
      writeObject($continuation_handle, $1);
   };
}

sub make_continuation
{
   local('$retained');
   $retained = "callcc state";
   capture_continuation();
   return $retained;
}

make_continuation();
closef($continuation_handle);

$printing = { println("Hello World!"); };
write_closure("closure-print.ser", $printing);
