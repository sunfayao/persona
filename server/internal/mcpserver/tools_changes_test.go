package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLeaderOldNodeKeepsOneShotDiscovery(t *testing.T) {
	fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[{"name":"report_to_supervisor"}]}`), nil })
	result, err := handle(context.Background(), "test", listRequest())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "report_to_supervisor") {
		t.Fatalf("old node list: %s", b)
	}
}
