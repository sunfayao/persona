# Persona conversations

Besides the main conversation, Persona keeps conversations for scheduled runs, Inbox items and channels. Find one with `agents_list` (`kind: "conversations"`); if several match, resolve which one the person means before acting.

Read recent exchanges with `agent_read` (`conversation`). Use `conversation_send` to deliver an actual message to an existing conversation; a queued receipt means it is already scheduled for delivery. Follow the conversation policy if a write is refused.
