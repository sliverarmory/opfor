sub benchmark {
    @items = @(0, 1, 2, 3, 4, 5, 6, 7, 8, 9);
    $sum = 0;
    for ($index = 0; $index < 1000; $index++) {
        $sum = $sum + @items[$index % 10];
    }
    return $sum;
}
