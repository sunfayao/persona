package toolnotes

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Digest is a digest of t's description as Persona wrote it, the same in every language the description is shown in
// (prompts.LocalizeTool): a note keeps its tool's, so a release that changes the description retires the note, and a
// change of the user's language does not.
func Digest(t proto.Tool) string {
	desc := prompts.LocalizeTool(prompts.MCPTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}, "").Description
	sum := sha256.Sum256([]byte(desc))
	return hex.EncodeToString(sum[:8])
}

// Annotate is tools with the notes that hold for the assistant's conversations appended to their descriptions, as a
// "Field notes:" block: approved, before their review date, for every runtime or the one the conversations run on,
// and proposed on the description the tool still has. A note whose tool's description changed is left out here and
// retired by Sweep. The list is copied, not changed.
func (d *Domain) Annotate(ctx context.Context, tools []proto.Tool) []proto.Tool {
	shown := d.shown(ctx)
	if len(shown) == 0 {
		return tools
	}
	out := slices.Clone(tools)
	for i, t := range out {
		notes := shown[t.Name]
		if len(notes) == 0 {
			continue
		}
		digest := Digest(t)
		notes = slices.DeleteFunc(slices.Clone(notes), func(n Note) bool { return n.PromptDigest != digest })
		if block := d.block(notes); block != "" {
			out[i].Description += "\n\n" + block
		}
	}
	return out
}

// Revision is a digest of the notes the assistant's conversations' tool lists show now (":notes=" and 16 hex digits),
// for the tool-list revision a tool watch compares: a note approved, retired, renewed or past its review date, a
// computer renamed or the conversations' runtime changed changes it. "" when no note is shown.
func (d *Domain) Revision(ctx context.Context) string {
	shown := d.shown(ctx)
	if len(shown) == 0 {
		return ""
	}
	h := sha256.New()
	for _, tool := range slices.Sorted(maps.Keys(shown)) {
		h.Write([]byte(tool + "\x00" + d.block(shown[tool]) + "\x00"))
	}
	return ":notes=" + hex.EncodeToString(h.Sum(nil))[:16]
}

// shown are the notes in the descriptions now, by tool, newest first: approved, before their review date, and for
// every runtime or the conversations' own. The runtime is asked only when a note is scoped to one.
func (d *Domain) shown(ctx context.Context) map[string][]Note {
	now := d.now()
	var notes []Note
	scoped := false
	for _, n := range d.held(ctx).Notes {
		if n.shownAt(now) {
			notes = append(notes, n)
			scoped = scoped || n.Runtime != ""
		}
	}
	if len(notes) == 0 {
		return nil
	}
	runtime := ""
	if scoped {
		runtime = d.h.Runtime(ctx)
	}
	out := map[string][]Note{}
	for _, n := range notes {
		if n.Runtime == "" || n.Runtime == runtime {
			out[n.Tool] = append(out[n.Tool], n)
		}
	}
	for _, list := range out {
		slices.SortFunc(list, func(a, b Note) int {
			return cmp.Or(b.ApprovedAt.Compare(a.ApprovedAt), cmp.Compare(a.ID, b.ID))
		})
	}
	return out
}

// block is one tool's "Field notes:" block: its notes in order while they fit, at most MaxPerTool of them in at most
// MaxBlock bytes (a note too long for what is left is skipped); "" when none fits.
func (d *Domain) block(notes []Note) string {
	var kept []prompts.FieldNote
	block := ""
	for _, n := range notes {
		if len(kept) == MaxPerTool {
			break
		}
		note := prompts.FieldNote{Text: n.Text}
		if n.Machine != "" {
			note.Machine = cmp.Or(d.h.MachineName(n.Machine), n.Machine)
		}
		next := append(slices.Clone(kept), note)
		if b := prompts.RenderToolFieldNotes(next); len(b) <= MaxBlock {
			kept, block = next, b
		}
	}
	return block
}
