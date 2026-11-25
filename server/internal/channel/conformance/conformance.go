// Package conformance holds a channel program to the channel extension point's protocol (docs/extensions/channels.md "Protocol"),
// after the plugin protocol's own checks (plugin/conformance): every channel the manifest contributes answers
// channel.send (sent, or cannot_send while there is nowhere to send to) the same way for the same ref, takes
// channel.typing, refuses a channel it does not have and a method it does not know; what both sides send holds to
// schema/extension/channel.schema.json.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/extension"
	pconf "github.com/nwverse/persona/server/internal/extension/conformance"
	"github.com/nwverse/persona/server/internal/rpc"
)
