package proto

// CodeFileChanged indicates that a file changed since its revision was read.
const CodeFileChanged = "file_changed"

// MachineFileTransfer is a machine's file transfer switch (settings key machine_file_transfer, off until the user
// turns it on): Inbox is its receive folder as hub.json names it ("~/…" or absolute), set while it is on.
type MachineFileTransfer struct {
	Enabled bool   `json:"enabled"`
	Inbox   string `json:"inbox,omitempty"`
}

// CodeFileTransferDisabled: the machine's file transfer switch is off (403): a dest=inbox upload to it.
const CodeFileTransferDisabled = "file_transfer_disabled"
