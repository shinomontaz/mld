package types

const (
	ItemTypeNode = iota
	ItemTypeWay
	ItemTypeRelation
)

type Tag struct {
	Key, Val string
}
