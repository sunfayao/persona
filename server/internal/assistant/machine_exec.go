package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/machineexec"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// machine_exec (assistant/machineexec): one quick command on one of the user's computers through its node. The root
// gives the domain the owner's setting (its own key in the settings table, changed through persona_settings), the
// computers (machineByName), the caller's class, the approval card and the node call; the hub audits the call.

// machineExecCardWait is how long a command's approval card waits for the user.
const machineExecCardWait = 10 * time.Minute

func (s *Service) toolMachineExec(ctx context.Context, machine, self string, args json.RawMessage) (string, error) {
	x := machineexec.New(machineexec.Dependencies{
		Setting: s.machineExecSetting,
		Resolve: func(ctx context.Context, name string) (machineexec.Machine, bool) {
			d, ok := s.machineByName(ctx, name)
			return machineexec.Machine{ID: d.ID, Name: d.Name, SSHHost: d.SSHHost, Online: d.Online || d.Reachable}, ok
		},
		Known:   s.machineIDs,
		Confirm: s.machineExecCard,
		Call: func(ctx context.Context, machine string, p proto.MachineExecParams, out *proto.MachineExecResult) error {
			return s.call(ctx, machine, proto.MethodMachineExec, p, out)
		},
	})
	return x.Run(ctx, s.machineExecCaller(ctx, machine, self), args)
}

// machineExecCaller classes the calling conversation as the dispatch policy does, inside the bounds every one of these
// tools has where a model calls it (mcpCall: a trusted caller, not a guest, not an Inbox conversation), whatever path
// the call came by; outside them it has no class, and machine_exec refuses it.
func (s *Service) machineExecCaller(ctx context.Context, machine, session string) machineexec.Caller {
	c := machineexec.Caller{Machine: machine, Session: session}
	if session != "" && s.assistantCaller(ctx, machine, session) && !s.isGuest(ctx, machine, session) &&
		!s.isInboxModelCall(ctx, machine, session, "") {
		c.Class = s.dispatchConversationClass(ctx, machine, session)
	}
	return c
}

// machineExecSetting is assistant.machine_exec in effect: what the owner saved, else on, every computer, no card.
func (s *Service) machineExecSetting(ctx context.Context) (machineexec.Setting, error) {
	v := machineexec.DefaultSetting()
	if st := s.store(); st != nil {
		if _, err := st.Setting(ctx, machineexec.SettingKey, &v); err != nil {
			return v, err
		}
	}
	if v.Machines == nil {
		v.Machines = []string{}
	}
	return v, nil
}

// machineExecField is the setting in persona_settings (registry kindJSON): changed on the usual approval card.
func (s *Service) machineExecField() settingField {
	return settingField{key: machineexec.SettingKey, kind: kindJSON,
		about: "machine_exec: enabled (true by default); confirm (false by default; true shows the user each command on a " +
			"card to approve first); machines (ids or names it may use; [] means every connected computer)",
		value:  `the whole object {"enabled": true|false, "confirm": true|false, "machines": [ids or names]}`,
		decode: func(raw json.RawMessage) (any, error) { return machineexec.DecodeSetting(raw) },
		get:    func(ctx context.Context) (any, error) { return s.machineExecSetting(ctx) },
		write: func(ctx context.Context, val any) error {
			st := s.store()
			if st == nil {
				return errors.New("no database on this manager")
			}
			return st.PutSetting(ctx, machineexec.SettingKey, val)
		}}
}

// machineExecCard asks the user whether a command may run, and waits for the answer.
func (s *Service) machineExecCard(ctx context.Context, c machineexec.Confirmation) (proto.Resolution, error) {
	return s.ask(ctx, s.machineExecInteraction(ctx, c, time.Now().UTC()))
}

// machineExecInteraction is the approval card made at now, as secret_run's is without the password: the computer
// (with its id when another has its name), the command verbatim and its directory.
func (s *Service) machineExecInteraction(ctx context.Context, c machineexec.Confirmation, now time.Time) proto.Interaction {
	l := s.lang(ctx)
	name := secretRunMachineLabel(Device{ID: c.Machine.ID, Name: c.Machine.Name, SSHHost: c.Machine.SSHHost}, s.devices(ctx))
	summary := i18n.T(l, "assistant-machine_exec.card", i18n.Args{"machine": name}) + "\n$ " + c.Command
	if c.Cwd != "" {
		summary += "\n" + i18n.T(l, "assistant-machine_exec.card_cwd", i18n.Args{"cwd": c.Cwd})
	}
	input, _ := json.Marshal(map[string]any{"machine": c.Machine.ID, "machine_name": name, "command": c.Command,
		"cwd": c.Cwd, "timeout_seconds": int(c.Timeout / time.Second)})
	return proto.Interaction{ID: "ix_" + randomHex(6), MachineID: c.Caller.Machine, SessionID: c.Caller.Session,
		SessionTitle: "Persona", Kind: proto.IxPermission, ToolName: machineexec.Tool, ToolInput: input, Summary: summary,
		CreatedAt: now, ExpiresAt: now.Add(machineExecCardWait)}
}
