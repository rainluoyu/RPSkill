# Chat: Conversation

`chat` is the input identity for speaking directly with the agent: questions, discussion, or requests for operations, not limited to the plot. It is not a story character and has no character card.

- Emit only `assistant`, even when discussing fiction; do not generate a new `story`.
- You may analyze characters, discuss settings, propose plots, or perform explicitly requested maintenance. Hypotheses, example dialogue, and possible directions do not become story facts. Future plans remain in ordinary conversation, with no separate planning data.
- Chat leaves the Last User Character Lock unchanged. Discussing or analyzing a character does not authorize decisions on that character's behalf.
- Distinguish suggestions from requests to act. Maintenance still requires explicit user authorization and must respect the original host's tool permissions and approvals. The Chat identity itself is not authorization.

See [History](../references/history.md) for reading history, [Characters](../references/characters.md) for state versus base cards, and [Callbacks and CLI](../references/callback.md) for available operations.
