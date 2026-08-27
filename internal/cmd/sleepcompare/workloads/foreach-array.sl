sub benchmark {
    @items = @();
    for ($index = 0; $index < 1000; $index++) {
        push(@items, $index);
    }
    $sum = 0;
    foreach $item (@items) {
        $sum = $sum + $item;
    }
    return $sum;
}
