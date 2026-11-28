package acceptance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var clock = func() time.Time { return time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC) }

// fakeChannels answers from maps; a missing key is an error (the channel cannot answer).
type fakeChannels struct {
	prs      []string
	merged   map[string]bool
	deployed map[string]bool
	ci       map[string]string
	pages    map[string]Page
	down     error
}

func (f fakeChannels) PullRequests(context.Context) []string { return f.prs }
func (f fakeChannels) Merged(_ context.Context, pr string) (bool, error) {
	return f.merged[pr], f.down
}
func (f fakeChannels) Deployed(_ context.Context, pr string) (bool, error) {
	return f.deployed[pr], f.down
}
func (f fakeChannels) CI(_ context.Context, pr string) (string, error) { return f.ci[pr], f.down }
func (f fakeChannels) Fetch(_ context.Context, u string) (Page, error) {
	if p, ok := f.pages[u]; ok {
		return p, f.down
	}
	return Page{}, errors.New("no route")
}

const pr1 = "https://github.com/o/r/pull/1"

func TestDeclareRefusesByRoleKindNumberAndArg(t *testing.T) {
	done := []string{"a", "b"}
	cases := []struct {
		name string
		in   []Declared
		by   Role
		want string
	}{
		{"worker", []Declared{{1, KindPRMerged, ""}}, Worker, "working Agent"},
		{"supervisor http", []Declared{{1, KindHTTP, "https://x.test/h"}}, Supervisor, "only pr_merged, ci_green or deployed"},
		{"supervisor command", []Declared{{1, "command", "ls"}}, Supervisor, "kind must be"},
		{"main command", []Declared{{1, "command", "ls"}}, Main, "kind must be"},
		{"number", []Declared{{3, KindPRMerged, ""}}, Main, "not a number from 1 to 2"},
		{"arg", []Declared{{1, KindCIGreen, "x"}}, Main, "takes no arg"},
		{"url", []Declared{{1, KindHTTP, "ftp://x"}}, Main, "http or https URL"},
		{"creds", []Declared{{1, KindHTTP, "https://u:p@x.test/"}}, Main, "no credentials"},
	}
	for _, c := range cases {
		if _, err := Declare(done, c.in, c.by); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	got, err := Declare(done, []Declared{{2, KindHTTP, "https://x.test/h 204 ok"}, {1, KindDeployed, ""}, {1, KindDeployed, ""}}, Main)
	if err != nil || len(got) != 2 || got[0].Criterion != "b" || got[1].Criterion != "a" {
		t.Fatalf("declared: %+v %v", got, err)
	}
	if _, err := Declare(done, []Declared{{1, KindPRMerged, ""}}, Supervisor); err != nil {
		t.Fatal(err)
	}
}

func TestChecksBelongToTheExactTextOfACriterion(t *testing.T) {
	checks := []Check{{"a", KindPRMerged, ""}, {"b", KindDeployed, ""}}
	if got := Align([]string{"a", "b2"}, checks); len(got) != 1 || got[0].Criterion != "a" {
		t.Fatalf("an edited criterion keeps its check: %+v", got)
	}
	if got := Align([]string{"b"}, checks); len(got) != 1 || got[0].Criterion != "b" {
		t.Fatalf("a dropped criterion keeps its check: %+v", got)
	}
	merged := Merge([]string{"a", "b"}, checks, []Check{{"a", KindCIGreen, ""}})
	if len(merged) != 2 || merged[0].Criterion != "b" || merged[1] != (Check{"a", KindCIGreen, ""}) {
		t.Fatalf("merge replaces the named criterion's checks: %+v", merged)
	}
	if Covered([]string{"a", "c"}, checks) || !Covered([]string{"a", "b"}, checks) || Covered(nil, checks) {
		t.Fatal("covered")
	}
}

func TestParseHTTPDefaultsAndLimits(t *testing.T) {
	got, err := ParseHTTP("https://h.test/health")
	if err != nil || got.Status != 200 || got.Contains != "" {
		t.Fatalf("%+v %v", got, err)
	}
	got, _ = ParseHTTP("https://h.test/health 204 all good")
	if got.Status != 204 || got.Contains != "all good" {
		t.Fatalf("%+v", got)
	}
	got, _ = ParseHTTP("https://h.test/health ready")
	if got.Status != 200 || got.Contains != "ready" {
		t.Fatalf("%+v", got)
	}
	for _, bad := range []string{"", "h.test/x", "https://h.test/x 99", "https://h.test/x 600"} {
		if _, err := ParseHTTP(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestAHubCheckThatPassesAndOneThatFails(t *testing.T) {
	f := fakeChannels{prs: []string{pr1}, merged: map[string]bool{pr1: true}, ci: map[string]string{pr1: CIFailure},
		deployed: map[string]bool{pr1: false}}
	checks := []Check{{"a", KindPRMerged, ""}, {"b", KindCIGreen, ""}, {"c", KindDeployed, ""}}
	got := Run(context.Background(), checks, f, clock)
	if got[0].State != Met || got[1].State != NotMet || got[2].State != NotMet || !got[0].At.Equal(clock()) {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got[1].Evidence, "failing checks") {
		t.Fatalf("evidence: %q", got[1].Evidence)
	}
	if AllMet([]string{"a", "b", "c"}, checks, got) {
		t.Fatal("not all met")
	}
	f.ci[pr1], f.deployed[pr1] = CISuccess, true
	if got = Run(context.Background(), checks, f, clock); !AllMet([]string{"a", "b", "c"}, checks, got) {
		t.Fatalf("all met: %+v", got)
	}
}

func TestACheckThatCannotBeMadeIsReportedAndNeverMet(t *testing.T) {
	checks := []Check{{"a", KindPRMerged, ""}}
	none := Run(context.Background(), checks, fakeChannels{}, clock)
	if none[0].State != Cannot || !strings.Contains(none[0].Evidence, "no pull request is recorded") {
		t.Fatalf("%+v", none)
	}
	noAccess := Run(context.Background(), checks, fakeChannels{prs: []string{pr1}, down: errors.New("no GitHub access")}, clock)
	if noAccess[0].State != Cannot || !strings.Contains(noAccess[0].Evidence, "no GitHub access") {
		t.Fatalf("%+v", noAccess)
	}
	v := Fold([]string{"a", "b"}, append(noAccess, Result{Check: Check{"a", KindDeployed, ""}, State: Met, Evidence: "x"}))
	if len(v) != 1 || v[0].State != Cannot {
		t.Fatalf("a criterion with a cannot is not met: %+v", v)
	}
	if AllMet([]string{"a"}, checks, noAccess) {
		t.Fatal("cannot counted as met")
	}
}

func TestGatesRunOwnerWaitFirst(t *testing.T) {
	later := errors.New("criteria")
	if err := Gates("todo_1", true, true, "owner picks", later); err == nil || !strings.Contains(err.Error(), "owner picks") {
		t.Fatalf("%v", err)
	}
	if err := Gates("todo_1", false, true, "", later); err != later {
		t.Fatalf("a closed to-do has no owner wait: %v", err)
	}
	if err := Gates("todo_1", true, false, "", nil); err != nil {
		t.Fatal(err)
	}
}

func fetch(t *testing.T, h HTTP, u string) (Page, error) {
	t.Helper()
	return h.Fetch(context.Background(), u)
}

func TestTheHTTPCheckAgainstAServer(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("credentials sent: %v", r.Header)
		}
		_, _ = w.Write([]byte("service ready SECRET-BODY"))
	})
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", MostBody+10) + "tail"))
	})
	mux.HandleFunc("/hop/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		n := int(r.URL.Path[len("/hop/")] - '0')
		if n == 0 {
			_, _ = w.Write([]byte("end"))
			return
		}
		http.Redirect(w, r, "/hop/"+string(rune('0'+n-1)), http.StatusFound)
	})
	block := make(chan struct{})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) { <-block })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(block)
	h := HTTP{Timeout: 100 * time.Millisecond}

	live := liveChannels{h}
	cases := []struct {
		arg, state, in, notIn string
	}{
		{srv.URL + "/ok", Met, "answered 200", "SECRET"},
		{srv.URL + "/ok 200 service ready", Met, "required text found", "SECRET"},
		{srv.URL + "/ok 200 missing words", NotMet, "required text not found", "SECRET"},
		{srv.URL + "/down", NotMet, "answered 503 (expected 200)", ""},
		{srv.URL + "/down 503", Met, "answered 503", ""},
		{srv.URL + "/hop/3", Met, "answered 200", ""},
		{srv.URL + "/hop/4", Cannot, "more than 3 redirects", ""},
		{srv.URL + "/slow", Cannot, "cannot be fetched", "SECRET"},
	}
	for _, c := range cases {
		got := Run(context.Background(), []Check{{"a", KindHTTP, c.arg}}, live, clock)[0]
		if got.State != c.state || !strings.Contains(got.Evidence, c.in) || c.notIn != "" && strings.Contains(got.Evidence, c.notIn) {
			t.Errorf("%s: %+v", c.arg, got)
		}
	}
	page, err := fetch(t, h, srv.URL+"/big")
	if err != nil || len(page.Body) != MostBody {
		t.Fatalf("the body is read to %d bytes: %d %v", MostBody, len(page.Body), err)
	}
	if _, err := fetch(t, h, "ftp://x.test/"); err == nil {
		t.Fatal("ftp fetched")
	}
}

// liveChannels fetches through h and knows no pull request.
type liveChannels struct{ h HTTP }

func (liveChannels) PullRequests(context.Context) []string          { return nil }
func (liveChannels) Merged(context.Context, string) (bool, error)   { return false, nil }
func (liveChannels) Deployed(context.Context, string) (bool, error) { return false, nil }
func (liveChannels) CI(context.Context, string) (string, error)     { return "", nil }
func (l liveChannels) Fetch(ctx context.Context, u string) (Page, error) {
	return l.h.Fetch(ctx, u)
}
