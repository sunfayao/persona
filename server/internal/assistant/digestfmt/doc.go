// Package digestfmt renders a supervision message, the one a conversation and a to-do's supervisor Agent both get: one
// block per to-do, and only what the reader has not been told already.
//
// A message about a to-do the reader was told of before repeats only what changed since (a new signal, a criterion line
// that changed, new last words) with one line that says the rest is unchanged. What a conversation has been told is a
// small Book, in memory: a restart of the manager or a compaction of the conversation forgets it, and the next block is
// whole again, which costs one whole block. The book commits what a message said only once the message went in
// (Delivered), so a message that was queued, replaced or refused tells nothing.
//
// Every block is cut to about BlockBytes, and the message to Budget, at the Agent's last words first, with a line that
// names the tool call that returns the rest. Criteria are never cut and never joined.
package digestfmt
