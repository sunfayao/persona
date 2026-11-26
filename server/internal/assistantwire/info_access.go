package assistantwire

import "encoding/json"

// MethodAssistantExtensionRead is one of assistant.conversations, assistant.messages and assistant.memory asked for
// an extension, the extension manager saying which (AssistantExtensionRead): what is shown is decided by that
// extension's trust and grants. The plain methods are read as a third party's with no grant.
const MethodAssistantExtensionRead = "assistant.extension_read"

// AssistantExtensionRead is assistant.extension_read's params. Extension is the extension manager's word, never the
// extension's own.
type AssistantExtensionRead struct {
	Extension string          `json:"extension"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// MethodExtensionTrustForget is ExtensionTrustForget → {}: an extension was removed, and what the user let it read
// goes with it (installed again, under the same id or not, it starts as a third party's with no grant). An older
// assistant answers "unknown method".
const MethodExtensionTrustForget = "assistant.extension_trust_forget"

// ExtensionTrustForget is assistant.extension_trust_forget's params.
type ExtensionTrustForget struct {
	Extension string `json:"extension"`
}

// Trust recorded for an extension.
const (
	// ExtensionTrustOwner: the user's own extension (such as their memory extension) reads everything.
	ExtensionTrustOwner = "owner"
	// ExtensionTrustThirdParty: a third party's reads sensitive information only of the categories it was granted.
	ExtensionTrustThirdParty = "third_party"
)
