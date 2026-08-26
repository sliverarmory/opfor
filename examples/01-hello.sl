# Read command-line arguments from @ARGV.

$name = "world";

if (size(@ARGV) > 0)
{
    $name = @ARGV[0];
}

println("Hello, " . $name . "!");
