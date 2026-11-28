# Asking the owner with `ask_user`

The tool's description holds what every call needs. This page holds the rest: how long a question waits, what happens when nobody answers, deadlines and `wait: false`. Each result says what happened and what to do next; never guess an answer.

## The card

- One to four questions, each with two to four options; `multiSelect` lets the owner pick several. `header` is a short label of at most 12 characters. Two questions with the same text are refused: answers come back by question.
- The card shows in this conversation in Persona, with a notification. The owner can also type an answer of their own instead of an option.
- Ask through this tool: your runtime's own question tool does not reach the owner.

## How long it waits

- A waiting call holds this turn for up to 2 hours.
- If the owner writes in this conversation instead of picking an option, the question is withdrawn and the call returns at once. End the turn: their message comes next, and it may be the answer.
- Anything else sent to this conversation waits behind a waiting call. Once such a message waits and the question has been up for a couple of minutes, the call returns without the answer and the card stays up, as with `wait: false`. End the turn then.
- If nobody answers in time, the question becomes a to-do that waits for the owner's call (in their to-do list and the next briefing). Do not ask it again, and do not go ahead with what you asked about; their answer comes to this conversation later as a new message.

## `wait: false`

The card goes up the same way, but the call returns at once. Finish what does not depend on the answer and end the turn without guessing it. The answer comes later as a new message that quotes the question and the answer, or says the card was closed unanswered, or expired and became a to-do. Use it only where your instructions say to: a confirmation this same turn needs (approving a message before it is sent) keeps the default.

## Deadlines

- `deadline`: until when the questions wait, a duration (`30m`, `4h`) or an RFC 3339 time, from 1 minute to 30 days ahead. With a deadline, every question names its recommended option in `recommend` (one of its labels), and `reversible` says whether going ahead with the recommendations can be undone.
- If nobody answers by the deadline and the decision is reversible, the recommended options are taken by default and recorded on the question's to-do; go ahead with them and tell the owner in a line.
- Deleting, sending anything to other people, spending money and changing production cannot be undone: mark them `reversible: false`. Such a question is never decided by default: at its deadline the owner is reminded again, and it keeps waiting.
