It is time to check this matter again. {{if .Who}}Waiting on: {{.Who}}{{else}}Waiting for the other party's reply{{end}}{{with .Next}}; next step: {{.}}{{end}}. This is consecutive check {{.Checks}}.

Do not start Agents, send messages or directly perform any action visible to others in this turn. Use read-only tools for investigation; the program executes suggestions only after {{.Owner}} confirms. First read the current state (new source messages, related task or Agent progress), not just the old recap. Then call inbox_suggest with exactly one of:
- A conclusion exists: kind conclusion, outcome done if finished, fyi if {{.Owner}} only needs to know.
- An action needs {{.Owner}}'s confirmation: one suggestion, such as a kind reply reminder.
- Still waiting: no kind, text or other suggestion fields; give recap and follow (title and summary still follow the original requirements). follow must include a new check_at or check_in, with who and next matching the current state.

Always update recap, then end this turn.
