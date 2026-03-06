package reader

import (
	"mld/osm"
	"mld/types"
)

type Reader struct {
	rules    map[int][]types.Tag
	filename string
}

//New to create new reader instance
func New(filename string) *Reader {
	return &Reader{
		filename: filename,
	}
}

// Read to read to memory and process provided pbf file, create spatial index
func (r *Reader) Read() *osm.Data {
	data, err := r.ParsePbf(r.filename)
	if err != nil {
		panic(err)
	}
	return data
}
