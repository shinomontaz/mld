package osm

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"github.com/qedus/osmpbf"
)

type Data struct {
	Nodes map[int64]Node
	Ways  map[int64]Way
}

func NodeFromPbf(n *osmpbf.Node) Node {
	return Node{
		Lon: n.Lon,
		Lat: n.Lat,
		Id:  n.ID,
	}
}

func WayFromPbf(w *osmpbf.Way) Way {
	return Way{
		NodeIDs: w.NodeIDs,
		Id:      w.ID,
		Tags:    w.Tags,
		Nodes:   make([]Node, 0, 2),
	}
}

func Read(path string) (*Data, error) {
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

	data := &Data{
		Nodes: map[int64]Node{},
		Ways:  map[int64]Way{},
	}

	allNodes := make(map[int64]Node)
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
