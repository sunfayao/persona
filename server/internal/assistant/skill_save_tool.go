package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/skillsave"
	"github.com/sunfayao/persona/server/internal/assistantskills"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// toolSkillSave is skill_save (assistant/skillsave): a procedure worked out in a conversation kept as a skill of the
// assistant's computer, through the importer and the permission card skill_import uses.
func (s *Service) toolSkillSave(ctx context.Context, machine, self string, raw json.RawMessage) (string, error) {
	return skillsave.New(skillsave.Host{
		Importer: func(ctx context.Context) (skillsave.Importer, error) {
			target, err := s.skillImportTarget(ctx)
			if err != nil {
				return nil, err
			}
			return skillSaveImporter{s, target}, nil
		},
		Confirm: func(ctx context.Context, summary string, details json.RawMessage, expires time.Time) (bool, string, error) {
			ok, text, err := s.confirmSkillImport(ctx, machine, self, "skill_save", summary, details, expires)
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil { // the card's own time ran out
				return false, prompts.RenderSettingUnanswered(), nil
			}
			return ok, text, err
		},
		Names:    s.skillSaveNames,
		Language: s.lang,
		Logf:     s.logf,
	}).Save(ctx, raw)
}

// skillSaveNames are the names a saved skill must not keep: this host's, and every computer's id, name and ssh host.
func (s *Service) skillSaveNames(ctx context.Context) []string {
	var names []string
	if host, err := os.Hostname(); err == nil {
		names = append(names, host)
	}
	for _, d := range s.devices(ctx) {
		names = append(names, d.ID, d.Name, d.SSHHost)
	}
	return names
}

// skillSaveImporter is the node's importer on the assistant's computer as one save found it: runSkillImport refuses a
// call once that target changed.
type skillSaveImporter struct {
	s      *Service
	target skillImportTarget
}

func (i skillSaveImporter) Preview(ctx context.Context, archive []byte) (skillsave.Receipt, error) {
	return i.run(ctx, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "preview", Archive: archive}})
}

func (i skillSaveImporter) Install(ctx context.Context, archive []byte, previewed skillsave.Receipt) (skillsave.Receipt, error) {
	return i.run(ctx, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "install", Archive: archive,
		Reference: previewed.Reference}, Environment: previewed.Environment})
}

func (i skillSaveImporter) Cancel(ctx context.Context, previewed skillsave.Receipt) error {
	_, err := i.run(ctx, skillImportInput{ImportRequest: assistantskills.ImportRequest{Op: "cancel",
		Reference: previewed.Reference}, Environment: previewed.Environment})
	return err
}

func (i skillSaveImporter) run(ctx context.Context, in skillImportInput) (skillsave.Receipt, error) {
	out, err := i.s.runSkillImport(ctx, i.target, in)
	return skillsave.Receipt{ImportResult: out.ImportResult, Environment: out.Environment, Machine: out.Machine}, err
}

// Skill is name in the computer's list of the assistant's skills (skills.list: the user's, built-in and extensions').
func (i skillSaveImporter) Skill(ctx context.Context, name string) (assistantskills.Skill, bool) {
	d, ok := i.s.device(ctx, i.target.machine)
	if !ok {
		return assistantskills.Skill{}, false
	}
	var list struct {
		Skills []assistantskills.Skill `json:"skills"`
	}
	if err := i.s.call(ctx, i.target.machine, "skills.list", map[string]any{"cwd": i.s.assistantCwd(d)}, &list); err != nil {
		return assistantskills.Skill{}, false
	}
	for _, skill := range list.Skills {
		if strings.EqualFold(skill.Name, name) {
			return skill, true
		}
	}
	return assistantskills.Skill{}, false
}
