package assistant

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// mutes hands the service the mutes as the hub does (fleet.mutes, after a handshake and after every change).
func (ft *fleetTest) mutes(p Pins) {
	ft.t.Helper()
	raw, err := json.Marshal(assistantwire.FleetMutes{Mutes: &assistantwire.Mutes{Spaces: p.Spaces, Sessions: p.Sessions, Allowed: p.Allowed}})
	if err != nil {
		ft.t.Fatal(err)
	}
	ft.s.FleetEvent(context.Background(), assistantwire.EventMutes, raw)
}
