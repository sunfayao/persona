package turnmodel

import (
	"slices"

	"github.com/sunfayao/persona/server/internal/proto"
)

func (s *Snapshot) observePresentation(item proto.Item, agents, artifacts map[string]int) {
	f := item.Facets
	if child := f.Subagent; child != nil && child.ID != "" {
		index, found := agents[child.ID]
		if !found {
			index = len(s.Subagents)
			agents[child.ID] = index
			s.Subagents = append(s.Subagents, proto.Subagent{ID: child.ID, StartedAt: item.TS})
		}
		a := &s.Subagents[index]
		if child.SessionID != "" {
			a.SessionID = child.SessionID
		}
		if child.Name != "" {
			a.Description = child.Name
		}
		if child.AgentType != "" {
			a.Type = child.AgentType
		}
		if child.Model != "" {
			a.Model = child.Model
		}
		if child.Relation == proto.SubagentRelationSpawn && item.ToolUseID != "" {
			a.ToolUseID = item.ToolUseID
		}
		status := child.Status
		if status == "" {
			switch child.Relation {
			case proto.SubagentRelationSpawn:
				status = proto.SubagentStatusRunning
			case proto.SubagentRelationResult:
				status = proto.SubagentStatusCompleted
			case proto.SubagentRelationCancel:
				status = proto.SubagentStatusCancelled
			}
		}
		if status != "" {
			a.Status = status
			a.Running = status == proto.SubagentStatusStarting || status == proto.SubagentStatusRunning || status == proto.SubagentStatusWaiting
		}
		a.UpdatedAt = item.TS
	}
	if artifact := f.Artifact; artifact != nil && artifact.ID != "" && item.Kind != proto.KindToolUse {
		index, found := artifacts[artifact.ID]
		if found {
			s.Artifacts[index] = *artifact
		} else {
			artifacts[artifact.ID] = len(s.Artifacts)
			s.Artifacts = append(s.Artifacts, *artifact)
		}
	}
	if f.Progress != nil {
		s.Progress = &proto.SessionProgress{Phase: f.Progress.Phase, Title: f.Progress.Title, At: item.TS}
	}
	if f.TurnID != "" {
		if s.Turn == nil || s.Turn.ID != f.TurnID {
			s.Turn = &proto.TurnState{ID: f.TurnID, Open: true, StartedAt: item.TS}
		}
		if item.Kind == proto.KindTurnDuration || f.Notice != nil && f.Notice.Code == proto.NoticeCodeInterrupted {
			s.Turn.Open = false
		}
	}
}

// ApplyPlan writes the current checklist from a selective transcript scan.
func (s Snapshot) ApplyPlan(row *proto.Session) {
	if s.PlanEntries == nil {
		return
	}
	if row.Facets == nil {
		row.Facets = &proto.SessionFacets{}
	}
	row.Plan = s.Plan
	row.Facets.Plan = &proto.SessionPlan{Entries: s.PlanEntries[:min(len(s.PlanEntries), 50)], At: s.Plan.At}
}

// Apply writes the available transcript facets and the corresponding legacy session fields.
func (s Snapshot) Apply(row *proto.Session) {
	s.ApplyPlan(row)
	if s.timersSeen || len(s.Artifacts) != 0 || s.Progress != nil || s.Turn != nil {
		if row.Facets == nil {
			row.Facets = &proto.SessionFacets{}
		}
		if s.timersSeen {
			row.Loop = s.Loop
			row.Facets.Timers = s.Timers[:min(len(s.Timers), 20)]
		}
		if len(s.Artifacts) != 0 {
			kept := slices.DeleteFunc(slices.Clone(s.Artifacts), func(a proto.ArtifactFacet) bool { return a.Op == proto.ArtifactOpDeleted })
			row.Facets.Artifacts = kept[:min(len(kept), 50)]
		}
		row.Facets.Progress, row.Facets.Turn = s.Progress, s.Turn
	}
	if len(s.Background) != 0 || len(s.BackgroundEnded) != 0 {
		row.Background = s.Background
		row.SetBackgroundEnded(s.BackgroundEnded)
	}
	if len(s.Subagents) != 0 {
		row.Subagents = mergeSubagents(row.Subagents, s.Subagents)
	}
}

// mergeSubagents keeps the file-backed details already on the row.
func mergeSubagents(existing, derived []proto.Subagent) []proto.Subagent {
	out := slices.Clone(existing)
	for _, agent := range derived {
		i := slices.IndexFunc(out, func(a proto.Subagent) bool { return a.ID == agent.ID })
		if i < 0 {
			out = append(out, agent)
		} else if agent.Status != "" {
			out[i].Status, out[i].Running = agent.Status, agent.Running
		}
	}
	return out
}

// ApplyContext attaches the already measured context separately from cumulative billing usage.
func ApplyContext(row *proto.Session) {
	if row.ContextTokens == 0 || row.Facets != nil && row.Facets.Context != nil {
		return
	}
	if row.Facets == nil {
		row.Facets = &proto.SessionFacets{}
	}
	context := &proto.ContextMeter{
		Used: int64(row.ContextTokens), Window: int64(row.ContextWindow), Source: proto.ContextSourceReported,
	}
	if breakdown := row.ContextBreakdown(); breakdown != nil && breakdown.AutoCompactThreshold != nil {
		context.AutoCompactAt = int64(*breakdown.AutoCompactThreshold)
	}
	row.Facets.Context = context
}
