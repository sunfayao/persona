package deeplink

// fixture is spec/fixtures/deep-links/cases.json's sections this package writes (its README says what each holds).
type fixture struct {
	Version int `json:"version"`
	Build   []struct {
		Name string            `json:"name"`
		Page map[string]string `json:"page"`
		Path string            `json:"path"`
	} `json:"build"`
	Links []struct {
		Name string            `json:"name"`
		Base string            `json:"base"`
		Page map[string]string `json:"page"`
		Web  string            `json:"web"`
		App  string            `json:"app"`
	} `json:"links"`
}
