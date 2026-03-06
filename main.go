package main

import (
	"fmt"
	"mld/osm"
)

func main() {
	data, err := osm.Read("./monaco.osm.pbf")
	if err != nil {
		panic(err)
	}

	fmt.Println("Nodes:", len(data.Nodes))
	fmt.Println("Ways:", len(data.Ways))

	// 1. читаем

	fmt.Println("mld osm.pbf reader and edge ajacency map building")
}
