// Package skillsave keeps a short procedure that a conversation worked out as a skill on the main conversation's
// computer (the skill_save tool). It checks the text, refuses text that would keep a secret, an address, a
// home-directory path or a machine name, writes the rest as a skill archive holding one file, <name>/SKILL.md, and
// installs that with the node's skill importer, the preview and install skill_import uses (skills.import), only after
// the user approved it on a permission card. The node gains nothing, so every computer that takes skill_import takes
// it. It never overwrites: the importer refuses a name in use. The Service lends it what it needs through Host.
package skillsave

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/sunfayao/persona/server/internal/assistantskills"
	"github.com/sunfayao/persona/server/internal/errreport"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

const (
	// MaxBody is the largest procedure kept, in bytes. A runtime reads a skill's SKILL.md whole into every conversation
	// that uses the skill, so each byte is paid for again in each of them: 8 KiB, about 2,000 tokens or 150 lines, holds
	// a long checklist. A procedure that needs more is a reference with files of its own, which a ZIP for skill_import
	// carries.
	MaxBody = 8 << 10
	// MaxDescription is the most characters of a description: one line, which runtimes list with every skill in every
	// conversation.
	MaxDescription = 200
	// maxArguments bounds the arguments read: a body of MaxBody fits several times, every character escaped.
	maxArguments = 256 << 10
)

// undoHint tells the caller how to take a saved skill back; skill_import's description says the same to a model that
// has read it.
const undoHint = `skill_import with op "undo" and this reference and environment, until expires_at`

// Receipt is the importer's answer for one archive: assistantskills.ImportResult with the environment it was given in
// and the computer's name, the shape of skill_import's results (whose status and undo take its reference and
// environment).
type Receipt struct {
	assistantskills.ImportResult
	Environment string `json:"environment"`
	Machine     string `json:"machine"`
}

// Importer is the node's skill importer on the main conversation's computer (skills.import). Each call goes to the
// computer the save began with and fails once that changed; each bounds its own wait.
type Importer interface {
	// Preview checks archive on the computer and keeps a receipt for it there; it installs nothing.
	Preview(ctx context.Context, archive []byte) (Receipt, error)
	// Install installs the archive a preview checked, the same bytes.
	Install(ctx context.Context, archive []byte, previewed Receipt) (Receipt, error)
	// Cancel drops a preview's receipt.
	Cancel(ctx context.Context, previewed Receipt) error
	// Skill is the skill the computer lists under name, in any case; false when it lists none.
	Skill(ctx context.Context, name string) (assistantskills.Skill, bool)
}

// Host is what the package takes from the Service.
type Host struct {
	// Importer is the importer of the main conversation's computer as of now; an error says why there is none.
	Importer func(ctx context.Context) (Importer, error)
	// Names are the user's machine names and host names, which a skill must not keep.
	Names func(ctx context.Context) []string
	// Logf logs what could not be cleaned up.
	Logf func(format string, args ...any)
}

// Saver saves skills (skill_save).
type Saver struct{ h Host }

// New is a Saver that uses h.
func New(h Host) *Saver {
	if h.Logf == nil {
		h.Logf = func(string, ...any) {}
	}
	return &Saver{h: h}
}

// Save keeps the procedure raw describes (skill_save's arguments) as a skill on the main conversation's computer,
// installing immediately after preview validation and returning the importer's receipt with how to undo it. A refusal
// is an error that says what to change.
func (s *Saver) Save(ctx context.Context, raw json.RawMessage) (string, error) {
	in, err := parse(raw)
	if err != nil {
		return "", err
	}
	if err := in.private(s.h.Names(ctx)); err != nil {
		return "", err
	}
	archive, err := in.archive()
	if err != nil {
		return "", fmt.Errorf("skill_save_archive: %v; nothing was saved", err)
	}
	importer, err := s.h.Importer(ctx)
	if err != nil {
		return "", err
	}
	previewed, err := importer.Preview(ctx, archive)
	if err != nil {
		return "", s.notTaken(ctx, importer, in.name, err)
	}
	if previewed.Status != "previewed" || previewed.Name != in.name {
		s.cancel(ctx, importer, previewed)
		return "", errors.New("skill_save_changed: the computer read another skill from the archive; nothing was saved")
	}
	installed, err := importer.Install(ctx, archive, previewed)
	if err != nil || installed.Status != "installed" {
		return "", unknownOutcome(err, previewed)
	}
	out, err := json.Marshal(struct {
		Receipt
		Undo string `json:"undo"`
	}{installed, undoHint})
	return string(out), err
}

// notTaken says why the computer did not take the archive. A preview installs nothing, so nothing was saved.
func (s *Saver) notTaken(ctx context.Context, importer Importer, name string, err error) error {
	switch code := errorCode(err, "skill_save_unavailable"); code {
	case "skill_conflict":
		skill, ok := importer.Skill(ctx, name)
		if !ok {
			return fmt.Errorf("skill_conflict: the main conversation's computer already has a skill or skill folder named %q; nothing was saved. Choose another name", name)
		}
		owner := map[string]string{"user": "the user's skill", "builtin": "Persona's built-in skill", "plugin": "an extension's skill"}[skill.Source]
		if owner == "" {
			owner = "the skill"
		}
		what := ""
		if d := textfmt.Shorten(skill.Description, MaxDescription); d != "" {
			what = fmt.Sprintf(" (%q)", d)
		}
		return fmt.Errorf("skill_conflict: the main conversation's computer already has %s %q%s; nothing was saved. Choose another name", owner, skill.Name, what)
	case "node_outdated":
		return errors.New("node_outdated: Persona on the main conversation's computer is too old to import skills; nothing was saved. The user can update it there")
	case "skill_import_busy":
		return errors.New("skill_import_busy: the computer is importing another skill; nothing was saved. Try again shortly")
	default:
		return fmt.Errorf("%s: the main conversation's computer did not take the skill; nothing was saved", code)
	}
}

// cancel drops a preview's receipt, which would otherwise hold one of the importer's few places for ten minutes.
func (s *Saver) cancel(ctx context.Context, importer Importer, previewed Receipt) {
	if err := importer.Cancel(context.WithoutCancel(ctx), previewed); err != nil {
		s.h.Logf("assistant: skill_save: dropping the preview of %s: %v", previewed.Name, err)
	}
}

// unknownOutcome is an install that failed or answered something else: it may still have happened, and the receipt
// of the preview says whether it did.
func unknownOutcome(err error, previewed Receipt) error {
	return fmt.Errorf("%s: the skill may or may not be saved. Before saving it again, call skill_import with op \"status\", reference %q and environment %q",
		errorCode(err, "skill_save_unknown"), previewed.Reference, previewed.Environment)
}

// errorCode is the stable code of a node's refusal, or fallback. Nothing else of the error is shown: it may carry
// paths or transport details of the computer.
func errorCode(err error, fallback string) string {
	var coded *rpc.Error
	if errors.As(err, &coded) && coded.Code != "" {
		return coded.Code
	}
	return fallback
}

// skill is skill_save's arguments, checked: the description trimmed, the body without trailing white space.
type skill struct{ name, description, body string }

// parse reads skill_save's arguments and checks them. A refusal names the argument and the rule.
func parse(raw json.RawMessage) (skill, error) {
	if len(raw) > maxArguments {
		return skill{}, fmt.Errorf("skill_save_body: the arguments are %d bytes; a body may have at most %d", len(raw), MaxBody)
	}
	var a struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Body        *string `json:"body"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return skill{}, fmt.Errorf("skill_save_request: %v; pass name, description and body as strings, and nothing else", err)
	}
	if d.Decode(new(any)) != io.EOF || a.Name == nil || a.Description == nil || a.Body == nil {
		return skill{}, errors.New("skill_save_request: pass name, description and body as strings, and nothing else")
	}
	in := skill{name: *a.Name, description: strings.TrimSpace(*a.Description), body: strings.TrimRight(*a.Body, " \t\r\n")}
	firstLine, _, _ := strings.Cut(strings.TrimLeft(in.body, " \t\r\n"), "\n")
	switch n := utf8.RuneCountInString(in.description); {
	case !assistantskills.ValidName(in.name):
		return in, fmt.Errorf("skill_save_name: %q is not a skill name: use lowercase letters and digits, words joined by single hyphens, at most 64 characters (such as deploy-checklist)", textfmt.Shorten(in.name, 80))
	case in.description == "":
		return in, errors.New("skill_save_description: the description is empty: say in one line what the skill is for and when to use it")
	case strings.IndexFunc(in.description, unicode.IsControl) >= 0:
		return in, errors.New("skill_save_description: the description must be one line")
	case n > MaxDescription:
		return in, fmt.Errorf("skill_save_description: the description has %d characters; the limit is %d", n, MaxDescription)
	case strings.TrimSpace(in.body) == "":
		return in, errors.New("skill_save_body: the body is empty: write the procedure in Markdown")
	case len(in.body) > MaxBody:
		return in, fmt.Errorf("skill_save_body: the body is %d bytes; the limit is %d bytes (8 KiB). Shorten it, or put a longer skill with files of its own in a ZIP for skill_import", len(in.body), MaxBody)
	case strings.TrimSpace(firstLine) == "---":
		return in, errors.New("skill_save_body: the body starts with front matter (---): pass only the procedure; Persona writes the front matter from name and description")
	case strings.IndexFunc(in.body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0:
		return in, errors.New("skill_save_body: the body holds control characters; write plain Markdown")
	}
	return in, nil
}

// private refuses a skill that would keep what Persona keeps out of an error report (errreport.Scrubber): a secret or
// a token, an e-mail address, a URL's host, a network address, a user name in a home-directory path, or one of names.
// The refusal shows each changed part as the scrubber leaves it, for the caller to rewrite; nothing has been shown to
// the user or sent to a computer yet.
func (in skill) private(names []string) error {
	scrub := errreport.NewScrubber(names...)
	var found []string
	if v := scrub.Text(in.name); v != in.name {
		found = append(found, "name: "+v)
	}
	if v := scrub.Text(in.description); v != in.description {
		found = append(found, "description: "+v)
	}
	if v := scrub.Text(in.body); v != in.body {
		found = append(found, changedLines(in.body, v)...)
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("skill_save_private: nothing was shown or saved: a skill must not keep secrets, e-mail or network addresses, URL hosts, home-directory paths or computer names. As Persona hides them (<redacted> a secret or token, <email>, <host>, <ip>, ~ a home directory, <machine> a computer's name):\n%s\nRewrite those parts without them, then call skill_save again",
		strings.Join(found, "\n"))
}

// changedLines are the body's lines the scrubber changed, by number, as it left them. The scrubber keeps line breaks;
// if it did not, the whole scrubbed body is shown.
func changedLines(body, scrubbed string) []string {
	before, after := strings.Split(body, "\n"), strings.Split(scrubbed, "\n")
	if len(before) != len(after) {
		return []string{"body:\n" + scrubbed}
	}
	var out []string
	for i := range before {
		if before[i] != after[i] {
			out = append(out, fmt.Sprintf("body line %d: %s", i+1, after[i]))
		}
	}
	return out
}

// archive is the skill as the importer takes it: a ZIP with <name>/SKILL.md, whose front matter holds the name and the
// description (YAML quotes what needs it, so both stay strings), then the body. The file is stored, not compressed:
// the importer refuses an entry that compresses more than 200 times, as a repetitive body could.
func (in skill) archive() ([]byte, error) {
	front, err := yaml.Marshal(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{in.name, in.description})
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.CreateHeader(&zip.FileHeader{Name: in.name + "/SKILL.md", Method: zip.Store})
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "---\n%s---\n\n%s\n", front, in.body); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
