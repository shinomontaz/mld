package graph

import "mld/osm"

type NodeID int64

type Node struct {
	ID       NodeID
	Lat, Lon float64
	OSMId    int64
}

type Direction byte

const (
	DirectionNone     Direction = 0
	DirectionForward            = 1
	DirectionBackward           = 2
	DirectionBoth     Direction = 3
)

type Edge struct {
	From, To NodeID
	Distance float64
	Duration float64
	Weight   float64 // Duration со штрафами

	Direction Direction

	OSMId int64
	Tags  map[string]string
}

type Graph struct {
	Nodes []Node
	Edges []Edge
	Adj   []int
}

func Build(osmData *osm.Data) *Graph {
	intersections := getIntersections(osmData)
	nodes := createNodes(osmData, intersections)
	edges := createEdges(osmData, intersections)

	return &Graph{
		Nodes: nodes,
		Edges: edges,
	}
}

func getIntersections(osmData *osm.Data) map[int64]bool {
	// подсчитать usage
	nodeUsage := make(map[int64]int)

	for _, way := range osmData.Ways {
		for _, node := range way.Nodes {
			nodeUsage[node.Id]++
		}
	}

	// перекрёстки = узлы с usage >= 2 ИЛИ концы ways ( как в OSRM )
	intersections := make(map[int64]bool)

	for _, way := range osmData.Ways {
		if len(way.Nodes) == 0 {
			continue
		}

		// Концы ways
		intersections[way.Nodes[0].Id] = true
		intersections[way.Nodes[len(way.Nodes)-1].Id] = true

		// Промежуточные узлы - если встречаются в нескольких ways
		for i := 1; i < len(way.Nodes)-1; i++ {
			if nodeUsage[way.Nodes[i].Id] >= 2 {
				intersections[way.Nodes[i].Id] = true
			}
		}
	}

	return intersections
}

func createNodes(osmData *osm.Data, intersections map[int64]bool) []Node {
	nodes := make([]Node, 0)

	for _, n := range osmData.Nodes {
		if !intersections[n.Id] {
			continue
		}

		nodes = append(nodes, Node{
			ID:    NodeID(n.Id),
			Lat:   n.Lat,
			Lon:   n.Lon,
			OSMId: n.Id,
		})
	}

	return nodes
}

func createEdges(osmData *osm.Data, intersections map[int64]bool) []Edge {
	edges := make([]Edge, 0)

	// TODO: реализовать построение рёбер на основе ways
	// Для каждого way создать ребра между consecutive nodes
	// Учесть direction (односторонка/двусторонняя)
	wints := []int{}
	for _, w := range osmData.Ways {
		// 	Для каждого way:
		//  1. Найти все перекрёстки в way.Nodes
		//  2. Между каждой парой consecutive перекрёстков создать ребро
		//  3. Вычислить Distance (сумма Haversine между промежуточными узлами)
		//  4. Определить Direction из way.Tags (oneway)
		wints = wints[:0]
		for i, nId := range w.NodeIDs {
			if intersections[nId] {
				wints = append(wints, i)
			}
		}

		for i := 0; i < len(wints)-1; i++ {
			edges = append(edges, Edge{
				From:      NodeID(w.Nodes[wints[i]].Id),
				To:        NodeID(w.Nodes[wints[i+1]].Id),
				OSMId:     w.Id,
				Tags:      w.Tags,
				Direction: getDirection(w.Tags),
				// TODO: calculate distance
				// можно использовать от w.Nodes[wints[i]] до w.Nodes[wints[i+1]]
			})

		}
	}

	return edges
}

func getDirection(tags map[string]string) Direction {
	oneway, ok := tags["oneway"]
	if !ok {
		if highway, ok := tags["highway"]; ok {
			if highway == "motorway" || highway == "motorway_link" {
				return DirectionForward
			}
		}
		return DirectionBoth
	}

	switch oneway {
	case "yes": // true?
		return DirectionForward
	case "reverse":
		return DirectionBackward
	case "no": // 0, -1, false?
		return DirectionBoth
	default:
		return DirectionBoth
	}
}
