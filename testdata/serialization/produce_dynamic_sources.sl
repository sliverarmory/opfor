# Opt-in official Sleep 2.1 / OPFOR producer for dynamic-source continuation
# groups. Arguments are writeObject destinations in this order:
# eval, two eval contexts, eval plus outer yield, eval with inline yield,
# expr with inline yield, include, and eval with foreach yield.

sub save_dynamic
{
   local('$handle');
   $handle = openf(">" . $1);
   writeObject($handle, $2);
   closef($handle);
}

$eval = {
   $value = eval('yield "eval-first"; return "eval-tail";');
   return "eval-outer:" . $value;
};
[$eval];
save_dynamic(@ARGV[0], $eval);

$two = {
   eval('yield "a1"; println("A2");');
   eval('yield "b1"; println("B2");');
   return "two-outer";
};
[$two];
save_dynamic(@ARGV[1], $two);

$outer = {
   eval('yield "inner-first"; println("INNER-TAIL");');
   yield "outer-first";
   return "outer-tail";
};
[$outer];
save_dynamic(@ARGV[2], $outer);

inline dynamic_inline
{
   yield "inline-first";
   return "inline-tail";
}

$eval_inline = {
   eval('dynamic_inline(); return "dynamic-tail";');
   return "eval-inline-outer";
};
[$eval_inline];
save_dynamic(@ARGV[3], $eval_inline);

$expr_inline = {
   $value = expr('dynamic_inline()');
   return "expr-inline-outer:" . $value;
};
[$expr_inline];
save_dynamic(@ARGV[4], $expr_inline);

$included = {
   include("testdata/serialization/dynamic_include_target.sl");
   return "include-outer";
};
[$included];
save_dynamic(@ARGV[5], $included);

$dynamic_foreach = {
   eval('foreach $item (@("a", "b")) { yield $item; }');
   return "foreach-outer";
};
[$dynamic_foreach];
save_dynamic(@ARGV[6], $dynamic_foreach);
