package geom

type Way struct {
	NodeIDs []int64
	Id      int64
	Tags    map[string]string
	Nodes   []Node
}

func (w *Way) IsOk() bool {
	hgw, ok := w.Tags["highway"]
	if !ok {
		return false
	}

	bad := (w.Tags["access"] == "no") ||
		(w.Tags["motor_vehicle"] == "no") ||
		(hgw == "footway") ||
		(hgw == "path") ||
		(hgw == "steps") ||
		(hgw == "cycleway") ||
		(hgw == "pedestrian") || // Пешеходные зоны
		(hgw == "track") || // Грунтовые дороги
		(hgw == "bridleway") || // Конные дорожки
		(hgw == "construction") || // Строящиеся дороги
		(hgw == "proposed") // Планируемые дороги

	return !bad
}
