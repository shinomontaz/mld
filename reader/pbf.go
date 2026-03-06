package reader

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"mld/geom"
	"mld/osm"

	"github.com/qedus/osmpbf"
)

func NodeFromPbf(n *osmpbf.Node) geom.Node {
	return geom.Node{
		Lon_: n.Lon,
		Lat_: n.Lat,
		Id:   n.ID,
	}
}

func WayFromPbf(w *osmpbf.Way) geom.Way {
	return geom.Way{
		NodeIDs: w.NodeIDs,
		Id:      w.ID,
		Tags:    w.Tags,
		Nodes:   make([]geom.Node, 0, 2),
	}
}

func (r *Reader) ParsePbf(path string) (*osm.Data, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := osmpbf.NewDecoder(f)
	err = d.Start(runtime.GOMAXPROCS(0) - 1)
	if err != nil {
		return nil, err
	}

	data := &osm.Data{
		Nodes: map[int64]geom.Node{},
		Ways:  map[int64]geom.Way{},
	}

	allNodes := make(map[int64]geom.Node)

	// w.Nodes = make([]Node, 0)
	// for _, id := range w.NodeIDs {
	// 	if _, ok := nodes[id]; !ok {
	// 		fmt.Println(id)
	// 		panic("cannot find node!")
	// 	}
	// 	w.Nodes = append(w.Nodes, nodes[id])
	// }

	for {
		if v, err := d.Decode(); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		} else {
			switch v := v.(type) {
			case *osmpbf.Node:
				node := NodeFromPbf(v)
				allNodes[node.Id] = node
			case *osmpbf.Way:
				way := WayFromPbf(v)
				if !way.IsOk() {
					continue
				}

				for _, id := range way.NodeIDs {
					n, ok := allNodes[id]
					if !ok {
						fmt.Println(id)
						panic("cannot find node!")
					}
					way.Nodes = append(way.Nodes, n)
					data.Nodes[id] = n
				}
				data.Ways[way.Id] = way
			}
		}
	}

	log.Println("Num ways", len(data.Ways))
	log.Println("Num nodes", len(data.Nodes))
	return data, nil
}
