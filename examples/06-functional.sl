# Transform and combine arrays with higher-order stock functions.

@numbers = @(1, 2, 3, 4, 5, 6);

@squares = map({
    return $1 * $1;
}, @numbers);

@evens = filter({
    return iff(($1 % 2) == 0, $1, $null);
}, @numbers);

$total = reduce({
    return $1 + $2;
}, @numbers);

println("numbers: " . @numbers);
println("squares: " . @squares);
println("evens:   " . @evens);
println("sum:     " . $total);
