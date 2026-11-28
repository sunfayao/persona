package prompts

// ToolFrictionNote is a field note the friction dream drafts (assistant/tooluse): calls of Tool with arguments shaped
// Failed fail with Code, and a retry shaped Worked worked. The shapes are argument skeletons, with no value in them.
type ToolFrictionNote struct {
	Tool, Failed, Code, Worked string
}

func RenderToolFrictionNote(d ToolFrictionNote) string {
	return render("assistant/tool_friction_note", d)
}

// ToolFrictionFix is a fix suggestion the friction dream drafts (assistant/tooluse): calls of Tool shaped Failed fail
// with Code, Count times in Conversations conversations, between the days From and To (UTC, 2006-01-02). Worked is the
// shape of a retry that worked although the code says the tool itself failed; "" when no retry worked.
type ToolFrictionFix struct {
	Tool, Failed, Code, Worked string
	Count, Conversations       int
	From, To                   string
}

func RenderToolFrictionFix(d ToolFrictionFix) string { return render("assistant/tool_friction_fix", d) }
