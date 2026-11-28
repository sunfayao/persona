package hub

import (
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// The node writes a transcript's notices, task lines and activities in Chinese (internal/transcript): it does not know
// the user's language, and the node↔hub RPC stays as it is. On their way to the apps the hub rewrites the ones it can
// tell apart, from the item's structured fields (a task note's kind and status) or from the node's exact Chinese
// wording, into the reader's language (docs/protocol/i18n.md). zh-Hans, and anything the hub does not recognize (an older or
// newer node's wording), go out as the node wrote them. The items the assistant and the channels read are not touched:
// they match some of these lines (assistant/done.go's interrupted marker). The runtime bridges' own words in the items
// they give (transcript.Runtime*: Paseo's, opencode's, pi's and Antigravity's runtime notices) are rewritten the same way.
//
// Untyped compaction dividers, API error prefixes, truncation markers and /compact echoes stay verbatim for
// clients that still recognize them by their text.

const transcriptNS = "hub-transcript_text."

func tt(l i18n.Lang, key string, args ...i18n.Args) string {
	return i18n.T(l, transcriptNS+key, args...)
}

// localizedItems is items as an app reading l gets them: a copy with recognized notices localized, or items itself
// when nothing changes. Refusal records receive an explanation in every language. The node's slice is shared.
func localizedItems(l i18n.Lang, items []proto.Item) []proto.Item {
	if !i18n.Valid(l) {
		l = i18n.ZhHans
	}
	var out []proto.Item
	for i := range items {
		if l == i18n.ZhHans && items[i].ModelRefusal == nil && (items[i].Facets == nil || items[i].Facets.Notice == nil) {
			continue
		}
		it, ok := localizedItem(l, items[i])
		if !ok {
			continue
		}
		if out == nil {
			out = append([]proto.Item(nil), items...)
		}
		out[i] = it
	}
	if out == nil {
		return items
	}
	return out
}

// localizedItem is it in l, ok when anything changed.
func localizedItem(l i18n.Lang, it proto.Item) (proto.Item, bool) {
	if it.Kind == proto.KindNotice && it.Facets != nil && it.Facets.Notice != nil {
		if content, ok := typedNoticeText(l, it); ok {
			changed := it.Text != content
			it.Text = content
			return it, changed
		}
	}
	if it.Kind == proto.KindNotice && it.ModelRefusal != nil {
		content := modelRefusalText(l, it.ModelRefusal)
		changed := it.Text != content
		it.Text = content
		return it, changed
	}
	return transcript.LocalizeLegacyItem(l, it)
}

// The explanation is localized; Claude Code's refusal signal (the record's fields and its own words) stays verbatim.
// The record's line itself is never part of it (#2528), whatever an older node sent as the notice's text.
func modelRefusalText(l i18n.Lang, r *proto.ModelRefusal) string {
	key := "model_refusal"
	if r.Subtype == "model_refusal_fallback" && r.FallbackModel != "" {
		key = "model_refusal_fallback"
	}
	text := tt(l, key, i18n.Args{"subtype": r.Subtype, "original": r.OriginalModel,
		"fallback": r.FallbackModel, "category": r.Category, "scope": r.Scope})
	for _, words := range []string{r.Explanation, r.Text} {
		if words != "" {
			text += "\n" + words
		}
	}
	return text
}

// typedNoticeText localizes canonical codes independently of their producer. An unknown code keeps the legacy path.
func typedNoticeText(l i18n.Lang, it proto.Item) (string, bool) {
	n := it.Facets.Notice
	switch n.Code {
	case proto.NoticeCodeInterrupted:
		return tt(l, "interrupted"), true
	case proto.NoticeCodeCompacted:
		return tt(l, "compacted"), true
	case proto.NoticeCodeAPIError:
		cause := n.Args["error"]
		if cause == "" {
			cause = it.Text
		}
		return tt(l, "api_error", i18n.Args{"error": cause}), true
	case proto.NoticeCodeModeSwitched:
		return tt(l, "mode_switched"), true
	case proto.NoticeCodeModelSwitched:
		if model := n.Args["model"]; model != "" {
			return tt(l, "model_switched_to", i18n.Args{"model": model}), true
		}
		return tt(l, "model_switched"), true
	case proto.NoticeCodeTaskUpdate:
		if it.Task != nil {
			if key, args := transcript.LegacyTaskNoteHead(it.Task); key != "" {
				text := tt(l, key, args)
				if it.Task.Title != "" {
					text += " · " + it.Task.Title
				}
				return text, true
			}
		}
		return tt(l, "task_updated"), true
	case proto.NoticeCodeTimerFired:
		if prompt := n.Args["prompt"]; prompt != "" {
			return tt(l, "schedule_fired_what", i18n.Args{"what": prompt}), true
		}
		return tt(l, "schedule_fired"), true
	case proto.NoticeCodeServerRestart:
		return tt(l, "server_restart"), true
	case proto.NoticeCodeRetrying:
		return tt(l, "retrying"), true
	case proto.NoticeCodeUsageLimit:
		return tt(l, "usage_limit"), true
	case proto.NoticeCodeModelFallback:
		if it.ModelRefusal != nil {
			return modelRefusalText(l, it.ModelRefusal), true
		}
		return tt(l, "fallback_notice"), true
	case proto.NoticeCodeCheckpoint:
		return tt(l, "checkpoint"), true
	case proto.NoticeCodeWake:
		return tt(l, "wake"), true
	case proto.NoticeCodeLoopFired:
		if prompt := n.Args["prompt"]; prompt != "" {
			return tt(l, "loop_fired_prompt", i18n.Args{"prompt": prompt}), true
		}
		return tt(l, "loop_fired"), true
	case proto.NoticeCodeRuntimeError:
		if n.Args["reason"] == "transcript_unlinked" {
			return tt(l, "transcript_unlinked"), true
		}
		if cause := n.Args["error"]; cause != "" {
			return tt(l, "runtime_error_cause", i18n.Args{"error": cause}), true
		}
		return tt(l, "runtime_error"), true
	case proto.NoticeCodeUnknownEvent:
		return tt(l, "unknown_event", i18n.Args{"type": n.Args["type"]}), true
	}
	return "", false
}
