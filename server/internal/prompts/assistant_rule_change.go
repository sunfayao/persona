package prompts

// AssistantRuleChange is the one-off <persona-context kind="about-me"> block a running conversation gets when the
// owner changed About me or the reply language after its standing rule files were written (assistant/rule_files).
type AssistantRuleChange struct {
	Notes           string // About me, trimmed; "" says it is now empty
	NotesChanged    bool
	Language        string // the reply language's name, as the rules print it ("" is Chinese)
	LanguageChanged bool
}

func RenderAssistantRuleChange(d AssistantRuleChange) string {
	return render("assistant/rule_change", d)
}
