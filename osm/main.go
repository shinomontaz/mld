package osm

import (
	"mld/geom"
)

type Data struct {
	Nodes map[int64]geom.Node
	Ways  map[int64]geom.Way
}
