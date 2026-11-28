package agy

import (
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
	"sort"
)

type PageParams struct {
	ItemID     string `json:"item_id"`
	History    string `json:"history"`
	SinceSeq   int    `json:"since_seq"`
	BeforeSeq  int    `json:"before_seq"`
	AfterSeq   *int   `json:"after_seq"`
	Limit      int    `json:"limit"`
	Turns      int    `json:"turns"`
	FromSeq    int    `json:"from_seq"`
	ThroughSeq int    `json:"through_seq"`
	UserInputs bool   `json:"user_inputs"`
	FoldWork   bool   `json:"fold_work"`
}

func LightItems(items []proto.Item) []proto.Item {
	out := make([]proto.Item, 0, len(items))
	for _, it := range items {
		out = append(out, transcript.LightItem(it))
	}
	return out
}

func agyIndex(items []proto.Item, seq int) int {
	return sort.Search(len(items), func(i int) bool { return items[i].Seq >= seq })
}

func AfterIndex(items []proto.Item, seq int) int {
	return sort.Search(len(items), func(i int) bool { return items[i].Seq > seq })
}

func (h Projection) After(since int) (items []proto.Item, span transcript.Span, more bool) {
	items = h.items[AfterIndex(h.items, since):]
	span = transcript.Span{History: h.token, From: since, To: max(since, h.seq)}
	if len(items) > 500 {
		// Keep every item from the same native step on the same page.
		end := AfterIndex(items, items[499].Seq)
		more = end < len(items)
		items = items[:end]
		if more {
			span.To = items[end-1].Seq
		}
	}
	return LightItems(items), span, more
}

func agyUserInput(it proto.Item) bool {
	return it.Role == "user" && (it.Kind == proto.KindText || (it.Kind == proto.KindImage && it.ToolUseID == ""))
}

func (h Projection) Page(p PageParams) any {
	if p.UserInputs {
		index := proto.UserInputIndex{History: h.token, Inputs: []proto.UserInputPosition{}}
		for _, it := range h.items {
			if agyUserInput(it) && (len(index.Inputs) == 0 || index.Inputs[len(index.Inputs)-1].Seq != it.Seq) {
				index.Inputs = append(index.Inputs, proto.UserInputPosition{ID: it.ID, Seq: it.Seq})
			}
		}
		index.Total = len(index.Inputs)
		return index
	}
	items := h.items
	if p.ThroughSeq > 0 {
		from, to := agyIndex(items, p.FromSeq), AfterIndex(items, p.ThroughSeq)
		items = items[min(from, to):to]
		if len(items) > 1000 {
			end := AfterIndex(items, items[999].Seq)
			return map[string]any{"items": LightItems(items[:end]), "more": end < len(items)}
		}
		return map[string]any{"items": LightItems(items), "more": false}
	}
	if p.AfterSeq != nil {
		page, span, more := h.After(*p.AfterSeq)
		return withSpan(map[string]any{"items": page, "has_more": false, "more_after": more}, span, true)
	}
	span := transcript.Span{History: h.token, To: h.seq}
	if p.BeforeSeq > 0 {
		items = items[:agyIndex(items, p.BeforeSeq)]
		span.To = 0
		if len(items) > 0 {
			span.To = items[len(items)-1].Seq
		}
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 80
	}
	start := max(0, len(items)-min(limit, 500))
	if p.Turns > 0 {
		turns := min(p.Turns, 10)
		start = max(0, len(items)-2000)
		lastSeq := -1
		for i := len(items) - 1; i >= start; i-- {
			if agyUserInput(items[i]) && items[i].Seq != lastSeq {
				lastSeq = items[i].Seq
				turns--
				if turns == 0 {
					start = i
					break
				}
			}
		}
	}
	if start > 0 {
		start = agyIndex(items, items[start].Seq)
	}
	items = LightItems(items[start:])
	if p.FoldWork {
		items = transcript.FoldWork(items)
	}
	return withSpan(map[string]any{"items": items, "has_more": start > 0}, span, false)
}

func withSpan(m map[string]any, sp transcript.Span, continues bool) map[string]any {
	if sp.History != "" {
		m["history"] = sp.History
	}
	m["to_seq"] = sp.To
	if continues {
		m["from_seq"] = sp.From
	}
	return m
}
