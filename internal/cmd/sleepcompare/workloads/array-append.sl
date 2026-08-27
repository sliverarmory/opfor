sub benchmark {
    @items = @();
    for ($index = 0; $index < 1000; $index++) {
        push(@items, $index);
    }
    return size(@items);
}
