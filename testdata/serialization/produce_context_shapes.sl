# Opt-in official Sleep 2.1 / OPFOR producer for suspended context shapes.
# Arguments are writeObject destinations in this order: saved try/catch,
# nested foreach, foreach with an inline body, a resumable foreach body,
# nested saved handlers, and a foreach body beneath a saved handler.

sub save_context_shape
{
   local('$handle');
   $handle = openf(">" . $1);
   writeObject($handle, $2);
   closef($handle);
}

$saved_try = {
   try
   {
      yield "try-first";
      throw "boom";
   }
   catch $error
   {
      return "caught:" . $error;
   }
   return "missed-catch";
};
[$saved_try];
save_context_shape(@ARGV[0], $saved_try);

$nested_foreach = {
   foreach $outer (@("a", "b"))
   {
      foreach $inner (@("1", "2"))
      {
         yield $outer . $inner;
      }
   }
   return "nested-done";
};
[$nested_foreach];
save_context_shape(@ARGV[1], $nested_foreach);

inline yield_from_foreach
{
   yield $1 . "-inline";
   return $1 . "-inline-tail";
}

$inline_foreach = {
   foreach $item (@("a", "b"))
   {
      yield_from_foreach($item);
      return "inline-body-tail:" . $item;
   }
   return "inline-loop-done";
};
[$inline_foreach];
save_context_shape(@ARGV[2], $inline_foreach);

$foreach_body_tail = {
   foreach $item (@("a", "b"))
   {
      yield $item;
      return "body-tail:" . $item;
   }
   return "loop-done";
};
[$foreach_body_tail];
save_context_shape(@ARGV[3], $foreach_body_tail);

$nested_try = {
   try
   {
      try
      {
         yield "nested-first";
         throw "inner";
      }
      catch $inner
      {
         throw "outer:" . $inner;
      }
   }
   catch $outer
   {
      return "nested-caught:" . $outer;
   }
   return "nested-missed";
};
[$nested_try];
save_context_shape(@ARGV[4], $nested_try);

$try_foreach = {
   try
   {
      foreach $item (@("a", "b"))
      {
         yield $item;
         throw "item:" . $item;
      }
   }
   catch $error
   {
      return "foreach-caught:" . $error;
   }
   return "foreach-missed";
};
[$try_foreach];
save_context_shape(@ARGV[5], $try_foreach);
