package geom

type Node struct {
	Lon_, Lat_ float64
	Id         int64
}

func (n *Node) Lon() float64 { return n.Lon_ }
func (n *Node) Lat() float64 { return n.Lat_ }
