package hub

import (
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Shortcuts the notification tests use. The hub itself notifies through Hub.notifyInteraction / Hub.notifyDone
// (notifyws.go) and stores settings with editPrefs; these write the same rows and build the same payloads.

// setPrefs stores a device's notification settings (also for a device the hub knows nothing else about).
func (p *Pusher) setPrefs(id string, prefs NotifyPrefs) error { return p.setPrefsBy(id, "", prefs) }

// interactionPayload is interactionPayloadIn in the default language.
func interactionPayload(ix proto.Interaction, machine string) map[string]any {
	return interactionPayloadIn(i18n.Default, ix, machine)
}

// roleRetiredAnswer is the role of a watch's answer-only token, which the hub no longer knows: a token that still has it
// opens nothing.
const roleRetiredAnswer = "answer"
