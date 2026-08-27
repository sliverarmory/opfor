sub benchmark {
    $matches = 0;
    for ($index = 0; $index < 1000; $index++) {
        if ("operator42@example.com" ismatch '[a-z]+[0-9]+\@[a-z]+\.[a-z]+') {
            $matches++;
        }
    }
    return $matches;
}
