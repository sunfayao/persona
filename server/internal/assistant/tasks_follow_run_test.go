package assistant

import (
	"testing"
)

// A turn's receipt id hashes what it hashed before receipts named their run: a manager upgraded between a turn's
// receipt and a second look at that turn finds the receipt under the same id and writes no second one.
func TestReceiptIDKeepsItsInputs(t *testing.T) {
	if got := receiptID("todo1", taskTurn{Machine: "box", Session: "s1", Key: "turn-7"}); got != "feed_turn_6ea0311ff3f9e31dc14e" {
		t.Fatalf("receiptID = %q", got)
	}
}
