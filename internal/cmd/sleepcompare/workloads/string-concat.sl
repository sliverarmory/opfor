sub benchmark {
    $result = "";
    for ($index = 0; $index < 250; $index++) {
        $result = $result . "opfor";
    }
    return strlen($result);
}
