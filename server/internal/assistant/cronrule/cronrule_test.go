package cronrule

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func mustCron(t *testing.T, expr string) Rule {
	t.Helper()
	r, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return r
}

// runsAfter are the n runs of expr after `after`, as the wall clock in loc shows them ("2026-11-12 09:00 Thu").
func runsAfter(t *testing.T, expr string, after time.Time, loc *time.Location, n int) []string {
	t.Helper()
	var out []string
	for _, at := range mustCron(t, expr).NextRuns(after, loc, n) {
		out = append(out, at.In(loc).Format("2006-01-02 15:04 Mon"))
	}
	return out
}

func sameRuns(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("%s:\n got  %q\n want %q", what, got, want)
	}
}

func TestParseCron(t *testing.T) {
	for _, c := range []struct{ in, expr, fields string }{
		{"*/30 9-18 * * 1-5", "*/30 9-18 * * 1-5", "*/30 9-18 * * 1-5"},
		{"  0   10 1 * *  ", "0 10 1 * *", "0 10 1 * *"},
		{"0 9 * * 7", "0 9 * * 7", "0 9 * * 0"},                   // 7 is Sunday too
		{"0 9 * * 5-7", "0 9 * * 5-7", "0 9 * * 5-6,0"},           // as is a range up to it
		{"0 9 * * MON-FRI", "0 9 * * MON-FRI", "0 9 * * MON-FRI"}, // names
		{"0 9 * JAN,JUL *", "0 9 * JAN,JUL *", "0 9 * JAN,JUL *"},
		{"@daily", "@daily", "0 0 * * *"},
		{"@HOURLY", "@HOURLY", "0 * * * *"},
		{"@weekly", "@weekly", "0 0 * * 0"},
		{"@monthly", "@monthly", "0 0 1 * *"},
		{"@yearly", "@yearly", "0 0 1 1 *"},
		{"@annually", "@annually", "0 0 1 1 *"},
		{"@midnight", "@midnight", "0 0 * * *"},
		{"*/5 * * * *", "*/5 * * * *", "*/5 * * * *"}, // the shortest gap allowed
		{"0,55 * * * *", "0,55 * * * *", "0,55 * * * *"},
	} {
		r, err := Parse(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if r.expr != c.expr || strings.Join(r.fields[:], " ") != c.fields {
			t.Fatalf("%q: stored %q, read as %q", c.in, r.expr, strings.Join(r.fields[:], " "))
		}
	}
}

func TestParseCronRefusesWithAReason(t *testing.T) {
	for _, c := range []struct{ in, key, says string }{
		{"", "cron_empty", "0 9 * * 1-5"},
		{"   ", "cron_empty", "minute hour day-of-month month day-of-week"},
		{"0 9 * *", "cron_fields", "has 4 fields"},
		{"0 0 9 * * *", "cron_fields", "seconds aren’t supported"},
		{"61 * * * *", "cron_field", `the minute (0–59) can’t be "61"`},
		{"0 24 * * *", "cron_field", `the hour (0–23) can’t be "24"`},
		{"0 9 32 * *", "cron_field", `day of month (1–31) can’t be "32"`},
		{"0 9 * 13 *", "cron_field", `month (1–12 or JAN–DEC) can’t be "13"`},
		{"0 9 * * 8", "cron_field", `day of week (0–7 or SUN–SAT, 0 and 7 are Sunday) can’t be "8"`},
		{"0 9 * * FUN", "cron_field", `can’t be "FUN"`},
		{"x 9 * * *", "cron_field", `the minute (0–59) can’t be "x"`},
		{"0 18-9 * * *", "cron_field", `the hour (0–23) can’t be "18-9"`},
		{"*/3 * * * *", "cron_often", "runs 3 minutes apart"},
		{"0-4 * * * *", "cron_often", "runs 1 minutes apart"},
		{"0,58 9,10 * * *", "cron_often", "runs 2 minutes apart"}, // 9:58, then 10:00
		{"@every 5m", "cron_every", "use an interval"},
		{"@reboot", "cron_alias", "isn’t a cron alias"},
		{"@daily 9", "cron_alias", "isn’t a cron alias"},
		{"TZ=Asia/Tokyo 0 9 * * *", "cron_zone", "run in your time zone"},
		{"CRON_TZ=UTC 0 9 * * *", "cron_zone", "Leave out TZ="},
		{"0 0 30 2 *", "cron_never", "never runs"},
	} {
		_, err := Parse(c.in)
		var m *i18n.Message
		if !errors.As(err, &m) || m.Key != "assistant-schedule."+c.key {
			t.Fatalf("%q: want %s, got %v", c.in, c.key, err)
		}
		if !strings.Contains(err.Error(), c.says) { // the tools' English
			t.Fatalf("%q: %q does not say %q", c.in, err.Error(), c.says)
		}
		for _, l := range []i18n.Lang{i18n.ZhHans, i18n.Ja} { // and the apps' languages
			if text := m.Text(l); text == "" || text == m.Key || strings.Contains(text, "{") {
				t.Fatalf("%q in %s: %q", c.in, l, text)
			}
		}
	}
}

// The user's examples, on Taipei's wall clock: every 30 minutes 9:00–18:30 on weekdays, and 10:00 on the 1st.
func TestCronNextInTheUsersTimeZone(t *testing.T) {
	tpe := mustZone(t, "Asia/Taipei")
	wed := time.Date(2026, 11, 11, 18, 10, 0, 0, tpe)
	sameRuns(t, "weekdays 9–18", runsAfter(t, "*/30 9-18 * * 1-5", wed, tpe, 4),
		[]string{"2026-11-11 18:30 Wed", "2026-11-12 09:00 Thu", "2026-11-12 09:30 Thu", "2026-11-12 10:00 Thu"})
	fri := time.Date(2026, 11, 13, 18, 30, 0, 0, tpe) // exactly at a run: the next one is strictly after
	sameRuns(t, "over the weekend", runsAfter(t, "*/30 9-18 * * 1-5", fri, tpe, 1), []string{"2026-11-16 09:00 Mon"})
	sameRuns(t, "the 1st", runsAfter(t, "0 10 1 * *", wed, tpe, 3),
		[]string{"2026-12-01 10:00 Tue", "2027-01-01 10:00 Fri", "2027-02-01 10:00 Mon"})

	// The same expression in another zone is another moment: it is read on the user's clock, not the hub's.
	at := mustCron(t, "0 9 * * *").Next(wed, tpe)
	if want := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC); !at.Equal(want) {
		t.Fatalf("09:00 in Taipei is 01:00 UTC: %v", at)
	}
	if ny := mustCron(t, "0 9 * * *").Next(wed, mustZone(t, "America/New_York")); !ny.Equal(time.Date(2026, 11, 11, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("09:00 in New York: %v", ny)
	}
}

func TestCronNextOverMonthEnds(t *testing.T) {
	tpe := mustZone(t, "Asia/Taipei")
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, tpe)
	// The 31st skips the months without one.
	sameRuns(t, "the 31st", runsAfter(t, "0 10 31 * *", nov, tpe, 4),
		[]string{"2026-12-31 10:00 Thu", "2027-01-31 10:00 Sun", "2027-03-31 10:00 Wed", "2027-05-31 10:00 Mon"})
	// From the last minute of a month to the first day of the next, and of the year.
	sameRuns(t, "the 1st at midnight", runsAfter(t, "@monthly", time.Date(2026, 12, 31, 23, 59, 30, 0, tpe), tpe, 2),
		[]string{"2027-01-01 00:00 Fri", "2027-02-01 00:00 Mon"})
	// February 29th waits for a leap year.
	sameRuns(t, "leap day", runsAfter(t, "0 9 29 2 *", nov, tpe, 2), []string{"2028-02-29 09:00 Tue", "2032-02-29 09:00 Sun"})
	// 28–31: every day at the end of a month, however long it is.
	sameRuns(t, "month ends", runsAfter(t, "0 9 28-31 2 *", time.Date(2027, 2, 1, 0, 0, 0, 0, tpe), tpe, 2),
		[]string{"2027-02-28 09:00 Sun", "2028-02-28 09:00 Mon"})
}

// Day of month and day of week, as Vixie cron reads them: both restricted, either one; one starting with *, both.
func TestCronDayOfMonthOrDayOfWeek(t *testing.T) {
	tpe := mustZone(t, "Asia/Taipei")
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, tpe) // a Sunday
	sameRuns(t, "the 13th or a Friday", runsAfter(t, "0 9 13 * 5", nov, tpe, 6), []string{
		"2026-11-06 09:00 Fri", "2026-11-13 09:00 Fri", "2026-11-20 09:00 Fri", "2026-11-27 09:00 Fri",
		"2026-12-04 09:00 Fri", "2026-12-11 09:00 Fri"})
	sameRuns(t, "the 13th or a Friday, into the next month", runsAfter(t, "0 9 13 * 5", time.Date(2026, 12, 11, 10, 0, 0, 0, tpe), tpe, 2),
		[]string{"2026-12-13 09:00 Sun", "2026-12-18 09:00 Fri"})
	sameRuns(t, "Mondays only", runsAfter(t, "0 9 * * 1", nov, tpe, 2), []string{"2026-11-02 09:00 Mon", "2026-11-09 09:00 Mon"})
	sameRuns(t, "the 1st only", runsAfter(t, "0 9 1 * *", nov, tpe, 2), []string{"2026-11-01 09:00 Sun", "2026-12-01 09:00 Tue"})
	// "*/2" starts with *: odd days that are also Mondays (robfig alone would take either).
	sameRuns(t, "odd-day Mondays", runsAfter(t, "0 9 */2 * 1", nov, tpe, 3),
		[]string{"2026-11-09 09:00 Mon", "2026-11-23 09:00 Mon", "2026-12-07 09:00 Mon"})
	sameRuns(t, "Sunday as 7", runsAfter(t, "0 9 * * 7", nov, tpe, 2), []string{"2026-11-01 09:00 Sun", "2026-11-08 09:00 Sun"})
	sameRuns(t, "the weekend as 6-7", runsAfter(t, "0 9 * * 6-7", time.Date(2026, 11, 2, 0, 0, 0, 0, tpe), tpe, 3),
		[]string{"2026-11-07 09:00 Sat", "2026-11-08 09:00 Sun", "2026-11-14 09:00 Sat"})
}

// A clock change never runs one time twice: the hour a fall-back repeats runs once, a time the clock skips runs
// shifted by the gap, once.
func TestCronNextAcrossClockChanges(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	utc := func(at time.Time) string { return at.UTC().Format("01-02 15:04Z") }
	list := func(expr string, after time.Time, loc *time.Location, n int) []string {
		var out []string
		for _, at := range mustCron(t, expr).NextRuns(after, loc, n) {
			out = append(out, utc(at)+" "+at.In(loc).Format("15:04 MST"))
		}
		return out
	}
	// 2026-11-01: 02:00 EDT goes back to 01:00 EST.
	fall := time.Date(2026, 11, 1, 0, 0, 0, 0, ny)
	sameRuns(t, "01:30 on the fall-back day", list("30 1 * * *", fall, ny, 2),
		[]string{"11-01 05:30Z 01:30 EDT", "11-02 06:30Z 01:30 EST"})
	sameRuns(t, "every 30 minutes over the fall-back", list("*/30 * * * *", fall.Add(45*time.Minute), ny, 4),
		[]string{"11-01 05:00Z 01:00 EDT", "11-01 05:30Z 01:30 EDT", "11-01 07:00Z 02:00 EST", "11-01 07:30Z 02:30 EST"})
	second := time.Date(2026, 11, 1, 6, 10, 0, 0, time.UTC) // 01:10 EST, the hour's second pass
	sameRuns(t, "from the second pass", list("*/30 * * * *", second, ny, 1), []string{"11-01 07:00Z 02:00 EST"})
	// 2026-03-08: 02:00 EST jumps to 03:00 EDT.
	spring := time.Date(2026, 3, 8, 0, 0, 0, 0, ny)
	sameRuns(t, "02:30 on the spring-forward day", list("30 2 * * *", spring, ny, 2),
		[]string{"03-08 07:30Z 03:30 EDT", "03-09 06:30Z 02:30 EDT"})
	sameRuns(t, "every 30 minutes over the spring-forward", list("*/30 * * * *", spring.Add(90*time.Minute), ny, 3),
		[]string{"03-08 07:00Z 03:00 EDT", "03-08 07:30Z 03:30 EDT", "03-08 08:00Z 04:00 EDT"})
	// East of UTC Go picks the second pass of an ambiguous time; the run still takes the first.
	berlin := mustZone(t, "Europe/Berlin") // 2026-10-25: 03:00 CEST goes back to 02:00 CET
	sameRuns(t, "02:30 in Berlin on the fall-back day", list("30 2 * * *", time.Date(2026, 10, 25, 0, 0, 0, 0, berlin), berlin, 2),
		[]string{"10-25 00:30Z 02:30 CEST", "10-26 01:30Z 02:30 CET"})
}

func TestCronGap(t *testing.T) {
	for expr, want := range map[string]int{
		"*/5 * * * *":     5,
		"*/30 9-18 * * *": 30,
		"0 * * * *":       60,
		"0 9 * * *":       24 * 60, // one hour, one minute: no two runs in an hour
		"0,58 9 * * *":    58,      // 9:58, then 9:00 the next day
		"0,55 9,10 * * *": 5,       // 9:55, then 10:00
		"58 */1 * * *":    60,      // */1 is every hour: hh:58, then (hh+1):58
		"55 23,0 * * *":   60,      // 23:55, then 00:55
		"0,30 23,0 * * *": 30,
	} {
		if g := mustCron(t, expr).gap(); g != want {
			t.Fatalf("%s: gap %d, want %d", expr, g, want)
		}
	}
}

func TestCronText(t *testing.T) {
	for _, c := range []struct {
		expr string
		lang i18n.Lang
		want string
	}{
		{"*/30 9-18 * * 1-5", i18n.En, "Every 30 minutes, between 09:00 and 18:59, Monday through Friday"},
		{"0 10 1 * *", i18n.En, "At 10:00, on day 1 of the month"},
		{"@daily", i18n.En, "At 00:00"},
		{"0 9 * * 7", i18n.En, "At 09:00, only on Sunday"},
		{"*/30 9-18 * * 1-5", i18n.ZhHans, "每隔 30 分钟, 在 09:00 和 18:59 之间, 星期一至星期五"},
		{"0 9 */2 * 1", i18n.En, ""}, // Vixie wants both days, the words would read as either
	} {
		if got := mustCron(t, c.expr).Text(c.lang); got != c.want {
			t.Fatalf("%s in %s: %q, want %q", c.expr, c.lang, got, c.want)
		}
	}
	if got := mustCron(t, "0 9 * * 1-5").Text(i18n.Ja); got == "" {
		t.Fatal("Japanese has words too")
	}
}
