package hub

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The machine list says why a computer cannot share its clipboard in the reader's language.
func TestClipboardReasonLocalized(t *testing.T) {
	in := []proto.Machine{{ID: "server", Clipboard: &proto.MachineClipboard{ReasonCode: proto.ClipboardReasonNoDisplay}}, {ID: "old"}}
	en := localizedMachines(i18n.En, in)
	zh := localizedMachines(i18n.ZhHans, in)
	if en[0].Clipboard.Reason == "" || zh[0].Clipboard.Reason == "" || en[0].Clipboard.Reason == zh[0].Clipboard.Reason {
		t.Fatalf("reasons: %q %q", en[0].Clipboard.Reason, zh[0].Clipboard.Reason)
	}
	if in[0].Clipboard.Reason != "" {
		t.Fatal("the manager's snapshot was changed")
	}
	if en[1].Clipboard != nil {
		t.Fatal("an older manager's machine has no clipboard")
	}
}
