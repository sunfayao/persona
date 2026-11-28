package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/cronrule"
	"github.com/sunfayao/persona/server/internal/assistantwire"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// Cron tasks sit beside the other three in one list: checkTask tidies each to its own fields, and what was stored before
// reads as it did.
func TestCronTasksBesideTheOtherRules(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	cronTask := Task{Title: "巡检", Prompt: "看看", Repeat: repeatCron, Cron: "  */30   9-18 * * 1-5 ", Time: "09:00", Weekdays: []int{1},
		EveryMinutes: 30, At: now, Enabled: true}
	if err := checkTask(&cronTask, now); err != nil {
		t.Fatal(err)
	}
	if cronTask.Cron != "*/30 9-18 * * 1-5" || cronTask.Time != "" || cronTask.Weekdays != nil || cronTask.EveryMinutes != 0 || !cronTask.At.IsZero() {
		t.Fatalf("a cron task keeps only its expression: %+v", cronTask)
	}
	bad := Task{Title: "x", Prompt: "y", Repeat: repeatCron, Cron: "*/1 * * * *", Enabled: true}
	if err := checkTask(&bad, now); err == nil || !strings.Contains(err.Error(), "at most every 5 minutes") {
		t.Fatalf("a cron task too often is refused: %v", err)
	}
	none := Task{Title: "x", Prompt: "y", Repeat: repeatCron, Enabled: true}
	if err := checkTask(&none, now); err == nil {
		t.Fatal("a cron task needs an expression")
	}
	for _, other := range []Task{
		{Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Cron: "0 9 * * *", Enabled: true},
		{Title: "巡检", Prompt: "x", Repeat: repeatInterval, EveryMinutes: 30, Cron: "0 9 * * *", Enabled: true},
		{Prompt: "x", Repeat: repeatOnce, At: now.Add(time.Hour), Cron: "0 9 * * *", Enabled: true},
	} {
		if err := checkTask(&other, now); err != nil || other.Cron != "" {
			t.Fatalf("a %s task drops an expression: %+v %v", other.Repeat, other, err)
		}
	}
	// A list stored before cron tasks (no cron field) reads the same, and writes nothing new for them.
	stored := `[{"id":"morning","title":"早报","prompt":"x","repeat":"daily","time":"09:00","weekdays":[1,2,3,4,5],"enabled":true},` +
		`{"id":"p","title":"巡检","prompt":"y","repeat":"interval","every_minutes":30,"at":"2026-11-11T10:30:00Z","enabled":true}]`
	var tasks []Task
	if err := json.Unmarshal([]byte(stored), &tasks); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(tasks)
	if strings.Contains(string(again), `"cron`) || strings.Contains(string(again), `"next_runs"`) {
		t.Fatalf("old tasks gain no cron fields: %s", again)
	}
	tpe := mustZone(t, "Asia/Taipei")
	if n := tasks[0].next(now, tpe); !n.Equal(time.Date(2026, 11, 12, 9, 0, 0, 0, tpe)) {
		t.Fatalf("the daily task: %v", n)
	}
	if n := tasks[1].next(now, tpe); !n.Equal(time.Date(2026, 11, 11, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("the interval task: %v", n)
	}
	cronTask.ID = "c"
	if n := cronTask.next(now, tpe); !n.Equal(time.Date(2026, 11, 11, 18, 30, 0, 0, tpe)) { // 10:00 UTC is 18:00 in Taipei
		t.Fatalf("the cron task: %v", n)
	}
	// Changing the expression, or switching the task on, arms it as a daily one is armed.
	on := cronTask
	rearm(cronTask, &on, now)
	if !on.ArmedAt.IsZero() {
		t.Fatal("nothing changed: not rearmed")
	}
	on.Cron = "0 9 * * *"
	if rearm(cronTask, &on, now); on.ArmedAt.IsZero() {
		t.Fatal("another expression rearms it")
	}
}

// The scheduler runs a cron task at its time, in the user's time zone, once per time; a time missed while the hub was
// down runs if it is less than 30 minutes ago and is skipped and recorded otherwise.
func TestSchedulerRunsCronTasks(t *testing.T) {
	f := readyHub()
	f.settings = &Settings{Timezone: "Asia/Taipei"}
	h := newStorelessService(t, f)
	tpe := mustZone(t, "Asia/Taipei")
	at := func(d, hh, mm int) time.Time { return time.Date(2026, 11, d, hh, mm, 0, 0, tpe) }
	armed := at(11, 8, 0) // created Wednesday 08:00
	setTasks(h,
		Task{ID: "c", Title: "巡检", Prompt: "看看部署", Repeat: repeatCron, Cron: "*/30 9-18 * * 1-5", Enabled: true, ArmedAt: armed},
		Task{ID: "m", Title: "月报", Prompt: "写月报", Repeat: repeatCron, Cron: "0 10 1 * *", Enabled: true, ArmedAt: armed},
		Task{ID: "r", Title: "早报", Prompt: "早报", Repeat: repeatDaily, Time: "09:00", Enabled: true, ArmedAt: armed})

	if next := h.nextDue(context.Background(), at(11, 8, 10)); !next.Equal(at(11, 9, 0)) {
		t.Fatalf("the scheduler wakes for 09:00: %v", next)
	}
	h.runDue(context.Background(), at(11, 8, 59))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.IsZero() {
		t.Fatalf("not yet: %+v", c)
	}
	// 09:00:20: both the cron task and the daily one run; the cron task's run is due 09:00.
	nine := at(11, 9, 0).Add(20 * time.Second)
	h.runDue(context.Background(), nine)
	h.sched.inflight.Wait()
	runs := waitRuns(t, h, 2)
	var cronRun *ScheduleRun
	for i := range runs {
		if runs[i].RefID == "c" {
			cronRun = &runs[i]
		}
	}
	if cronRun == nil || !cronRun.Due.Equal(at(11, 9, 0)) || cronRun.Status != runOK || cronRun.Kind != "routine" {
		t.Fatalf("the cron task ran at 09:00: %+v", runs)
	}
	sentPrompt := false
	f.mu.Lock()
	for _, p := range f.sends {
		if text, _ := p["text"].(string); strings.Contains(text, "看看部署") && strings.Contains(text, "⏰ 巡检 (scheduled task") {
			sentPrompt = true
		}
	}
	f.mu.Unlock()
	if !sentPrompt {
		t.Fatal("its run sends its prompt as a scheduled task's")
	}
	// Not again in the same half hour; again at 09:30.
	h.runDue(context.Background(), at(11, 9, 10))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(nine) {
		t.Fatalf("one run per time: %+v", c)
	}
	h.runDue(context.Background(), at(11, 9, 30))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(11, 9, 30)) {
		t.Fatalf("09:30 runs: %+v", c)
	}
	// Down from 09:40 to 10:20: 10:00 is missed, the latest time (10:00) is less than 30 minutes ago and runs once now.
	h.runDue(context.Background(), at(11, 10, 20))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(11, 10, 20)) || c.LastStatus == runSkipped {
		t.Fatalf("a time missed by 20 minutes runs late: %+v", c)
	}
	// Down from 10:20 to 12:45 on a task whose last time is long past: 12:30 is 15 minutes ago and runs.
	h.runDue(context.Background(), at(11, 12, 45))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(11, 12, 45)) || c.LastStatus == runSkipped {
		t.Fatalf("the latest missed time runs: %+v", c)
	}
	// Saturday is not a weekday.
	sc := h.loadSchedule(context.Background())
	sc.mu.Lock()
	sc.tasks[0].LastRun = at(13, 18, 30)
	sc.mu.Unlock()
	h.runDue(context.Background(), at(14, 9, 0))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(13, 18, 30)) {
		t.Fatalf("no run on Saturday: %+v", c)
	}
	// Switched off, it does not run on Monday either.
	sc.mu.Lock()
	sc.tasks[0].Enabled, sc.tasks[2].Enabled = false, false
	sc.tasks[1].LastRun = time.Date(2026, 11, 1, 10, 0, 5, 0, tpe) // the monthly one ran last month
	sc.mu.Unlock()
	h.runDue(context.Background(), at(16, 9, 0))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(13, 18, 30)) {
		t.Fatalf("off: %+v", c)
	}
	// The monthly one, a month after its last run, waits for the 1st; down over 10:00 until 11:00 it is skipped and
	// recorded, not sent an hour late, and only once.
	h.runDue(context.Background(), at(30, 23, 0))
	h.sched.inflight.Wait()
	if m, _ := taskByID(h, "m"); m.LastRun.Day() != 1 {
		t.Fatalf("not before the 1st: %+v", m)
	}
	dec := time.Date(2026, 12, 1, 11, 0, 0, 0, tpe)
	h.runDue(context.Background(), dec)
	h.sched.inflight.Wait()
	if m, _ := taskByID(h, "m"); !m.LastRun.Equal(dec) || m.LastStatus != runSkipped {
		t.Fatalf("the monthly run an hour late is skipped: %+v", m)
	}
	h.runDue(context.Background(), dec.Add(time.Minute))
	h.sched.inflight.Wait()
	runs = waitRuns(t, h, 1)
	if last := runs[len(runs)-1]; last.RefID != "m" || last.Status != runSkipped || !last.Due.Equal(time.Date(2026, 12, 1, 10, 0, 0, 0, tpe)) {
		t.Fatalf("one skip, due 10:00: %+v", last)
	}
	skips := 0
	for _, r := range runs {
		if r.RefID == "m" {
			skips++
		}
	}
	if skips != 1 {
		t.Fatalf("skipped once: %+v", runs)
	}
}

func TestCronTaskRoutes(t *testing.T) {
	f := newFakeHub()
	f.settings = &Settings{Timezone: "Asia/Taipei"}
	h := newStorelessService(t, f)
	setTasks(h, Task{ID: "morning", Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Enabled: true})
	type answer struct {
		Code int
		Body *bytes.Buffer
	}
	// As the hub relays an app's request (bus.go serveHTTP): the errors read in its Accept-Language.
	in := func(lang, method, target, body string) answer {
		path, query, _ := strings.Cut(target, "?")
		res, err := serveHTTP(context.Background(), h, assistantwire.HTTPRequest{Method: method, Path: path, Query: query,
			Header: map[string][]string{"Accept-Language": {lang}}, Body: []byte(body)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return answer{res.Status, bytes.NewBuffer(res.Body)}
	}
	do := func(method, target, body string) answer { return in("en", method, target, body) }
	rec := do("POST", "/api/v1/assistant/tasks", `{"title":"巡检","prompt":"看看部署","repeat":"cron","cron":"*/30 9-18 * * 1-5","enabled":true}`)
	var created struct{ Task Task }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	c := created.Task
	if c.Repeat != repeatCron || c.Cron != "*/30 9-18 * * 1-5" || len(c.NextRuns) != cronrule.Runs || !c.NextRun.Equal(c.NextRuns[0]) ||
		c.CronText != "Every 30 minutes, between 09:00 and 18:59, Monday through Friday" {
		t.Fatalf("created: %+v", c)
	}
	tpe := mustZone(t, "Asia/Taipei")
	for _, at := range c.NextRuns {
		if l := at.In(tpe); l.Minute()%30 != 0 || l.Hour() < 9 || l.Hour() > 18 || l.Weekday() == time.Saturday || l.Weekday() == time.Sunday {
			t.Fatalf("a next run off the rule: %v", l)
		}
	}
	if stored, _ := taskByID(h, c.ID); stored.CronText != "" || stored.NextRuns != nil || stored.ArmedAt.IsZero() {
		t.Fatalf("words and next runs are the view's, the task is armed: %+v", stored)
	}
	// A bad expression: 400, saying what is wrong, in the app's language.
	rec = do("POST", "/api/v1/assistant/tasks", `{"title":"x","prompt":"y","repeat":"cron","cron":"0 25 * * *","enabled":true}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `the hour (0–23) can’t be \"25\"`) {
		t.Fatalf("bad: %d %s", rec.Code, rec.Body)
	}
	// An app that does not know cron tasks (an older iOS app sends it back as a one-off without an expression) keeps the rule.
	rec = do("PUT", "/api/v1/assistant/tasks/"+c.ID, `{"title":"巡检","prompt":"看看部署和 CI","repeat":"once","time":"09:00","weekdays":[],"every_minutes":0,"enabled":false}`)
	if rec.Code != 200 {
		t.Fatalf("old app's put: %d %s", rec.Code, rec.Body)
	}
	if stored, _ := taskByID(h, c.ID); stored.Repeat != repeatCron || stored.Cron != "*/30 9-18 * * 1-5" || stored.Enabled || stored.Prompt != "看看部署和 CI" {
		t.Fatalf("the rule stays, the rest changes: %+v", stored)
	}
	// One that does changes it, back to daily as well.
	rec = do("PUT", "/api/v1/assistant/tasks/"+c.ID, `{"title":"巡检","prompt":"y","repeat":"cron","cron":"@hourly","enabled":true}`)
	if stored, _ := taskByID(h, c.ID); rec.Code != 200 || stored.Cron != "@hourly" || !stored.Enabled {
		t.Fatalf("new expression: %d %s", rec.Code, rec.Body)
	}
	rec = do("PUT", "/api/v1/assistant/tasks/"+c.ID, `{"title":"巡检","prompt":"y","repeat":"daily","time":"08:30","cron":"","enabled":true}`)
	if stored, _ := taskByID(h, c.ID); rec.Code != 200 || stored.Repeat != repeatDaily || stored.Cron != "" || stored.Time != "08:30" {
		t.Fatalf("back to daily: %d %+v", rec.Code, stored)
	}
	// The list has the other tasks as they were.
	rec = do("GET", "/api/v1/assistant/tasks", "")
	if !strings.Contains(rec.Body.String(), `"id":"morning"`) || strings.Contains(rec.Body.String(), `"cron"`) {
		t.Fatalf("list: %s", rec.Body)
	}

	// The preview: words, the time zone and the next runs; or what is wrong.
	rec = do("GET", "/api/v1/assistant/tasks/cron?cron="+url.QueryEscape("0 10 1 * *"), "")
	var preview struct {
		Cron, Text, Timezone string
		NextRuns             []time.Time `json:"next_runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || rec.Code != 200 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if preview.Cron != "0 10 1 * *" || preview.Text != "At 10:00, on day 1 of the month" || preview.Timezone != "Asia/Taipei" || len(preview.NextRuns) != cronrule.Runs {
		t.Fatalf("preview: %+v", preview)
	}
	for _, at := range preview.NextRuns {
		if l := at.In(tpe); l.Day() != 1 || l.Hour() != 10 || l.Minute() != 0 {
			t.Fatalf("a preview run off the rule: %v", l)
		}
	}
	rec = do("GET", "/api/v1/assistant/tasks/cron?cron="+url.QueryEscape("*/2 * * * *"), "")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "at most every 5 minutes") {
		t.Fatalf("preview of a bad one: %d %s", rec.Code, rec.Body)
	}
	rec = in("zh-Hans", "GET", "/api/v1/assistant/tasks/cron?cron="+url.QueryEscape("0 9 * *"), "")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "cron 表达式是 5 段") {
		t.Fatalf("in Chinese: %d %s", rec.Code, rec.Body)
	}
}

func TestCronInTheTools(t *testing.T) {
	f := newFakeHub()
	f.settings = &Settings{Timezone: "Asia/Taipei"}
	h := newStorelessService(t, f)
	ctx := context.Background()
	out, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"title":"月报","prompt":"写月报","cron":"0 10 1 * *"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cron `0 10 1 * *` (At 10:00, on day 1 of the month)", "Asia/Taipei (UTC+08:00)", "next runs 12-01 10:00 Tue, 01-01 10:00 Fri"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the reply says %q: %s", want, out)
		}
	}
	sc := h.loadSchedule(ctx)
	r := sc.tasks[len(sc.tasks)-1]
	if r.Repeat != repeatCron || r.Cron != "0 10 1 * *" || r.CreatedBy != "assistant" || r.ArmedAt.IsZero() {
		t.Fatalf("routine: %+v", r)
	}
	if !strings.Contains(h.describeSchedule(ctx, ""), "cron `0 10 1 * *` (At 10:00, on day 1 of the month), next ") {
		t.Fatalf("list_followups: %s", h.describeSchedule(ctx, ""))
	}
	// Back to a time of day by id; a bad expression changes nothing and says why.
	if _, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"id":"`+r.ID+`","time":"09:00"}`)); err != nil {
		t.Fatal(err)
	}
	if r2, _ := taskByID(h, r.ID); r2.Repeat != repeatDaily || r2.Cron != "" || r2.Time != "09:00" {
		t.Fatalf("back to daily: %+v", r2)
	}
	if _, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"id":"`+r.ID+`","cron":"0 9 31 2 *"}`)); err == nil || !strings.Contains(err.Error(), "never runs") {
		t.Fatalf("never: %v", err)
	}
	if r2, _ := taskByID(h, r.ID); r2.Repeat != repeatDaily {
		t.Fatalf("a refused change is not saved: %+v", r2)
	}
	if _, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"title":"x","prompt":"y","cron":"0 9 * * *","time":"09:00"}`)); err == nil {
		t.Fatal("cron and time together are refused")
	}

	// A repeating follow-up on cron, into this conversation.
	out, err = h.toolScheduleFollowup(ctx, "server", "s1", json.RawMessage(`{"prompt":"看看 CI","title":"CI","cron":"*/30 9-18 * * 1-5"}`))
	if err != nil || !strings.Contains(out, "on cron `*/30 9-18 * * 1-5`") || !strings.Contains(out, "It repeats by itself") {
		t.Fatalf("follow-up: %q %v", out, err)
	}
	fu := sc.tasks[len(sc.tasks)-1]
	if fu.Repeat != repeatCron || fu.SessionID != "s1" || !fu.At.IsZero() || fu.EveryMinutes != 0 || fu.ArmedAt.IsZero() {
		t.Fatalf("follow-up task: %+v", fu)
	}
	for _, bad := range []string{`{"prompt":"x","cron":"0 9 * * *","every":"30m"}`, `{"prompt":"x","cron":"0 9 * * *","at":"2h"}`, `{"prompt":"x","cron":"*/1 * * * *"}`} {
		if _, err := h.toolScheduleFollowup(ctx, "server", "s1", json.RawMessage(bad)); err == nil {
			t.Fatalf("refused: %s", bad)
		}
	}
	// The snapshot lists it with the routines, by its rule.
	_, _, routines, _, _, _ := h.snapshotSchedule(ctx, mustZone(t, "Asia/Taipei"), time.Now())
	found := false
	for _, rt := range routines {
		found = found || strings.HasPrefix(rt.Rule, "cron `*/30 9-18 * * 1-5`")
	}
	if !found {
		t.Fatalf("snapshot: %+v", routines)
	}
}
