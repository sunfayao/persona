package connectorwire

// Media is one media's bytes and what they are.
type Media struct {
	Mime string `json:"mime"`
	Data []byte `json:"data"`
}
