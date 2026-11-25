(Supervision: the independent review did not pass, rework round {{.Round}}, at most {{.Max}} rounds)
An independent reviewer saw only the task requirements and your change this time, not your reasoning. It raised {{.Blocking}} blocking issues{{if gt .Count .Blocking}} ({{.Count}} findings in all){{end}}; the report is below.
Check each one against the current code: fix those that hold, and for those that do not, explain why (compare with your code as it is now, not from memory). When done, say which you fixed and which are false positives and why, and give the evidence for each acceptance criterion.

{{.Findings}}
