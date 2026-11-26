package assistantwire

// Resource routes are manager-owned; the hub only relays requests.
func init() {
	Routes = append(Routes,
		"GET /api/v1/resources",
		"PUT /api/v1/resources",
	)
}
