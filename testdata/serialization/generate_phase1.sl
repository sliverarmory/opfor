# Generate Java Object Serialization interoperability fixtures with the
# official Sleep 2.1 JAR. Run this script from testdata/serialization:
#
#   /opt/homebrew/opt/openjdk/bin/java -jar /tmp/opfor-sleep21.jar \
#     generate_phase1.sl
#
# Each writeObject/writeAsObject call creates one independent stream. Keep the
# roots in separate files so a failing schema is easy to identify.

sub write_sleep_scalar
{
   local('$handle');
   $handle = openf(">official-sleep-2.1/ $+ $1");
   writeObject($handle, $2);
   closef($handle);
}

sub write_java_object
{
   local('$handle');
   $handle = openf(">official-sleep-2.1/ $+ $1");
   writeAsObject($handle, $2);
   closef($handle);
}

write_sleep_scalar("scalar-null.ser", $null);
write_sleep_scalar("scalar-string.ser", "hello, Sleep");
write_sleep_scalar("scalar-int.ser", 42);
write_sleep_scalar("scalar-long.ser", 4294967296L);
write_sleep_scalar("scalar-double.ser", 3.25);
write_sleep_scalar("scalar-binary-string.ser", pack("B4", 0, 127, 128, 255));

@inner = @("x", 7);
@outer = @(@inner, @inner, @("nested", $null));
write_sleep_scalar("array-shared.ser", @outer);

@cycle = @("head");
push(@cycle, @cycle);
write_sleep_scalar("array-cycle.ser", @cycle);

%hash = %(a => "apple", b => 2, c => 3.5);
write_sleep_scalar("hash.ser", %hash);

%ordered = ohash();
%ordered["third"] = 3;
%ordered["first"] = 1;
%ordered["second"] = 2;
write_sleep_scalar("ordered-hash.ser", %ordered);

$handle = openf(">official-sleep-2.1/concatenated-scalars.ser");
writeObject($handle, "first", 2, @("third"));
closef($handle);

write_java_object("raw-string.ser", "raw string");
write_java_object("raw-binary-string.ser", pack("B4", 0, 127, 128, 255));
write_java_object("raw-int.ser", 17);
write_java_object("raw-long.ser", 4294967296L);
write_java_object("raw-double.ser", 6.5);
write_java_object("raw-boolean.ser", casti(1, "z"));
write_java_object("raw-class-string.ser", ^String);
