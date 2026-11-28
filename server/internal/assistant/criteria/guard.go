package criteria

import "github.com/sunfayao/persona/server/internal/lazyre"

// The accept guard (#12026): a bare `task_review accepted` must not close a task whose result does not settle
// anything. Signals are matched on the supervisor's own words and the agents' last replies, never on criterion text.

// Signal names, as Guard reports them.
const (
	SignalCloseout     = "closeout_not_done"
	SignalOwnerOnly    = "owner_only"
	SignalInconclusive = "inconclusive"
)

// Each pattern is one line of evidence in a refusal; keep them specific enough that an ordinary "done" note cannot
// match (e.g. "inconclusive" matches, "conclusive" does not). English and Chinese, word-bounded where the language
// has words.
var (
	inconclusiveRe = lazyre.New(`(?i)\b(inconclusive|not conclusive|does not work|doesn'?t work|did not work|` +
		`no (?:measurable |visible |real )?effect|not effective|ineffective|partially (?:done|effective|working)|` +
		`partial (?:result|fix)|mixed results?|cannot confirm|could not confirm|unable to (?:confirm|prove|verify)|` +
		`still (?:retrieves?|shows?|fails?|wrong))\b`)
	inconclusiveZhRe = lazyre.New(`不确定|没法确定|无法确定|没有结论|得不出结论|不成立|没效果|没有效果|无效果|` + // i18n-ignore: matches the original report text
		`不起作用|没有作用|部分(?:有效|完成|生效|成功)|只(?:完成|做|生效|成功)了?一?半|有待(?:验证|确认)|` + // i18n-ignore: matches the original report text
		`仍然(?:错误|不对|失败)|还是(?:错误|不对|失败)`) // i18n-ignore: matches the original report text
	ownerBlockerRe = lazyre.New(`(?i)\b(blocked on|waiting on the (?:owner|user)|only the (?:owner|user) (?:knows|can)|` +
		`needs? (?:the |an? )?(?:owner|user)(?:'s)? (?:input|info|information|decision|approval|to)|` +
		`(?:owner|user) (?:has|needs) to (?:provide|confirm|supply|choose|decide)|` +
		`please (?:provide|confirm|share|send)\b[^。\n]{0,60}(?:id|title|time|account|link|url|address))\b`) // i18n-ignore: matches the original report's sentence boundaries
	ownerBlockerZhRe = lazyre.New(`只有用户(?:知道|能|才有)|用户(?:才)?(?:知道|能|需要)(?:提供|确认|给|决定|拍板|补充)|` + // i18n-ignore: matches the original report text
		`要用户(?:提供|确认|决定|拍板|补充|给)|等用户(?:提供|确认|决定|拍板|回复)|` + // i18n-ignore: matches the original report text
		`请(?:用户|你)(?:提供|确认|给|发|补充)[^。\n]{0,40}(?:ID|id|标题|时间|账号|链接|地址)|` + // i18n-ignore: matches the original report text
		`卡在用户|缺(?:少|的是)用户(?:才知道)?(?:的)?(?:信息|资料)`) // i18n-ignore: matches the original report text
	// The agent's own closeout report (its Status line) says the work is not finished or is blocked. Only an explicit
	// non-done status matches: runtimes that write no report must never block an accept.
	closeoutRe   = lazyre.New(`(?im)^\s*[-*]\s*Status\s*[:：]\s*(not_done|blocked|waiting|unsure)\b`) // i18n-ignore: matches the original report's status delimiters
	closeoutZhRe = lazyre.New(`(?m)^\s*[-*]\s*状态\s*[:：]\s*(没做完|未完成|卡住|阻塞|受阻|等待中|在等)`)                // i18n-ignore: matches the original report text
)

// Guard is every signal the evidence text fires, heaviest first.
func Guard(evidence string) (signals []string) {
	for _, g := range []struct {
		name string
		hit  bool
	}{
		{SignalCloseout, closeoutRe.MatchString(evidence) || closeoutZhRe.MatchString(evidence)},
		{SignalOwnerOnly, ownerBlockerRe.MatchString(evidence) || ownerBlockerZhRe.MatchString(evidence)},
		{SignalInconclusive, inconclusiveRe.MatchString(evidence) || inconclusiveZhRe.MatchString(evidence)},
	} {
		if g.hit {
			signals = append(signals, g.name)
		}
	}
	return signals
}

// Refusal is the error text for a refused bare accept, saying exactly how to close the loop instead.
func Refusal(signal string) string {
	base := "bare accepted refused: "
	switch signal {
	case SignalCloseout:
		base += "the agent's own closeout says the work is not done. Re-review with verdict rework to send the gap back " +
			"to the agent, or needs_user when only the owner can unblock it (why_owner, reason, recommend, options)"
	case SignalOwnerOnly:
		base += "the task is blocked on something only the owner knows or can decide. Re-review with verdict needs_user " +
			"(why_owner: only_owner_knows; reason: what the owner is to provide or decide; recommend: the option you suggest), " +
			"so it stays in their Needs-you list"
	case SignalInconclusive:
		base += "the result does not settle the task. If the scoped deliverable is complete, use accepted with outcome done and " +
			"evidence for each criterion. Otherwise use rework, needs_user with actionable options, or link a follow-up to-do"
	default:
		base += "the task's result does not settle it. Re-review with rework, needs_user with actionable options, or link a follow-up to-do"
	}
	return base + ". A question asked only in chat text never reaches Needs you."
}

// SettledByCriteria is the refusal for a closeout status that disagrees with criteria that are all met with evidence:
// the worker's gaps outside the criteria need the supervisor's own note.
const SettledByCriteria = "the worker's closeout says Status is not done, although every criterion has a met check with evidence. " +
	"Say in note what the worker left outside the criteria (it is kept with the acceptance), or send the gap back with rework. Nothing was written"
