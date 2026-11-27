package hub

import (
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// hello publishes the limits the send routes check.
func TestHelloPublishesImageLimits(t *testing.T) {
	b, err := json.Marshal((&Hub{cfg: &Config{}}).hello()["image_limits"])
	if err != nil {
		t.Fatal(err)
	}
	var got proto.ImageLimitsTable
	if err := json.Unmarshal(b, &got); err != nil || got.ImageLimits != proto.DefaultImageLimits || len(got.Agents) != 0 {
		t.Fatalf("hello's image_limits: %s", b)
	}
	if want := `{"request_bytes":33554432,"image_bytes":3750000,"max_side":1600,"per_message":20}`; string(b) != want {
		t.Fatalf("hello's image_limits: %s, want %s", b, want)
	}
}
