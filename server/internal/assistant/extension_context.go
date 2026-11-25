package assistant

import ()

// What extensions wrote for the prompts this manager builds (docs/extensions/extensions.md "Assistant content from
// the data directory"): a project's background for a new Agent, and the Inbox's triage and drafts with the sentences a
// draft must not repeat. The extension manager reads them from the extensions' data directories; an older one that
// does not know the calls, or none at all, is no context. The Inbox's half, the never-quote check with it, is in
// inbox_extension_context.go.
