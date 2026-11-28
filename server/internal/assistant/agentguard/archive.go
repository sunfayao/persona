package agentguard

import (
	"fmt"
	"time"
)

// OwnerInputWindow is how long after the owner last used a session themselves the assistant leaves it unarchived.
const OwnerInputWindow = 24 * time.Hour

// ArchiveFacts are what the archive guard needs to know about one session.
type ArchiveFacts struct {
	Now time.Time
	// LastOwnerInput: when the owner last typed in the session or answered a card of it (zero: not seen).
	LastOwnerInput time.Time
	// OpenTodo: the id of the open to-do the session is actively linked to ("": none).
	OpenTodo string
	// OwnerTurn tells whether this call is in a turn the owner's own message started; asked only when a refusal would
	// otherwise apply.
	OwnerTurn func() bool
}

// ArchiveRefusal is why the assistant's agent_archive may not archive the session called label on its own, "" when
// it may. The owner's own turn is allowed to archive anything; restoring is never refused (not asked here).
func ArchiveRefusal(label string, f ArchiveFacts) string {
	recent := !f.LastOwnerInput.IsZero() && f.Now.Sub(f.LastOwnerInput) < OwnerInputWindow
	if !recent && f.OpenTodo == "" || f.OwnerTurn != nil && f.OwnerTurn() {
		return ""
	}
	var why string
	switch {
	case recent && f.OpenTodo != "":
		why = fmt.Sprintf("the owner gave it input in the last 24 hours and it is linked to open to-do %s", f.OpenTodo)
	case recent:
		why = "the owner gave it input in the last 24 hours (they may still be using it)"
	default:
		why = fmt.Sprintf("it is linked to open to-do %s", f.OpenTodo)
	}
	return fmt.Sprintf("refused: %s: %s. Archiving such a session needs the owner to ask for it in their own message. Nothing changed.", label, why)
}
