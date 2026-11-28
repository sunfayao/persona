package prompts

// FieldNote is one field note as a tool's description ends with it (assistant/toolnotes): the user-approved line, and
// the computer it holds on ("" for every computer).
type FieldNote struct {
	Machine string
	Text    string
}

// RenderToolFieldNotes is the "Field notes:" block appended to a tool's description, one line a note, with no newline
// at either end.
func RenderToolFieldNotes(notes []FieldNote) string {
	return render("assistant/tool_field_notes", notes)
}

// SnapshotToolReview is what waits for review in tool_note list: friction drafts, and field notes past their review
// date.
type SnapshotToolReview struct {
	Drafts, NotesDue int
}
