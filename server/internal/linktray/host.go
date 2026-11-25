package linktray

// PairError is a pairing failure classified for the connector, not a user-interface implementation.
// It is retained because cmd/persona-link's pairError uses it while the retired Windows tray is gone.
type PairError struct {
	Rejected bool
	Hosts    []string
	Err      error
}
