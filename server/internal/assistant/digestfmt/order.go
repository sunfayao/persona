package digestfmt

import (
	"sort"

	"github.com/sunfayao/persona/server/internal/prompts"
)

// rank is how soon a signal kind comes in a message: what waits on a decision first.
var rank = map[string]int{"needs_you": 0, "ask": 0, "escalated": 0, "decide": 0, "owner_cannot_answer": 0, "undecidable": 1, "delivery_hold": 1,
	"recheck": 1, "cardless_wait": 1, "needs_user_external_info": 1, "estimate_due": 2, "parked_due": 2, "park_decided": 2, "idle": 2, "unjudged": 2,
	"all_stopped": 2, "failed": 3, "started": 3, "todo_ready": 3, "pr_merged": 3, "parent_closeout": 3, "interrupted": 4, "orphaned": 4,
	"timed_out": 4, "review": 5, "stalled": 6, "no_progress": 6, "overdue": 6, "inbox": 6, "report": 7}

// Sorted puts what waits on a decision first, in place, and returns list.
func Sorted(list []prompts.SuperviseEntry) []prompts.SuperviseEntry {
	at := func(e prompts.SuperviseEntry) int {
		switch {
		case e.Kind == "check" && e.Report == "decided": // the owner's answer
			return rank["owner_cannot_answer"]
		case e.Kind == "check":
			return rank["stalled"]
		case e.Kind != "report":
		case e.Report == "question" || e.Report == "blocked": // waits on a decision too
			return 1
		case e.Report == "done": // with the work waiting for review
			return rank["review"]
		}
		return rank[e.Kind]
	}
	sort.SliceStable(list, func(i, j int) bool { return at(list[i]) < at(list[j]) })
	return list
}
