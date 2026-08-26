# Define and call a recursive subroutine.

sub factorial
{
    if ($1 <= 1)
    {
        return 1;
    }

    return $1 * factorial($1 - 1);
}

for ($number = 1; $number <= 6; $number++)
{
    println($number . "! = " . factorial($number));
}
