package subscriptions

import (
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// The kind descriptions are a prompt for the triage model: English whatever the owner's language, naming the subscription.
func TestSubscriptionKindDescriptionsAreEnglish(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.ZhHans, i18n.En, i18n.Ja} {
		for kind, text := range kindsOf("Example #42", lang) {
			if !strings.Contains(text, "Example #42") || !strings.Contains(text, "ubscription") {
				t.Fatalf("%s %s: %s", lang, kind, text)
			}
		}
	}
}
