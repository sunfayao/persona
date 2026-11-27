package link

import (
	"reflect"
	"testing"
)

// sshReady also requires the ordered endpoints and node messages, so lifecycle actions cannot close the socket while
// those initial writes are still pending.
func sshReady(s Status, connected func(Status) bool) bool {
	return connected(s) && s.HubName == "home" && s.Node == "online"
}

// waitForSSHReady is the shared readiness gate for SSH lifecycle actions. The
// connected match keeps each lifecycle step's connection and mode requirements.
func waitForSSHReady(t *testing.T, ch chan Status, connected func(Status) bool) Status {
	t.Helper()
	return waitFor(t, ch, func(s Status) bool { return sshReady(s, connected) })
}

func TestSSHLifecycleReadinessObservesInitialHubMessages(t *testing.T) {
	connected := func(s Status) bool {
		return s.State == "connected" && s.Kind == KindSSH && s.Conn == 1
	}

	t.Run("connected before initial hub messages", func(t *testing.T) {
		ch := make(chan Status, 3)
		ch <- Status{State: "connected", Kind: KindSSH, Conn: 1, Node: "unknown"}
		ch <- Status{State: "connected", Kind: KindSSH, Conn: 1, HubName: "home", Node: "unknown"}
		ch <- Status{State: "connected", Kind: KindSSH, Conn: 1, HubName: "home", Node: "online"}

		st := waitForSSHReady(t, ch, connected)
		if st.HubName != "home" || st.Node != "online" {
			t.Fatalf("SSH lifecycle became ready before the hub's initial endpoints and node messages were observed: %+v", st)
		}
	})

	t.Run("normal ready control", func(t *testing.T) {
		ch := make(chan Status, 1)
		want := Status{State: "connected", Kind: KindSSH, Conn: 1, HubName: "home", Node: "online"}
		ch <- want

		if got := waitForSSHReady(t, ch, connected); !reflect.DeepEqual(got, want) {
			t.Fatalf("ready SSH status = %+v, want %+v", got, want)
		}
	})
}
