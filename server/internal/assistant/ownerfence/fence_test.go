package ownerfence

import (
	"testing"
	"time"
)

func TestOwnerInputFencesPreparedAction(t *testing.T) {
	var book Book
	main, leader, worker := Ref{"hub", "main"}, Ref{"dev", "leader"}, Ref{"dev", "worker"}
	start := time.Unix(1, 0)
	old := book.Record(worker, Input{At: start, Text: "previous input", ByOwner: true})
	book.Observe(main, worker, old)
	book.Observe(leader, worker, old)
	action := book.Action(leader, worker)

	ownerArrived := make(chan Input, 1)
	go func() {
		ownerArrived <- book.Record(worker, Input{At: start.Add(time.Second), Text: "hourly", ByOwner: true})
	}()
	current := <-ownerArrived
	if got := book.Admit(action, Input{}); got != current {
		t.Fatalf("delivery after the owner arrived: %+v, want %+v", got, current)
	}
	book.Observe(main, worker, current)
	book.Observe(main, worker, old)
	if got := book.Admit(book.Action(main, worker), Input{}); got.Seq != 0 {
		t.Fatalf("fresh main read did not advance: %+v", got)
	}
	if got := book.Admit(book.Action(leader, worker), Input{}); got != current {
		t.Fatalf("main observation leaked to leader: %+v", got)
	}
}

func TestRelayedMainDecisionInvalidatesTheLeadersAction(t *testing.T) {
	var book Book
	main, leader, worker := Ref{"hub", "main"}, Ref{"dev", "leader"}, Ref{"dev", "worker"}
	leadAction := book.Action(leader, worker)
	mainAction := book.Action(main, worker)
	mainAction.Owner = book.Record(main, Input{Text: "hourly", ByOwner: true})
	if got := book.Admit(mainAction, mainAction.Owner); got.Seq != 0 {
		t.Fatalf("main relay refused: %+v", got)
	}
	if got := book.Admit(leadAction, Input{}); got.Text != "hourly" {
		t.Fatalf("later leader action missed the relayed decision: %+v", got)
	}
}
