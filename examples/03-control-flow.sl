# FizzBuzz exercises loops, arithmetic, and branching.

for ($number = 1; $number <= 20; $number++)
{
    $label = "";

    if (($number % 3) == 0)
    {
        $label = $label . "Fizz";
    }

    if (($number % 5) == 0)
    {
        $label = $label . "Buzz";
    }

    if ($label eq "")
    {
        $label = $number;
    }

    println($label);
}
