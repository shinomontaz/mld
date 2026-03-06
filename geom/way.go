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

	// см. OSRM restricted_highway_whitelist
	allowedHighways := map[string]bool{
		"motorway":       true,
		"motorway_link":  true,
		"trunk":          true,
		"trunk_link":     true,
		"primary":        true,
		"primary_link":   true,
		"secondary":      true,
		"secondary_link": true,
		"tertiary":       true,
		"tertiary_link":  true,
		"residential":    true,
		"living_street":  true,
		"unclassified":   true,
		"service":        true,
	}

	if !allowedHighways[hgw] {
		return false
	}

	// OSRM avoid list ( профиль car )
	if w.Tags["area"] == "yes" {
		return false
	}
	if w.Tags["reversible"] == "yes" {
		return false
	}
	if w.Tags["impassable"] == "yes" {
		return false
	}
	if w.Tags["status"] == "impassable" {
		return false
	}
	if hgw == "construction" || w.Tags["highway"] == "construction" {
		return false
	}
	if hgw == "proposed" || w.Tags["highway"] == "proposed" {
		return false
	}

	// OSRM access_tag_blacklist
	access := w.Tags["access"]
	if access == "no" || access == "agricultural" || access == "forestry" ||
		access == "emergency" || access == "psv" || access == "customers" ||
		access == "private" || access == "delivery" || access == "destination" {
		return false
	}

	// Проверка motor_vehicle
	motorVehicle := w.Tags["motor_vehicle"]
	if motorVehicle == "no" || motorVehicle == "agricultural" ||
		motorVehicle == "forestry" || motorVehicle == "emergency" {
		return false
	}

	// Проверка motorcar
	motorcar := w.Tags["motorcar"]
	if motorcar == "no" || motorcar == "agricultural" ||
		motorcar == "forestry" || motorcar == "emergency" {
		return false
	}

	return true
}
