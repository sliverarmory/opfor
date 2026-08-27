sub benchmark {
    $sum = 0;
    for ($index = 0; $index < 1000; $index++) {
        $sum = $sum + $index;
    }
    return $sum;
}
