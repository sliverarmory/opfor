# Build closures that retain independent state between calls.

sub make_accumulator
{
    return lambda({
        $total = $total + $1;
        return $total;
    }, $total => $1);
}

$first = make_accumulator(10);
$second = make_accumulator(100);

println("first + 5 = " . [$first: 5]);
println("first + 3 = " . [$first: 3]);
println("second + 7 = " . [$second: 7]);
