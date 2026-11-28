// Package ownerattention projects owner-facing attention without changing the worker's supervision state.
package ownerattention

import "github.com/sunfayao/persona/server/internal/proto"

type Cards struct {
	Questions int
	OwnerOnly int
}

func Project(row *proto.Session, managed bool, cards Cards, ownerQuestions int, mainDecisions bool) bool {
	if ownerQuestions > 0 || mainDecisions {
		row.OwnerNeedsYou = true
		row.State = proto.StateNeedsYou
		row.PendingInteractions += ownerQuestions
		return true
	}
	waitingForAnswer := row.AwaitsReply || row.State == proto.StateNeedsYou && row.TurnError == "" || row.Work != nil && row.Work.On == proto.WaitUser &&
		row.Work.Link == nil && len(row.Work.Questions) > 0
	if !managed || cards.OwnerOnly > 0 || cards.Questions == 0 && !waitingForAnswer {
		return false
	}
	row.PendingInteractions = 0
	row.AwaitsReply = false
	if row.State == proto.StateNeedsYou || row.State == proto.StateIdle {
		row.State = proto.StateWorking
	}
	return true
}
