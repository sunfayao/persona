package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// Each manager works out its own to-dos' views and counts (#10226). The hub relays them and adds the counts
// field by field and key by key. Clients order a merged list by rank, then manager, then id.

const (
	// featureTodoView: every row of GET /api/v1/todos carries `view`, the list `counts`, and GET /api/v1/todos/{id}
	// answers one row so (docs/protocol/protocol.md, the to-dos rows).
	featureTodoView = assistantwire.CapabilityTodoView
	// featureTodoTimeline: GET /api/v1/todos/{id} carries the rounds the paper shows, its results and
	// what changed since the owner looked (#10465).
	featureTodoTimeline = assistantwire.CapabilityTodoTimeline
	// featureTodoImages: POST /api/v1/todos persists images and serves their private bytes (#11084). A hub-wide flag
	// cannot promise image creation for only some managers, so an older or offline one hides the picker.
	featureTodoImages = assistantwire.CapabilityTodoImages
)

// todoViewFeatures advertises todo_view while every manager whose to-dos the list merges (listManagers)
// is connected and names it in its hello: a hub-wide feature cannot promise the view for part of the list, so an older
// or offline manager hides it until it is ready, and clients keep their own rules until then.
func (h *Hub) todoViewFeatures() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	view := true
	for _, id := range h.listManagers(ctx) {
		view = view && h.archiveManagerHas(id, assistantwire.CapabilityTodoView) == http.StatusOK
	}
	if view {
		return []string{featureTodoView}
	}
	return nil
}

// todoTimelineFeatures is todo_timeline while every manager whose to-dos the list merges is connected and names it, as
// todoViewFeatures has todo_view: a hub-wide feature cannot promise a to-do's history for part of the list, so an older
// or offline manager hides it until it is ready, and clients draw no conversation, results or recap until then.
func (h *Hub) todoTimelineFeatures() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, id := range h.listManagers(ctx) {
		if h.archiveManagerHas(id, assistantwire.CapabilityTodoTimeline) != http.StatusOK {
			return nil
		}
	}
	return []string{featureTodoTimeline}
}

// todoImageFeatures is todo_images only while every manager whose to-dos the list merges is connected and names it.
// An older manager would ignore the additive images field and create an image-less to-do, so the Web offers no picker
// in that case. It does not gate reading metadata or bytes that a capable manager already stored.
func (h *Hub) todoImageFeatures() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, id := range h.listManagers(ctx) {
		if h.archiveManagerHas(id, assistantwire.CapabilityTodoImages) != http.StatusOK {
			return nil
		}
	}
	return []string{featureTodoImages}
}

// todoListSums are the members of a merged to-dos list the hub adds up over its answers (mergeList, before the list's
// validator is taken): counts, those of every answer the merge takes that has them (sumTodoCounts). None when no such
// answer has them (every manager is older): the merged list then has none either.
func todoListSums(answers []listAnswer) map[string]json.RawMessage {
	var all []json.RawMessage
	for i := range answers {
		if raw, ok := mergedMember(&answers[i], "todos", "counts"); ok {
			all = append(all, raw)
		}
	}
	sum, ok := sumTodoCounts(all)
	if !ok {
		return nil
	}
	return map[string]json.RawMessage{"counts": sum}
}

// sumTodoCounts adds up the counts objects of all (each answer's; anything not an object, null included, is none):
// one as it was sent, else member by member over the raw JSON (sumMembers), so a member a newer manager adds, which this
// hub does not know, is passed on or added up too rather than dropped. False when none is an object.
func sumTodoCounts(all []json.RawMessage) (json.RawMessage, bool) {
	var objs []map[string]json.RawMessage
	var first json.RawMessage
	for _, raw := range all {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil || m == nil {
			continue
		}
		if first == nil {
			first = raw
		}
		objs = append(objs, m)
	}
	switch len(objs) {
	case 0:
		return nil, false
	case 1:
		return first, true
	}
	out, err := json.Marshal(sumMembers(objs))
	return out, err == nil
}

// sumMembers is objs added up member by member: a member that is an integer in the first object that has it is the sum
// of the integers the objects hold there; one that is an object, those objects added up so (sections, by key); any
// other, the first's as it is. The same objects in the same order give the same members.
func sumMembers(objs []map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, o := range objs {
		for k, v := range o {
			if _, done := out[k]; done {
				continue
			}
			v = bytes.TrimSpace(v)
			switch {
			case len(v) > 0 && v[0] == '{':
				var subs []map[string]json.RawMessage
				for _, o := range objs {
					var m map[string]json.RawMessage
					if json.Unmarshal(o[k], &m) == nil && m != nil {
						subs = append(subs, m)
					}
				}
				if b, err := json.Marshal(sumMembers(subs)); err == nil {
					v = b
				}
			default:
				if _, isInt := jsonInteger(v); isInt {
					var n int64
					for _, o := range objs {
						if i, ok := jsonInteger(o[k]); ok {
							n += i
						}
					}
					v = strconv.AppendInt(nil, n, 10)
				}
			}
			out[k] = v
		}
	}
	return out
}

// jsonInteger is raw as an integer, when it is one.
func jsonInteger(raw json.RawMessage) (int64, bool) {
	n, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	return n, err == nil
}

// mergedMember is the top-level member name of a, one answer of a list merged under key, as the merge reads it; false
// when a has none, or is no answer the merge takes (not 2xx, not a JSON object, its list not an array).
func mergedMember(a *listAnswer, key, name string) (json.RawMessage, bool) {
	if !a.ok() {
		return nil, false
	}
	s, use := a.scan(key)
	switch use {
	case scanMerge:
		for _, m := range s.members {
			if string(m.key) == name {
				return m.val, true
			}
		}
	case scanDecode: // as writeDecoded reads it
		var t map[string]json.RawMessage
		if json.Unmarshal(a.body, &t) != nil || t == nil {
			return nil, false
		}
		if raw, has := t[key]; has && json.Unmarshal(raw, new([]json.RawMessage)) != nil {
			return nil, false
		}
		v, has := t[name]
		return v, has
	}
	return nil, false
}
