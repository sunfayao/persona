package assistant

import (
	"context"
	"encoding/json"
)

// Interleave another Manager call after registration, before the first prepare reaches Node.
type heldWorkspacePrepareHub struct {
	Hub
	beforePrepare func()
}
