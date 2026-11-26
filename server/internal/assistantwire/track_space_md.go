package assistantwire

// DeviceAssistantSpaceMD: SpaceMDCall → SpaceMD, a track's SPACE.md on the assistant's machine.
const DeviceAssistantSpaceMD = "assistant.space_md"

// SpaceMDStateMax is the most bytes a SPACE.md's state block may hold (space_state_update).
const SpaceMDStateMax = 2048

// What a SpaceMDCall does.
const (
	// SpaceMDPutState replaces the inner text of its state block with State, and nothing else.
	SpaceMDPutState = "put_state"
)

// Why a SpaceMDCall wrote nothing (SpaceMD.Refused).
const (
	// SpaceMDManaged: State holds a managed marker.
	SpaceMDManaged = "managed"
	// SpaceMDTooLarge: Content is over 64 KB, or State over SpaceMDStateMax.
	SpaceMDTooLarge = "too_large"
	// SpaceMDNoState: the SPACE.md has no single state block (the user took it out).
	SpaceMDNoState = "no_state_block"
	// SpaceMDMissing: the track's directory or its SPACE.md is not there.
	SpaceMDMissing = "missing"
)

// SpaceMDCall is DeviceAssistantSpaceMD's params.
type SpaceMDCall struct {
	Op string `json:"op"`
	// Dir is the track's directory relative to the machine's home, as assistant.prepare's dir; Slug its track's.
	Dir   string `json:"dir"`
	Slug  string `json:"slug"`
	State string `json:"state,omitempty"`
}

// SpaceMD is a track's SPACE.md as it is now after a state update.
type SpaceMD struct {
	Content string `json:"content"`
	// Digest is the hex SHA-256 of Content.
	Digest string `json:"digest"`
	// State is the inner text of the state block, "" when it is empty or missing.
	State string `json:"state"`
	// Refused says why nothing was written (for example, SpaceMDMissing).
	Refused string `json:"refused,omitempty"`
}
