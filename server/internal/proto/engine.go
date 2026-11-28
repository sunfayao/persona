package proto

// EngineHerdr is the terminal engine every node drives: the herdr command line, built in and always on. It is no
// setting: Persona starts and drives every terminal Agent in herdr (docs/protocol/protocol.md "Engine").
const EngineHerdr = "herdr"

// HostAgy is the host a start names to run on the computer's Antigravity companion (the agy runtime's terminal-less
// sessions, node runtimeOfIn) instead of in herdr. It is the one host a start may name: a new Agent without it runs agy
// in a herdr pane (#10237), a task the assistant starts runs it here when the companion is connected (#13447).
const HostAgy = "agy"

// Engine is herdr as found on a machine (node.info's engines, the machine's capabilities): Available when its server
// answers, its socket file is there or its binary is on PATH; Version what it reports.
type Engine struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
}
