package usagewindows

import (
	"testing"
	"time"
)

func TestClaudeUsageReportWithWrapperBannerAndScopedWindow(t *testing.T) {
	now := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	raw := []byte("launcher started\n" + `[{"type":"system"},{"usage_report":{"rate_limits":{"limits":[
		{"kind":"weekly_scoped","percent":23,"resets_at":"2026-10-11T12:59:59.98Z","scope":{"model":{"display_name":"Example"}}},
		{"kind":"session","percent":0,"resets_at":"2026-10-10T11:00:00Z"},
		{"kind":"weekly_all"},
		{"kind":"session","percent":12,"resets_at":"2026-10-10T07:59:00Z"}
	 ]}}}]`)
	account, retry := ParseClaudeReport(raw, now)
	if retry || !account.SampledAt.Equal(now) || len(account.Windows) != 2 {
		t.Fatalf("account %+v, retry %t", account, retry)
	}
	first, scoped := account.Windows[0], account.Windows[1]
	if first.WindowMinutes != 300 || first.UsedPercent != 0 || scoped.Model != "Example" ||
		scoped.ResetsAt == nil || scoped.ResetsAt.Format(time.RFC3339) != "2026-10-11T13:00:00Z" {
		t.Fatalf("windows %+v", account.Windows)
	}
}

func TestClaudeUsageClassifiesFailures(t *testing.T) {
	for _, test := range []struct {
		out   string
		retry bool
	}{
		{"temporary connection problem", true},
		{`{"type":"result","result":"Please run /login"}`, false},
		{`{"usage_report":{"rate_limits":null}}`, false},
	} {
		account, retry := ParseClaudeReport([]byte(test.out), time.Now())
		if len(account.Windows) != 0 || retry != test.retry {
			t.Errorf("%q returned %+v, retry %t", test.out, account, retry)
		}
	}
}
