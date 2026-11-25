package assistant

// How a computer stands when the assistant checks it before a session on it (dispatch state, A2A delivery).
const (
	// standingComputerOffline: the computer is not connected now.
	standingComputerOffline = "computer_offline"
	// standingComputerRemoved: the computer is no longer one of this manager's computers.
	standingComputerRemoved = "computer_removed"
)

// computerStanding is how a computer stands: state is empty when it is connected; listed is whether its session list is
// known on this connection.
type computerStanding struct {
	state  string
	listed bool
}

// computerState is how machine stands, from the devices: connected or not is the computer's own connection (online or
// reachable, the readiness check's rule), never whether its list could be read. Without the manager's own fleet (a
// hub answers the lists) its list decides.
func (s *Service) computerState(machine string) computerStanding {
	c := computerStanding{listed: true}
	if s.fleetOn() {
		d, ok := s.devs.Summary(machine)
		switch {
		case !ok:
			c.state = standingComputerRemoved
		case !d.Online && !d.Reachable:
			c.state = standingComputerOffline
		}
		if shown, ok := s.devs.Shown(machine); ok {
			c.listed = shown.Listed
		}
	}
	return c
}
