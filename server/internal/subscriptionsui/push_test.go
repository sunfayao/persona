package subscriptionsui

import (
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/subscriptions"
)

func pushSub(f *fake, device string, on bool) subscriptions.Subscription {
	s := subscriptions.Subscription{ID: "sub_push", Connector: subscriptions.PushConnector, Source: device, SourceName: "Test iPhone",
		Label: "Shortcuts on Test iPhone", Icon: "bell", Recipe: subscriptions.Recipe{Mode: "items", Kind: "sub_notice", GroupBy: "thread"},
		RecipeOrigin: subscriptions.OriginPush, State: subscriptions.StateOff, Revision: 3}
	if on {
		s.ToInbox, s.State, s.LastOKAt, s.LastNewCount = true, subscriptions.StateOK, at(-5*time.Minute), 2
	}
	f.subs[s.ID] = s
	f.runs[s.ID] = []subscriptions.Run{{At: now.Add(-5 * time.Minute), Trigger: subscriptions.TriggerPush, Count: 2, New: 2}}
	return s
}

func TestAPushSourceHasNoRecipeIntervalOrPull(t *testing.T) {
	f := newFake()
	s := pushSub(f, "dev_phone", true)
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		v := draw(t, f, env(lang, true), state{P: pageSource, ID: s.ID})
		var got []string
		for _, sec := range v.Sections {
			got = append(got, sec.Key)
		}
		if strings.Join(got, ",") != "dest,status,how,act" {
			t.Fatalf("%s: sections %v", lang, got)
		}
		for _, k := range []string{"f.interval", "st.next", "h.recipe", "b.run"} {
			if item(v, k) != nil {
				t.Fatalf("%s: a push source shows %s", lang, k)
			}
		}
		if how := item(v, "h.push"); how == nil || !strings.Contains(how.Text, "Test iPhone") {
			t.Fatalf("%s: how it is sent: %+v", lang, how)
		}
	}
	en := draw(t, f, env(i18n.En, true), state{P: pageSource, ID: s.ID})
	in := item(en, "d.to_inbox")
	if in == nil || in.Control == nil || strings.Contains(in.Control.Action.Confirm, "Pulled") || !strings.Contains(in.Control.Action.Confirm, "shortcut") {
		t.Fatalf("the Inbox's consent: %+v", in)
	}
	if last := item(en, "st.last"); last == nil || last.Value != "5 minutes ago, 2 new" {
		t.Fatalf("last item: %+v", last)
	}
	runs := draw(t, f, env(i18n.En, true), state{P: pageRuns, ID: s.ID})
	if r := item(runs, "r.0"); r == nil || !strings.Contains(r.Title, "sent from Shortcuts") || strings.Contains(r.Subtitle, " s") {
		t.Fatalf("a push in the runs: %+v", r)
	}
	if sec := section(runs, "runs"); sec == nil || strings.Contains(sec.Footer, "rate-limited") || !strings.Contains(sec.Footer, "Shortcuts") {
		t.Fatalf("the runs' footer for a push source: %+v", sec)
	}
	// In the list it is a source like the others.
	list := draw(t, f, env(i18n.En, true), state{})
	if r := item(list, "s."+s.ID); r == nil || r.Icon != "bell" || !strings.Contains(r.Subtitle, "as a shortcut sends") {
		t.Fatalf("its row: %+v", r)
	}
}
