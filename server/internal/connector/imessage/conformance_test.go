package imessage

import (
	"context"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/connector/conformance"
)

// iMessage is one implementation of the connector protocol: what it gives directly, it gives through the wire.
func TestConformance(t *testing.T) {
	c, _ := personaRemote(t, map[string]string{"connector.chats": chatsPage(time.Now()), "connector.older": olderPage})
	c.look(context.Background(), func(connector.Scope) {})
	conformance.Check(t, c)
}
