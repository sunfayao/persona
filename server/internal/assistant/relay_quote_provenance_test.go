package assistant

import (
	"context"
	"testing"
)

func TestRetainedQuoteMixedStoreAfterUpgrade(t *testing.T) {
	ctx := context.Background()
	store := todoTestDB(t)
	book := map[string]relayQuote{
		quoteRef("garden", "unmarked"): {Request: BriefRequest{MessageID: "unmarked", Text: "Plant carrots"}},
		quoteRef("garden", "checked"):  {Request: BriefRequest{MessageID: "checked", Text: "Plant beans"}, SourceChecked: true},
	}
	if err := store.PutSetting(ctx, relayQuotesKey, book); err != nil {
		t.Fatal(err)
	}
	service := New(Options{Hub: newFakeHub(), Store: func() Store { return store }})
	got := service.verifiedQuote(ctx, "garden", "checked")
	if got == nil || got.Text != "Plant beans" || service.verifiedQuote(ctx, "garden", "unmarked") != nil {
		t.Fatalf("mixed retained quotes after restart: checked = %+v", got)
	}
	var durable map[string]relayQuote
	if _, err := store.Setting(ctx, relayQuotesKey, &durable); err != nil || len(durable) != 1 || !durable[quoteRef("garden", "checked")].SourceChecked {
		t.Fatalf("persisted upgrade contains %d quotes, error %v", len(durable), err)
	}
}
