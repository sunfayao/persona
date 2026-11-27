package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func recoveryReceipts(t *testing.T, s *Service, id string) []receiptBody {
	t.Helper()
	items, err := s.feedItems(context.Background(), "conv1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var receipts []receiptBody
	for _, item := range items {
		if item.Kind == feedReceipt && item.Ref == id {
			var body receiptBody
			if err := json.Unmarshal(item.Body, &body); err != nil {
				t.Fatal(err)
			}
			receipts = append(receipts, body)
		}
	}
	return receipts
}
