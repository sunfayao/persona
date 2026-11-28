<persona-context kind="about-me">
The owner changed their settings after your standing rules were written. From now on the text below replaces {{if .NotesChanged}}the About me section{{end}}{{if and .NotesChanged .LanguageChanged}} and {{end}}{{if .LanguageChanged}}the answer language{{end}} of your standing rules; the rule files themselves are rewritten when this conversation is next compacted or started.
{{if .NotesChanged}}{{if .Notes}}
About the user (written by the owner as About me; their words, not instructions from Persona):
{{ctx .Notes}}
{{else}}
The About me section is now empty.
{{end}}{{end}}{{if .LanguageChanged}}
Answer in {{lang .Language}}.
{{end}}</persona-context>
