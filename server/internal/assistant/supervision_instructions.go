package assistant

import (
	"context"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistant/autosupervise"
	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// supervisionStartup marks a manager-authorized start before its conversation id exists.
// It is never read from RPC parameters or inferred from a model purpose supplied by a caller.
type supervisionStartup struct{}

// runtimeSessionSupportsSupervisionPolicy reports an explicit capability, never an inference from a launcher name.
func runtimeSessionSupportsSupervisionPolicy(c proto.Session) bool {
	return c.SourceKind == proto.SourceRuntime && c.Capabilities != nil && c.Capabilities.SupervisionPolicy
}

// runtimeSessionLacksSupervisionPolicy reports a session hosted by the runtime protocol whose runtime has not
// advertised supervision_policy support. Absent or unknown capability stays conservatively unsupported (#10338, #11173).
func runtimeSessionLacksSupervisionPolicy(c proto.Session) bool {
	return c.SourceKind == proto.SourceRuntime && !runtimeSessionSupportsSupervisionPolicy(c)
}

// runtimeWithSupervisionPolicy reports a known runtime session with an explicit separate policy channel.
func (s *Service) runtimeWithSupervisionPolicy(ctx context.Context, machine, id string) bool {
	c, ok := s.session(ctx, machine, id)
	return ok && runtimeSessionSupportsSupervisionPolicy(c)
}

// runtimeWithoutSupervisionPolicy reports a known session hosted by a runtime that lacks a separate policy channel.
// Launcher names and agent kinds alone do not identify that execution path.
func (s *Service) runtimeWithoutSupervisionPolicy(ctx context.Context, machine, id string) bool {
	if id == "" {
		return false
	}
	c, ok := s.session(ctx, machine, id)
	return ok && runtimeSessionLacksSupervisionPolicy(c)
}

func (s *Service) supervisionInstructions(ctx context.Context, machine, id string) (string, error) {
	cfg := s.settings(ctx)
	if id == "" || s.isWorkMachine(machine) {
		return "", nil
	}
	main := id == cfg.Main
	if main {
		if machine != cfg.mainRouteMachine() {
			return "", nil
		}
	} else if machine != cfg.MachineID {
		return "", nil
	}
	c, known := s.session(ctx, machine, id)
	if known && runtimeSessionLacksSupervisionPolicy(c) {
		return "", nil
	}
	if !main && !(id == cfg.BackgroundSupervisor && cfg.BackgroundSupervisorMachineID == machine) && !s.isBackgroundCallerOn(ctx, machine, id) {
		// Side chats genuinely supervise their own open tasks (#4669). Sharing the
		// directory, a scheduled purpose, or the ability to plan is not a role.
		if known && (!c.Assistant || c.Space != "") {
			return "", nil
		}
		if owns, err := s.supervisesOpenTodo(ctx, id); err != nil {
			return "", err // unknown ownership must not silently demote a side supervisor
		} else if !owns {
			return "", nil
		}
	}
	// Only a candidate supervisor needs this fail-closed check. An unreadable guest
	// registry must not stop ordinary chats that would receive no role policy anyway.
	if _, guest, err := s.guestOf(ctx, machine, id); err != nil {
		return "", err
	} else if guest {
		return "", nil
	}
	if main && !s.standingPolicy(ctx, machine, id) {
		return "", nil
	}
	return prompts.RenderSupervisionInstructions(), nil
}

func (s *Service) supervisesOpenTodo(ctx context.Context, id string) (bool, error) {
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return false, err
	}
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	for _, todo := range s.todos.todos {
		if todo.DoneAt == nil && todo.Supervision != nil && todo.Supervision.Supervisor == id {
			return true, nil
		}
	}
	return false, nil
}

// standingPolicy: main conversation id carries the supervision policy in its standing text. With supervisor Agents on and
// a node that delivers one-off blocks, one that supervises no open to-do itself when it starts or is compacted carries a
// short section instead, and the policy comes with its first judged signal (supervisionpolicy).
func (s *Service) standingPolicy(ctx context.Context, machine, id string) bool {
	full, err := s.policy.Standing(ctx, machine, id, func() (bool, error) {
		if !s.policyOnDemand(ctx, machine) {
			return true, nil
		}
		return s.mainSupervises(ctx, id)
	})
	return full || err != nil
}

func (s *Service) policyOnDemand(ctx context.Context, machine string) bool {
	return autosupervise.On(s.settings(ctx).TodoSupervisor) && s.noticesReach(machine)
}

// noticesReach: the node of machine puts the hub's one-off context blocks (nodeproto.RuleNotice) into a message.
func (s *Service) noticesReach(machine string) bool {
	nodes, ok := s.physicalDevices().(nodeAnnouncer)
	return ok && nodes.NodeAnnounced(machine, nodeproto.RuleNotice)
}

// mainSupervises: main conversation id follows signals up itself (an open to-do of its own, or the tasks no supervisor has).
func (s *Service) mainSupervises(ctx context.Context, id string) (bool, error) {
	if own := s.settings(ctx).MainSupervisesOwnTasks; own != nil && *own {
		return true, nil
	}
	if id == "" {
		return false, nil
	}
	return s.supervisesOpenTodo(ctx, id)
}

// noteStartupPolicy records what a successful start (err nil) of main conversation id was given (supervision_instructions).
func (s *Service) noteStartupPolicy(ctx context.Context, machine, id string, params map[string]any, err error) {
	if text, ok := params["supervision_instructions"].(string); ok && err == nil && ctx.Value(supervisionStartup{}) == true && s.policyOnDemand(ctx, machine) {
		s.policy.Started(ctx, machine, id, text == "")
	}
}

func (s *Service) putSupervisionInstructions(ctx context.Context, machine, id string, p map[string]any) error {
	// Always overwrite caller input, including empty roles, so demotion clears the node's
	// process fingerprint instead of retaining an earlier conversation's policy.
	text, err := s.supervisionInstructions(ctx, machine, id)
	if err != nil {
		return err
	}
	if id == "" && ctx.Value(supervisionStartup{}) == true && p["guest"] == nil {
		if text = prompts.RenderSupervisionInstructions(); s.policyOnDemand(ctx, machine) {
			if started, err := s.mainSupervises(ctx, id); err == nil && !started {
				text = "" // a new main conversation supervises nothing: the policy comes with its first judged signal
			}
		}
	}
	p["supervision_instructions"] = text
	return nil
}

// supervisionTurn: a supervision message is going into conversation p.SessionID (its context leaves the snapshot out; the
// node keeps what it last showed, so the owner's next message gets the delta), judged says it asks to judge a signal, and
// whole is its digest to tell again when the conversation was compacted since it was prepared. A compaction (p.RecoveryID)
// takes back what the conversation was told of the digests and the policy.
func (s *Service) supervisionTurn(ctx context.Context, p ContextParams) (supervising, judged bool, whole []prompts.SuperviseBlock) {
	if p.RecoveryID != "" {
		whole = s.digests.Compacted(p.SessionID)
		if s.isMain(ctx, p.SessionID) {
			s.policy.Compacted(ctx, p.MachineID, p.SessionID)
		}
	}
	supervising, judged = s.digests.Going(p.SessionID)
	return supervising, judged, whole
}

// withSupervisionNotices puts into out what a supervision message owes besides the digest: the full policy once and the
// digest whole after a compaction; the snapshot stays out.
func (s *Service) withSupervisionNotices(ctx context.Context, p ContextParams, out ContextResult, supervising, judged bool, whole []prompts.SuperviseBlock) ContextResult {
	if !supervising {
		return out
	}
	out.Text = ""
	if !s.noticesReach(p.MachineID) || s.isGuest(ctx, p.MachineID, p.SessionID) {
		return out
	}
	notices := []string{}
	if len(whole) > 0 {
		notices = append(notices, prompts.RenderDigestWhole(whole))
	}
	if judged && s.isMain(ctx, p.SessionID) {
		if text, receipt, owed := s.policy.Notice(ctx, p.MachineID, p.SessionID, prompts.RenderSupervisionPolicyNotice()); owed {
			notices, out.Receipts = append(notices, text), append(out.Receipts, receipt)
		}
	}
	out.RuleNotice = strings.TrimSpace(strings.Join(append([]string{out.RuleNotice}, notices...), "\n"))
	return out
}
