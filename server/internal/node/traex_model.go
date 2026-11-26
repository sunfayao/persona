package node

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// A Persona start records its launcher. A plain native conversation can use the native catalog, but for an unrecorded
// custom provider the database names the model's adapter, not the launcher that configured it.
func (n *Node) traexModelLauncher(id string, thread codexAppThread) string {
	if st, ok := n.state.get(id); ok && st.Launcher != "" {
		return st.Launcher
	}
	if thread.ModelProvider == "" || thread.ModelProvider == "trae" {
		return traexAgent
	}
	return ""
}

func (n *Node) traexModelRow(s *proto.Session) {
	thread, ok := n.traexThread(s.SessionID)
	if !ok {
		return
	}
	// The native setting changes without a new turn or transcript entry.
	if thread.Model != "" {
		s.Model = thread.Model
	}
	if n.traexModelLauncher(s.SessionID, thread) != "" {
		s.Capabilities.Model = proto.Bool(true)
	}
}

func traexModelRefusal(ctx context.Context, code string) error {
	return refusal(code, i18n.T(i18n.From(ctx), "rpc-errors."+code), nil)
}

func traexModelUnconfirmed(ctx context.Context) error {
	return uncertain(errors.New(i18n.T(i18n.From(ctx), "rpc-errors.traex_model_unconfirmed")))
}

// Some launchers advertise launch-time variants containing @effort alongside native model IDs. Only the latter
// can be passed to the live CLI's /model command.
func traexLiveModelID(id string) bool {
	return proto.ValidModelID(id) && !strings.Contains(id, "@")
}

func (n *Node) setTraexModel(ctx context.Context, id string, a host.Agent, model string) error {
	if model == "" {
		return nil
	}
	if !traexLiveModelID(model) {
		return traexModelRefusal(ctx, "traex_model_unavailable")
	}
	if err := n.switchingRefusal(id); err != nil {
		return err
	}
	thread, ok := n.traexThread(id)
	launcher := n.traexModelLauncher(id, thread)
	if !ok || launcher == "" || host.ParsePane(a.PaneID).Kind != host.Herdr {
		return traexModelRefusal(ctx, "traex_model_terminal")
	}
	if a.AgentStatus == proto.StatusWorking || a.AgentStatus == proto.StatusBlocked {
		return traexModelRefusal(ctx, "traex_model_busy")
	}
	catalog, err := n.launcherModels(withStartHost(ctx, host.Herdr), launcher, traexAgent, false)
	if err != nil {
		return err
	}
	if catalog.Locked != "" || !slices.ContainsFunc(catalog.Models, func(m proto.AgentModel) bool { return m.ID == model }) {
		return traexModelRefusal(ctx, "traex_model_unavailable")
	}

	defer n.typing.lock(id)()
	owner := func() bool {
		return n.traeForeground(ctx, a.PaneID) && n.traexLiveThread(a.PaneID, id) == id
	}
	if err := n.switchingRefusal(id); err != nil {
		return err
	}
	if !owner() {
		return traexModelRefusal(ctx, "traex_model_terminal")
	}
	// A draft or an interactive question owns the input. Do not clear it, answer it, or queue a model command.
	text, err := n.readPaneANSI(ctx, a.PaneID, "visible", 80)
	box := parseTraeComposer(text)
	switch {
	case err != nil, !box.open, box.dialog:
		return traexModelRefusal(ctx, "traex_model_terminal")
	case box.busy, box.canQueue, len(box.queued) > 0:
		return traexModelRefusal(ctx, "traex_model_busy")
	case box.draft != "":
		return refusal(screen.InputDraft, i18n.T(i18n.From(ctx), "rpc-errors.traex_model_draft"), nil)
	}
	if strings.EqualFold(n.traexNativeModel(ctx, id), model) {
		return nil
	}
	cmd := "/model " + model
	rejected := "Model '" + model + "' is unavailable"
	seenRejection := strings.Contains(screen.Plain(text), rejected)
	defer n.notifySessionsSoon()
	if err := n.sendText(ctx, a.PaneID, pasteStart+cmd+pasteEnd); err != nil {
		return traexModelUnconfirmed(ctx)
	}
	box, err = n.waitTraeComposer(ctx, a.PaneID, func(b traeComposer) bool { return b.draft == cmd })
	if err != nil || box.busy || box.canQueue || box.dialog || !owner() {
		return traexModelUnconfirmed(ctx)
	}
	if err := n.sendKeys(ctx, a.PaneID, "enter"); err != nil {
		return traexModelUnconfirmed(ctx)
	}

	confirm, cancel := context.WithTimeout(ctx, codexConfirm)
	defer cancel()
	for confirm.Err() == nil {
		if strings.EqualFold(n.traexNativeModel(confirm, id), model) && owner() {
			invalidateCodexPaginated(n.traexHome())
			return nil
		}
		if !seenRejection {
			text, err := n.readPaneANSI(confirm, a.PaneID, "visible", 80)
			if err == nil && strings.Contains(screen.Plain(text), rejected) && owner() {
				return traexModelRefusal(ctx, "traex_model_unavailable")
			}
		}
		select {
		case <-confirm.Done():
		case <-time.After(codexStep):
		}
	}
	return traexModelUnconfirmed(ctx)
}

// The history cache is intentionally shared and can be five seconds old. Confirm the exact live thread's native
// setting directly, without reading any prompt or treating terminal input delivery as a completed change.
func (n *Node) traexNativeModel(ctx context.Context, id string) string {
	if !validID(id) {
		return ""
	}
	db := codexAppDBPath(n.traexHome())
	if db == "" {
		return ""
	}
	var rows []struct {
		Model string `json:"model"`
	}
	if err := querySQLite(ctx, db, `SELECT COALESCE(model, '') AS model FROM threads WHERE id = '`+id+`' AND archived = 0`, &rows); err != nil || len(rows) != 1 {
		return ""
	}
	return rows[0].Model
}
