package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A persona_settings change is the user's to approve (#3722): only their own allow applies it and only their own deny
// is a decline. A card that expires unanswered, is closed, or that someone else answers for them changes nothing and
// says nobody answered, so the conversation never reports that the user refused.

const interjectKey = "assistant.interject_background"
