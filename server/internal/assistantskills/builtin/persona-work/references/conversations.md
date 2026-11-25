# Persona conversations

Besides the main conversation, Persona keeps conversations for scheduled runs, Inbox items and channels. Find one with `conversations_list`; if several match, resolve which one the person means before acting.

Read recent exchanges with `conversation_read`. Use `conversation_send` to deliver an actual message to an existing conversation; a queued receipt means it is already scheduled for delivery. Follow the conversation policy if a write is refused.
