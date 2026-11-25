New external information arrived while a needs_user ask is still open (card untouched):
   - New information{{with .Source}} ({{.}}){{end}}: {{.Summary}}
   - Pending needs_user ask: {{.PendingQuestion}}{{with .PendingRecommend}} (recommended: {{.}}){{end}}
   - Evaluate whether this resolves or supersedes the pending ask. If the owner still must decide, call supervision_finish(visibility="silent") so the original card stays untouched. If this resolves or supersedes it, call task_review with the new verdict and a clear natural-language reason to clear the stale card (never fabricate an owner selection with task_decided).
