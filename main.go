package main

import (
	"fmt"
	"mld/reader"
)

func main() {
	r := reader.New("./monaco.osm.pbf")
	r.Read()

	// 1. читаем

	fmt.Println("mld osm.pbf reader and edge ajacency map building")
}
