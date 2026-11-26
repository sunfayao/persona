package node

import (
	"runtime"
	"strings"
	"testing"
)

// The node's own options: the agent looked at on a Mac, never reloaded from inside.
func TestNodeDoctorOptionsStayInsideTheNode(t *testing.T) {
	e := newDoctorEnv(t)
	opts := e.n.doctorOptions()
	if !opts.InsideNode {
		t.Error("node.doctor may reload the agent it runs under")
	}
	if (opts.LaunchAgent != "") != (runtime.GOOS == "darwin") {
		t.Errorf("LaunchAgent %q on %s", opts.LaunchAgent, runtime.GOOS)
	}
	// No agent in this home: today's label, where a new install writes it.
	if runtime.GOOS == "darwin" && (opts.LaunchAgent != NodeLaunchAgentPath(e.n.home, NodeLaunchdLabel) ||
		!strings.HasSuffix(opts.LaunchdTarget, "/"+NodeLaunchdLabel)) {
		t.Errorf("LaunchAgent %q %q", opts.LaunchAgent, opts.LaunchdTarget)
	}
	if (opts.Preflight != nil) != (runtime.GOOS != "windows") {
		t.Error("preflight")
	}
}
