package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestBriefTaskWaitUsesLatestAgentActivity(t *testing.T) {
	s := newStorelessService(t, newFakeHub().withMachine())
	now := time.Now().UTC()
	agents := []proto.Session{
		{SessionID: "old", State: proto.StateWorking, UpdatedAt: now.Add(-3 * time.Hour), Activity: "old stopped tool"},
		{SessionID: "current", State: proto.StateIdle, UpdatedAt: now.Add(-30 * time.Minute), Activity: "recent answer"},
	}
	if w, stopped := s.briefTaskWait(context.Background(), Todo{ID: "todo-live", Title: "Still active"}, agents, nil, nil,
		now, defaultBriefingWaits(), nil, nil, nil); stopped {
		t.Fatalf("an old agent hid the newer agent's activity: %+v", w)
	}
	agents[1].UpdatedAt = now.Add(-2 * time.Hour)
	w, stopped := s.briefTaskWait(context.Background(), Todo{ID: "todo-stopped", Title: "Both stopped"}, agents, nil, nil,
		now, defaultBriefingWaits(), nil, nil, nil)
	if !stopped || w.Kind != "stalled" || !w.Since.Equal(agents[1].UpdatedAt) || w.Reason != "recent answer" {
		t.Fatalf("the latest stopped session did not supply the wait row: %+v, stopped %v", w, stopped)
	}
}

// A task-linked ordinary card keeps the task's name, not a stale title denormalized onto the card. A Private card keeps
// neither identity nor question text. This calls the collector directly so withTask cannot repair a bad fixture first.
func TestBriefTaskCardWaitIdentityAndPrivacy(t *testing.T) {
	s := newStorelessService(t, newFakeHub().withMachine())
	now := time.Now().UTC()
	todo := Todo{ID: "todo-real", Title: "The real task title"}
	for _, tc := range []struct {
		name    string
		private bool
		kind    string
	}{
		{name: "question", kind: "question"},
		{name: "permission", kind: "permission"},
		{name: "private", kind: "private", private: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ix := proto.Interaction{ID: "ix-card", MachineID: "server", SessionID: "s-card", Kind: proto.IxQuestion,
				TaskTitle: "stale card title", Questions: []proto.Question{{Question: "secret question"}}, CreatedAt: now.Add(-time.Hour), Private: tc.private}
			if tc.kind == "permission" {
				ix.Kind = proto.IxPermission
			}
			w, stopped := s.briefTaskWait(context.Background(), todo, nil, []proto.Interaction{ix}, nil, now, defaultBriefingWaits(), nil, nil, nil)
			if !stopped || w.Kind != tc.kind || w.InteractionID != ix.ID {
				t.Fatalf("wait = %+v, stopped %v", w, stopped)
			}
			if tc.private {
				if w.TodoID != "" || w.Title == todo.Title || w.Reason != "" || strings.Contains(w.Title, "secret") {
					t.Fatalf("private card kept content: %+v", w)
				}
			} else if w.TodoID != todo.ID || w.Title != todo.Title {
				t.Fatalf("ordinary card lost its task identity: %+v", w)
			}
		})
	}
}

// max_rows bounds only the non-owner tail, in main as well as every leader. Every owner-wait row remains, even when
// owner rows alone exceed the configured limit; the folded rows become one direction-level "N more" count.
func TestFoldBriefDirectionKeepsEveryOwnerWait(t *testing.T) {
	now := time.Now().UTC()
	for _, ownerDirection := range []bool{false, true} {
		t.Run(fmt.Sprintf("owner_direction_%v", ownerDirection), func(t *testing.T) {
			d := prompts.BriefDirection{Owner: ownerDirection}
			for i := 0; i < 20; i++ {
				d.Waits = append(d.Waits, prompts.BriefWait{TodoID: fmt.Sprintf("owner-%02d", i), Kind: "needs_user", Since: now.Add(time.Duration(i) * time.Minute)})
			}
			for i := 0; i < 17; i++ {
				d.Waits = append(d.Waits, prompts.BriefWait{TodoID: fmt.Sprintf("other-%02d", i), Kind: "stalled", Since: now.Add(time.Duration(i) * time.Minute)})
			}
			foldBriefDirection(&d, defaultBriefingWaits().MaxRows)
			if len(d.Waits) != 35 || d.More != 2 {
				t.Fatalf("kept %d rows, more %d; want 35 and 2", len(d.Waits), d.More)
			}
			for i := 0; i < 20; i++ {
				if !briefOwnerWait(d.Waits[i].Kind) {
					t.Fatalf("owner row %d was folded or sorted behind another wait: %+v", i, d.Waits[i])
				}
			}
		})
	}
}

func TestBriefingWaitsSettingUsesSupervisionDefaultsAndApprovalChange(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, newFakeHub().withMachine())
	defaults := defaultBriefingWaits()
	if defaults.StalledMinutes != int(superviseStallMin/time.Minute) || defaults.SilentMinutes != int(superviseSilentMin/time.Minute) ||
		defaults.OrphanMinutes != int(orphanOfflineAfter/time.Minute) || defaults.CardMinutes != int(superviseNeedsAfter/time.Minute) || defaults.MaxRows != 15 {
		t.Fatalf("defaults no longer come from supervision: %+v", defaults)
	}
	var registered *ownSetting
	for i := range s.ownSettings() {
		o := s.ownSettings()[i]
		if o.Key == "assistant.briefing_waits" {
			registered = &o
			break
		}
	}
	if registered == nil || settingTitles["assistant.briefing_waits"] != "title_briefing_waits" {
		t.Fatal("assistant.briefing_waits is not registered in persona_settings")
	}
	for _, source := range []string{"superviseStallMin", "superviseSilentMin", "orphanOfflineAfter", "superviseNeedsAfter"} {
		if !strings.Contains(registered.About, source) {
			t.Errorf("setting documentation does not name %s: %s", source, registered.About)
		}
	}
	got, err := registered.get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BriefingWaits
	if err := json.Unmarshal([]byte(got), &decoded); err != nil || decoded != defaults {
		t.Fatalf("get = %s (%v), want %+v", got, err, defaults)
	}
	for _, bad := range []string{`{}`, `{"stalled_minutes":20,"silent_minutes":120,"orphan_minutes":10,"card_minutes":3,"max_rows":0}`,
		`{"stalled_minutes":0,"silent_minutes":120,"orphan_minutes":10,"card_minutes":3,"max_rows":15}`,
		`{"stalled_minutes":20,"silent_minutes":120,"orphan_minutes":10,"card_minutes":3,"max_rows":101}`, `"15"`} {
		if _, err := s.setBriefingWaits(ctx, json.RawMessage(bad)); err == nil {
			t.Errorf("accepted invalid value %s", bad)
		}
	}
	custom := BriefingWaits{StalledMinutes: 25, SilentMinutes: 150, OrphanMinutes: 12, CardMinutes: 4, MaxRows: 7}
	raw, _ := json.Marshal(custom)
	change, err := s.setBriefingWaits(ctx, raw)
	if err != nil || change == nil || change.apply == nil || change.cardText == nil {
		t.Fatalf("set did not prepare an approval-backed change: %+v, %v", change, err)
	}
	if card := change.card(i18n.ZhHans); !strings.Contains(card, "25m") || !strings.Contains(card, "max_rows=7") {
		t.Fatalf("approval card omits before/after values: %s", card)
	}
	if _, err := change.apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := briefingWaitsOf(s.settings(ctx)); got != custom {
		t.Fatalf("applied = %+v, want %+v", got, custom)
	}
	if again, err := s.setBriefingWaits(ctx, raw); err != nil || again.apply != nil {
		t.Fatalf("unchanged value prepared another approval: %+v, %v", again, err)
	}
	reset, err := s.setBriefingWaits(ctx, json.RawMessage(`null`))
	if err != nil || reset.apply == nil {
		t.Fatalf("null did not prepare a defaults reset: %+v, %v", reset, err)
	}
	if _, err := reset.apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := briefingWaitsOf(s.settings(ctx)); got != defaults || s.settings(ctx).BriefingWaits != nil {
		t.Fatalf("null reset = %+v, saved pointer %+v", got, s.settings(ctx).BriefingWaits)
	}
}

// Disabled routines retain the actual transition time. A legacy disabled routine has no such record: its line says so,
// even when LastRun is much newer, and one-off reminders are not mislabeled as daily reports or patrols.
func TestBriefDisabledRoutinesUseDisableTimeNotLastRun(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	s := newTestService(t, newFakeHub().withMachine())
	disabledReport, disabledPatrol := now.Add(-2*time.Hour), now.Add(-3*time.Hour)
	setTasks(s,
		Task{ID: "morning-off", Title: "Morning report", Repeat: repeatDaily, Time: "09:00", Facts: true, SessionID: "main",
			RunMachineID: "server", RunSessionID: "main", DisabledAt: disabledReport, LastRun: now.Add(-10 * time.Minute)},
		Task{ID: "legacy-off", Title: "Legacy evening report", Repeat: repeatDaily, Time: "21:00", Report: true, SessionID: "main",
			RunMachineID: "server", RunSessionID: "main", LastRun: now.Add(-5 * time.Minute)},
		Task{ID: "patrol-off", Title: "Deploy patrol", Repeat: repeatInterval, EveryMinutes: 30, SessionID: "lead",
			RunMachineID: "server", RunSessionID: "lead", DisabledAt: disabledPatrol},
		Task{ID: "cron-off", Title: "Quota patrol", Repeat: repeatCron, Cron: "*/30 * * * *", SessionID: "lead", DisabledAt: disabledPatrol},
		Task{ID: "once-off", Title: "One-off reminder", Repeat: repeatOnce, DisabledAt: disabledReport},
		Task{ID: "ordinary-daily-off", Title: "Daily reminder", Repeat: repeatDaily, Time: "12:00", DisabledAt: disabledReport},
		Task{ID: "enabled", Title: "Enabled patrol", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
	got := s.briefDisabledRoutines(ctx, now, i18n.ZhHans, map[string]string{"main": "主对话", "lead": "平台"})
	if len(got) != 4 {
		t.Fatalf("disabled routines = %+v", got)
	}
	byID := map[string]prompts.BriefDisabledRoutine{}
	for _, r := range got {
		byID[r.ID] = r
		if !strings.Contains(r.Line, r.Title) || !strings.Contains(r.Line, r.Direction) {
			t.Errorf("incomplete disabled row: %+v", r)
		}
	}
	if r := byID["morning-off"]; r.Kind != "report" || r.DisabledAt == nil || !r.DisabledAt.Equal(disabledReport) || !strings.Contains(r.Line, "2 小时") {
		t.Errorf("recorded disable duration = %+v", r)
	}
	if r := byID["legacy-off"]; r.DisabledAt != nil || !strings.Contains(r.Line, "关闭时间未记录") || strings.Contains(r.Line, "5 分钟") {
		t.Errorf("legacy row substituted LastRun or omitted the unknown marker: %+v", r)
	}
	if r := byID["patrol-off"]; r.Kind != "patrol" || r.DisabledAt == nil || !r.DisabledAt.Equal(disabledPatrol) {
		t.Errorf("patrol = %+v", r)
	}
}

func TestRoutineDisabledAtTracksTransitionsAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		old  Task
		next Task
		want time.Time
	}{
		{name: "created disabled", old: Task{}, next: Task{ID: "r"}, want: now},
		{name: "POST created disabled", old: Task{ID: "r", CreatedAt: now}, next: Task{ID: "r", CreatedAt: now}, want: now},
		{name: "enabled to disabled", old: Task{ID: "r", Enabled: true}, next: Task{ID: "r"}, want: now},
		{name: "legacy stays unknown", old: Task{ID: "r", CreatedAt: now.Add(-24 * time.Hour)}, next: Task{ID: "r", CreatedAt: now.Add(-24 * time.Hour)}},
		{name: "reenable clears", old: Task{ID: "r", DisabledAt: now.Add(-time.Hour)}, next: Task{ID: "r", Enabled: true, DisabledAt: now.Add(-time.Hour)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rearm(tc.old, &tc.next, now)
			if !tc.next.DisabledAt.Equal(tc.want) {
				t.Fatalf("DisabledAt = %s, want %s", tc.next.DisabledAt, tc.want)
			}
		})
	}

	s := newStorelessService(t, newFakeHub().withMachine())
	setTasks(s, Task{ID: "expiring", Title: "Temporary patrol", Prompt: "check", Repeat: repeatInterval, EveryMinutes: 30,
		Enabled: true, At: now.Add(-time.Hour), Until: now.Add(-time.Minute)})
	s.runDue(context.Background(), now)
	s.sched.inflight.Wait()
	got, ok := taskByID(s, "expiring")
	if !ok || got.Enabled || !got.DisabledAt.Equal(now) {
		t.Fatalf("Until expiry did not record its disable time: %+v", got)
	}
}
