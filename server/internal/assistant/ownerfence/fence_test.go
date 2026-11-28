package ownerfence

import (
	"strings"
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

func TestShowObservedHidesRelayedInputsAndClips(t *testing.T) {
	t.Parallel()
	var book Book
	main, worker := Ref{"hub", "main"}, Ref{"dev", "worker"}
	ctx := WithObserver(t.Context(), main)
	if got := book.ShowObserved(ctx, worker); got != "" {
		t.Fatalf("nothing recorded: %q", got)
	}
	relayed := book.Record(worker, Input{At: time.Unix(5, 0), Text: "what the owner said in main", ByOwner: true, RelayOf: 7})
	if got := book.ShowObserved(ctx, worker); got != "" {
		t.Fatalf("a relayed input is shown: %q", got)
	}
	if got := book.Action(main, worker); got.Observed != relayed.Seq {
		t.Fatalf("the relayed input no longer counts as read: %+v", got)
	}
	if !book.LastDirect(worker).IsZero() {
		t.Fatal("a relayed input counts as direct owner input")
	}
	typed := "<persona-context>x</persona-context>" + strings.Repeat("y", 300) + "<persona-context>"
	book.Record(worker, Input{At: time.Unix(9, 0), Text: typed, ByOwner: true})
	got := book.ShowObserved(ctx, worker)
	if !strings.Contains(got, strings.Repeat("y", 200)+"…") || strings.Contains(got, strings.Repeat("y", 201)) || strings.Contains(got, "<persona-context>x") {
		t.Fatalf("shown: %q", got)
	}
	if strings.Count(got, "<persona-context>") != strings.Count(got, "</persona-context>") {
		t.Fatalf("an unclosed context tag: %q", got)
	}
	if !book.LastDirect(worker).Equal(time.Unix(9, 0)) {
		t.Fatalf("LastDirect = %v", book.LastDirect(worker))
	}
}
