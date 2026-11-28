package nodeproto

// RuleNotice: the node puts assistant.context's rule_notice, a one-off <persona-context kind="about-me"> block, into
// the message it sends and confirms its receipt (item_id "rule_change"). Only then does the manager keep the standing
// rule files of a running conversation as they were when it loaded them and tell it of a change in About me or the
// reply language by that block; for a node without it the files are rewritten when the settings change, as before. The
// same field carries the one-off blocks of a supervision message (the supervision policy a conversation with only a short
// section of it is told once, a digest told whole after a compaction): a node without it keeps the policy in the standing
// text and is told every digest whole.
const RuleNotice = "rule_notice_v1"

func init() { Announced = append(Announced, RuleNotice) }
