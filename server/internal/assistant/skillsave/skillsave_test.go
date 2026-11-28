package skillsave

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sunfayao/persona/server/internal/assistantskills"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/rpc"
)

var receiptExpires = time.Date(2026, 11, 27, 12, 10, 0, 0, time.UTC)

// fake is the node's importer and the permission card as a save sees them. Its preview reads the archive as the
// importer does: one <name>/SKILL.md whose front matter holds the name and the description as strings.
type fake struct {
	mu         sync.Mutex
	previewErr error
	previewAs  string // the name the preview reports; the archive's when empty
	installErr error
	installAs  string // the install's status; installed when empty
	listed     map[string]assistantskills.Skill
	asked      int // Host.Importer calls
	previews   [][]byte
	installs   []installCall
	cancels    []string
	cards      []card
}

type installCall struct {
	archive   []byte
	previewed Receipt
}

type card struct {
	summary string
	details json.RawMessage
	expires time.Time
}

func (f *fake) Preview(_ context.Context, archive []byte) (Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews = append(f.previews, archive)
	if f.previewErr != nil {
		return Receipt{}, f.previewErr
	}
	name, description, content, err := unpack(archive)
	if err != nil {
		return Receipt{}, &rpc.Error{Code: "skill_zip_invalid", Message: err.Error()}
	}
	if f.previewAs != "" {
		name = f.previewAs
	}
	return Receipt{ImportResult: assistantskills.ImportResult{Status: "previewed", Name: name, Description: description,
		Files: 1, Bytes: len(content), Reference: "ref-preview", ExpiresAt: receiptExpires}, Environment: "env-1",
		Machine: "Example computer"}, nil
}

func (f *fake) Install(_ context.Context, archive []byte, previewed Receipt) (Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs = append(f.installs, installCall{archive, previewed})
	if f.installErr != nil {
		return Receipt{}, f.installErr
	}
	out := previewed
	out.Status, out.Reference = "installed", "ref-installed"
	if f.installAs != "" {
		out.Status = f.installAs
	}
	return out, nil
}

func (f *fake) Cancel(_ context.Context, previewed Receipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, previewed.Reference)
	return nil
}

func (f *fake) Skill(_ context.Context, name string) (assistantskills.Skill, bool) {
	skill, ok := f.listed[name]
	return skill, ok
}

// saver saves through f; confirm answers the card.
func (f *fake) saver(t *testing.T, confirm func() (bool, string, error)) *Saver {
	return New(Host{
		Importer: func(context.Context) (Importer, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.asked++
			return f, nil
		},
		Confirm: func(_ context.Context, summary string, details json.RawMessage, expires time.Time) (bool, string, error) {
			f.mu.Lock()
			f.cards = append(f.cards, card{summary, details, expires})
			early := len(f.installs) > 0
			f.mu.Unlock()
			if early {
				t.Error("the skill was installed before the user answered the card")
			}
			return confirm()
		},
		Names:    func(context.Context) []string { return []string{"kiln-box", "orchard.example.net"} },
		Language: func(context.Context) i18n.Lang { return i18n.En },
		Logf:     t.Logf,
	})
}

func approve() (bool, string, error) { return true, "", nil }

// unpack reads a skill archive as the importer does, and refuses any other layout.
func unpack(archive []byte) (name, description, content string, err error) {
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", "", "", err
	}
	if len(z.File) != 1 || !strings.HasSuffix(z.File[0].Name, "/SKILL.md") || strings.Count(z.File[0].Name, "/") != 1 {
		return "", "", "", errors.New("want one <name>/SKILL.md")
	}
	r, err := z.File[0].Open()
	if err != nil {
		return "", "", "", err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", "", "", err
	}
	content = string(b)
	front, ok := strings.CutPrefix(content, "---\n")
	front, _, found := strings.Cut(front, "\n---\n")
	if !ok || !found {
		return "", "", "", errors.New("no front matter")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return "", "", "", err
	}
	values := map[string]string{}
	pairs := doc.Content[0].Content
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1].Tag != "!!str" {
			return "", "", "", errors.New(pairs[i].Value + " is not a string")
		}
		values[pairs[i].Value] = pairs[i+1].Value
	}
	if strings.TrimSuffix(z.File[0].Name, "/SKILL.md") != values["name"] {
		return "", "", "", errors.New("the folder is not the skill's name")
	}
	return values["name"], values["description"], content, nil
}

func arguments(name, description, body string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"name": name, "description": description, "body": body})
	return raw
}

func TestSaveInstallsTheSkillOnlyAfterTheUsersApproval(t *testing.T) {
	t.Parallel()
	f := &fake{}
	out, err := f.saver(t, approve).Save(context.Background(),
		arguments("deploy-checklist", "  Use when: the user ships the web app  ", "# Deploy\n\n1. Run the tests.\n2. Tag the release.\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	name, description, content, err := unpack(f.previews[0])
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nname: deploy-checklist\ndescription: 'Use when: the user ships the web app'\n---\n\n# Deploy\n\n1. Run the tests.\n2. Tag the release.\n"
	if name != "deploy-checklist" || description != "Use when: the user ships the web app" || content != want {
		t.Fatalf("archive holds %q (%q, %q), want %q", content, name, description, want)
	}
	if len(f.cards) != 1 {
		t.Fatalf("cards: %+v", f.cards)
	}
	c := f.cards[0]
	wantSummary := "Save “deploy-checklist” (" + strconv.Itoa(len(want)) + " bytes) as a skill on Example computer? New conversations there can use it."
	if c.summary != wantSummary || !c.expires.Equal(receiptExpires) {
		t.Fatalf("card %q until %v, want %q until %v", c.summary, c.expires, wantSummary, receiptExpires)
	}
	var details map[string]any
	if json.Unmarshal(c.details, &details) != nil || details["name"] != "deploy-checklist" || details["machine"] != "Example computer" ||
		details["description"] != "Use when: the user ships the web app" || details["bytes"] != float64(len(want)) || len(details) != 4 {
		t.Fatalf("card details %s", c.details)
	}
	if len(f.installs) != 1 || !bytes.Equal(f.installs[0].archive, f.previews[0]) || f.installs[0].previewed.Reference != "ref-preview" ||
		f.installs[0].previewed.Environment != "env-1" || len(f.cancels) != 0 {
		t.Fatalf("installs %+v, cancels %v", f.installs, f.cancels)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("result %q: %v", out, err)
	}
	if result["status"] != "installed" || result["reference"] != "ref-installed" || result["environment"] != "env-1" ||
		result["machine"] != "Example computer" || !strings.Contains(result["undo"].(string), `skill_import with op "undo"`) {
		t.Fatalf("result %s", out)
	}
}

func TestTheLimitsAreInclusive(t *testing.T) {
	t.Parallel()
	f := &fake{}
	// Text a scrubber leaves alone: a long run of hex letters would read as a token.
	description := strings.Repeat("é", MaxDescription)
	body := strings.Repeat("Check the logs.\n", MaxBody/16-1) + "Check the logs!"
	body += strings.Repeat("!", MaxBody-len(body))
	if _, err := f.saver(t, approve).Save(context.Background(), arguments(strings.Repeat("z", 64), description, body)); err != nil {
		t.Fatal(err)
	}
	if len(f.installs) != 1 {
		t.Fatalf("installs %d", len(f.installs))
	}
}

func TestADeclinedOrUnansweredSaveChangesNothing(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		text string
		err  error
	}{
		{"declined", "The user declined the change: nothing changed.", nil},
		{"unanswered", "The user did not answer the approval card for this change themselves: nothing changed.", nil},
		{"withdrawn", "", context.Canceled},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := &fake{}
			out, err := f.saver(t, func() (bool, string, error) { return false, c.text, c.err }).Save(context.Background(),
				arguments("deploy-checklist", "Ship the web app", "1. Run the tests."))
			if out != c.text || !errors.Is(err, c.err) || (c.err == nil) != (err == nil) {
				t.Fatalf("got %q, %v", out, err)
			}
			if len(f.installs) != 0 || len(f.cancels) != 1 || f.cancels[0] != "ref-preview" {
				t.Fatalf("installs %+v, cancels %v: the preview must be dropped and nothing installed", f.installs, f.cancels)
			}
		})
	}
}

func TestPrivateTextIsRefusedWithWhatWouldBeHidden(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, skill, description, body string
		want                           string
	}{
		{"a token", "deploy", "Ship it", "1. Run make.\n2. Export API_TOKEN=abcd1234efgh5678", "body line 2: 2. Export API_TOKEN=<redacted>"},
		{"an e-mail address", "deploy", "Ship it", "Tell ops@example.com when done.", "body line 1: Tell <email> when done."},
		{"a home-directory path", "deploy", "Ship it", "# Edit\nOpen /Users/alice/app/config.json", "body line 2: Open ~/app/config.json"},
		{"a URL", "deploy", "Ship it", "Open https://intranet.example.com/deploy", "body line 1: Open https://<host>/deploy"},
		{"an address", "deploy", "Ship it", "ssh to 192.0.2.10 first", "body line 1: ssh to <ip> first"},
		{"a host name", "deploy", "Ship it", "Build on orchard.example.net", "body line 1: Build on <machine>"},
		{"a machine name in the description", "deploy", "Ships from Kiln-Box", "1. Run make.", "description: Ships from <machine>"},
		{"a machine name in the name", "kiln-box-deploy", "Ship it", "1. Run make.", "name: <machine>-deploy"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := &fake{}
			_, err := f.saver(t, approve).Save(context.Background(), arguments(c.skill, c.description, c.body))
			if err == nil || !strings.HasPrefix(err.Error(), "skill_save_private: nothing was shown or saved") || !strings.Contains(err.Error(), "\n"+c.want+"\n") {
				t.Fatalf("got %v, want a refusal showing %q", err, c.want)
			}
			if f.asked != 0 || len(f.cards) != 0 {
				t.Fatalf("a private skill reached the computer (%d) or a card (%d)", f.asked, len(f.cards))
			}
		})
	}
}

func TestBadArgumentsAreRefusedBeforeTheComputerIsAsked(t *testing.T) {
	t.Parallel()
	ok := func(name, description, body string) string { return string(arguments(name, description, body)) }
	for _, c := range []struct {
		raw, code string
	}{
		{`null`, "skill_save_request"},
		{`[]`, "skill_save_request"},
		{`{"name":"deploy","description":"Ship it"}`, "skill_save_request"},
		{`{"name":"deploy","description":"Ship it","body":null}`, "skill_save_request"},
		{`{"name":"deploy","description":"Ship it","body":"x","replace":true}`, "skill_save_request"},
		{`{"name":5,"description":"Ship it","body":"x"}`, "skill_save_request"},
		{`{"name":"deploy","description":"Ship it","body":"x"} {}`, "skill_save_request"},
		{ok("Deploy", "Ship it", "x"), "skill_save_name"},
		{ok("deploy_checklist", "Ship it", "x"), "skill_save_name"},
		{ok("-deploy", "Ship it", "x"), "skill_save_name"},
		{ok("deploy--checklist", "Ship it", "x"), "skill_save_name"},
		{ok("deploy checklist", "Ship it", "x"), "skill_save_name"},
		{ok("", "Ship it", "x"), "skill_save_name"},
		{ok("con", "Ship it", "x"), "skill_save_name"},
		{ok(strings.Repeat("a", 65), "Ship it", "x"), "skill_save_name"},
		{ok("deploy", " ", "x"), "skill_save_description"},
		{ok("deploy", "Ship it\nthen rest", "x"), "skill_save_description"},
		{ok("deploy", strings.Repeat("é", MaxDescription+1), "x"), "skill_save_description"},
		{ok("deploy", "Ship it", " \n\t\n"), "skill_save_body"},
		{ok("deploy", "Ship it", strings.Repeat("a", MaxBody+1)), "skill_save_body"},
		{ok("deploy", "Ship it", "---\nname: deploy\n---\n1. Run make."), "skill_save_body"},
		{ok("deploy", "Ship it", "\n\n---\n1. Run make."), "skill_save_body"},
		{ok("deploy", "Ship it", "1. Run\x00make."), "skill_save_body"},
		{`{"name":"deploy","description":"Ship it","body":"` + strings.Repeat("a", maxArguments) + `"}`, "skill_save_body"},
	} {
		f := &fake{}
		_, err := f.saver(t, approve).Save(context.Background(), json.RawMessage(c.raw))
		if err == nil || !strings.HasPrefix(err.Error(), c.code+": ") {
			t.Errorf("%.80s: got %v, want %s", c.raw, err, c.code)
		}
		if f.asked != 0 || len(f.cards) != 0 {
			t.Errorf("%.80s reached the computer (%d) or a card (%d)", c.raw, f.asked, len(f.cards))
		}
	}
}

func TestAnExistingNameIsRefusedNamingTheSkill(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		listed map[string]assistantskills.Skill
		want   string
	}{
		{"the user's", map[string]assistantskills.Skill{"deploy-checklist": {Name: "deploy-checklist", Description: "The team's release steps", Source: "user"}},
			`the user's skill "deploy-checklist" ("The team's release steps")`},
		{"built in", map[string]assistantskills.Skill{"deploy-checklist": {Name: "deploy-checklist", Source: "builtin"}},
			`Persona's built-in skill "deploy-checklist";`},
		{"unlisted", nil, `a skill or skill folder named "deploy-checklist"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := &fake{previewErr: &rpc.Error{Code: "skill_conflict", Message: "private path details"}, listed: c.listed}
			_, err := f.saver(t, approve).Save(context.Background(), arguments("deploy-checklist", "Ship it", "1. Run make."))
			if err == nil || !strings.HasPrefix(err.Error(), "skill_conflict: ") || !strings.Contains(err.Error(), c.want) ||
				!strings.Contains(err.Error(), "nothing was saved") || strings.Contains(err.Error(), "private path") {
				t.Fatalf("got %v, want it to name %s", err, c.want)
			}
			if len(f.cards) != 0 || len(f.installs) != 0 {
				t.Fatalf("cards %d, installs %d", len(f.cards), len(f.installs))
			}
		})
	}
}

func TestAComputerThatDoesNotTakeTheSkillSavesNothing(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		f    *fake
		want string
	}{
		{"an old node", &fake{previewErr: &rpc.Error{Code: "node_outdated"}}, "node_outdated: Persona on the assistant's computer is too old"},
		{"busy", &fake{previewErr: &rpc.Error{Code: "skill_import_busy"}}, "skill_import_busy: "},
		{"another refusal", &fake{previewErr: &rpc.Error{Code: "skill_import_io", Message: "/private/path"}}, "skill_import_io: the assistant's computer did not take the skill; nothing was saved"},
		{"unreachable", &fake{previewErr: errors.New("dial: private address")}, "skill_save_unavailable: "},
		{"another skill read", &fake{previewAs: "other-skill"}, "skill_save_changed: "},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.f.saver(t, approve).Save(context.Background(), arguments("deploy-checklist", "Ship it", "1. Run make."))
			if err == nil || !strings.HasPrefix(err.Error(), c.want) || strings.Contains(err.Error(), "private") {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			if len(c.f.cards) != 0 || len(c.f.installs) != 0 {
				t.Fatalf("cards %d, installs %d", len(c.f.cards), len(c.f.installs))
			}
		})
	}
}

func TestAnUncertainInstallPointsAtTheReceipt(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		f    *fake
		code string
	}{
		{"a lost answer", &fake{installErr: errors.New("connection reset by a private peer")}, "skill_save_unknown"},
		{"a refusal", &fake{installErr: &rpc.Error{Code: "skill_import_expired"}}, "skill_import_expired"},
		{"another status", &fake{installAs: "previewed"}, "skill_save_unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := c.f.saver(t, approve).Save(context.Background(), arguments("deploy-checklist", "Ship it", "1. Run make."))
			want := c.code + `: the skill may or may not be saved. Before saving it again, call skill_import with op "status", reference "ref-preview" and environment "env-1"`
			if err == nil || err.Error() != want {
				t.Fatalf("got %v, want %q", err, want)
			}
		})
	}
}

func TestNoComputerIsSaidAsTheHostSaysIt(t *testing.T) {
	t.Parallel()
	missing := errors.New("no assistant computer")
	s := New(Host{
		Importer: func(context.Context) (Importer, error) { return nil, missing },
		Names:    func(context.Context) []string { return nil },
	})
	if _, err := s.Save(context.Background(), arguments("deploy-checklist", "Ship it", "1. Run make.")); !errors.Is(err, missing) {
		t.Fatalf("got %v", err)
	}
}
