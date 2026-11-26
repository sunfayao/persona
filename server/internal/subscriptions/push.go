package subscriptions

import "net/http"

// These identifiers let the web app display and manage saved Shortcuts subscriptions and their runs.
// PushConnector is a push source's connector: reserved, as no extension or connector id starts with "-".
const PushConnector = "-shortcuts"

// TriggerPush is a push's run in a subscription's history.
const TriggerPush = "push"

// CodePushSource refuses what only a pulled subscription has (a recipe, a schedule, Run Now, drafting).
const CodePushSource = "push_source"

// IsPush says whether sub is a push source.
func IsPush(sub Subscription) bool { return sub.Connector == PushConnector }

func pushOnly(what string) *Error {
	return errorf(http.StatusBadRequest, CodePushSource, "a push source has no "+what+": its device sends its items")
}
