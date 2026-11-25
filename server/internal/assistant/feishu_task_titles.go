package assistant

import (
	"context"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

func feishuSessionNames(sess proto.Session, preferTab bool) (label, secondary string) {
	if tab := sess.DisplayTabName(preferTab); tab != "" {
		if title := transcript.Title(sess.Title); preferTab && title != tab {
			secondary = clip(oneLine(title), 70)
		}
		return tab, secondary
	}
	return agentLabel(sess), ""
}

// feishuTaskPrefix leads a task's line in a Feishu card or report.
const feishuTaskPrefix = "[Persona] "

// feishuAgentPlace identifies the session even when the node supplied no title.
func (s *Service) feishuAgentPlace(ctx context.Context, ix proto.Interaction) (place, secondary string) {
	agent, machine, secondary := s.feishuAgentPlaceParts(ctx, ix)
	if machine == "" {
		return agent, secondary
	}
	return agent + " · " + machine, secondary
}

// feishuAgentPlaceParts is feishuAgentPlace's parts: the agent, its computer ("" when it says none), and the session's
// other name when the agent goes by its herdr tab name.
func (s *Service) feishuAgentPlaceParts(ctx context.Context, ix proto.Interaction) (agent, machine, secondary string) {
	agent = transcript.Title(ix.SessionTitle)
	sess, listed := s.nodeSession(ix.MachineID, ix.SessionID)
	if listed {
		if m, found := s.fleetMachine(ix.MachineID); found {
			sess.Assistant = m.isAssistant(sess)
		}
		if sess.HerdrTabName() != "" {
			preferTab := s.useHerdrTabName(ctx)
			if sess.DisplayTabName(preferTab) != "" {
				agent, secondary = feishuSessionNames(sess, preferTab)
			}
		}
	}
	if agent == "" && ix.Kind == proto.IxScreen {
		if listed {
			agent = agentTitle(&sess, 60)
		}
		if agent == "" || agent == "." {
			agent = short(ix.SessionID)
		}
	}
	if agent == "" {
		agent = "Persona"
	}
	agent = clip(oneLine(agent), 70)
	if ix.Kind == proto.IxScreen {
		if m, ok := s.fleetMachine(ix.MachineID); ok && m.Name != "" {
			return agent, clip(oneLine(m.Name), 40), secondary
		}
	}
	if d, ok := s.device(ctx, ix.MachineID); ok && d.Name != "" && agent != "Persona" {
		return agent, clip(oneLine(d.Name), 40), secondary
	}
	if ix.Kind == proto.IxScreen && ix.MachineID != "" {
		return agent, clip(oneLine(ix.MachineID), 40), secondary
	}
	return agent, "", secondary
}
