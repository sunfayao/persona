package nodeproto

// TraexStartModes announces that terminal TRAE starts take permission_mode and effort.
const TraexStartModes = "traex_start_modes"

func init() { Announced = append(Announced, TraexStartModes) }
