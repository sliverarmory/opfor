sub increment { return $1 + 1; }

sub benchmark {
    $value = 0;
    for ($index = 0; $index < 1000; $index++) {
        $value = increment($value);
    }
    return $value;
}
