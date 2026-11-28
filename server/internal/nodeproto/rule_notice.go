package nodeproto

// RuleNotice: the node puts assistant.context's rule_notice, a one-off <persona-context kind="about-me"> block, into
// the message it sends and confirms its receipt (item_id "rule_change"). Only then does the manager keep the standing
// rule files of a running conversation as they were when it loaded them and tell it of a change in About me or the
// reply language by that block; for a node without it the files are rewritten when the settings change, as before.
const RuleNotice = "rule_notice_v1"

func init() { Announced = append(Announced, RuleNotice) }
