package subscriptionsui

import (
	"cmp"

	"github.com/sunfayao/persona/sdk/go/wire"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/subscriptions"
)

// pushSource is a push source's page.
func (p *pages) pushSource(s subscriptions.Subscription) wire.View {
	v := wire.View{Title: s.Label, State: state{P: pageSource, ID: s.ID}.String()}
	v.Sections = append(v.Sections, p.switches(s.ID, s.Label, s.Recipe.Kind, p.schedule(s), s.ToInbox, true, true))
	last := row("st.last", p.t("source.push_last"))
	last.Value = p.lastLine(s)
	runs := row("st.runs", p.t("source.runs"))
	runs.Action = open(state{P: pageRuns, ID: s.ID}, p.t("source.runs"))
	v.Sections = append(v.Sections, wire.Section{Key: "status", Title: p.t("source.status"), Items: []wire.Component{last, runs}})
	how := text("h.push", p.t("source.push_how", i18n.Args{"device": cmp.Or(s.SourceName, s.Label)}), "secondary")
	rules := row("h.rules", p.t("source.rules"))
	rules.Subtitle = cmp.Or(s.TriageRules, p.t("source.rules_default"))
	rules.Action = open(state{P: pageRules, ID: s.ID}, p.t("source.rules"))
	fb := row("h.fallback", p.t("source.fallback"))
	fb.Value = p.t("tier." + cmp.Or(s.FallbackTier, "default"))
	fb.Action = open(state{P: pageFallback, ID: s.ID}, p.t("source.fallback"))
	sec := wire.Section{Key: "how", Title: p.t("source.push_section"), Items: []wire.Component{how, rules, fb}}
	if p.env.Builtin != nil { // the apps its items came from that are muted (#5306)
		sec.Items = append(sec.Items, p.mutesRow(s.ID))
	}
	v.Sections = append(v.Sections, sec)
	del := button("b.delete", p.t("source.delete"), "delete")
	del.Role = "destructive"
	del.Action.Confirm = p.t("source.delete_confirm", i18n.Args{"source": s.Label})
	v.Sections = append(v.Sections, wire.Section{Key: "act", Items: []wire.Component{del}})
	return v
}
