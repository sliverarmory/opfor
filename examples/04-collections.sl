# Work with arrays and hashes using stock collection functions.

@languages = @("Sleep", "Go", "Python");
push(@languages, "Rust");

println("Languages:");
foreach $index => $language (@languages)
{
    println("  " . $index . ": " . $language);
}

%scores["Ada"] = 10;
%scores["Grace"] = 8;
%scores["Linus"] = 9;

@names = keys(%scores);
sorta(@names);

println("Scores:");
foreach $name (@names)
{
    println("  " . $name . ": " . %scores[$name]);
}
