# Characters: Base Cards and Story-Specific Dynamic State

## Base Cards

Ordinary characters use `characters/<character-name>/card.md`. The folder name is both the character's name and reference identifier; Chinese names are supported. Frontmatter contains only `enabled` and optional `avatar`, not `id`/`name`. The avatar path is relative to the character directory:

```markdown
---
enabled: true
avatar: avatar.png
---

# Identity and Personality
A 22-year-old human swordsman; calm and cautious.
```

Reference only avatars that actually exist. The current story's settings control character availability; a file's existence does not make its character selectable. Disabling a character does not delete their history. `chat` and `director` are special input identities; `assistant` and `story` are output kinds. None of these four, nor Narrator/Environment, receive ordinary character cards.

Read participating characters' base cards as the plot requires, together with their effective state in this story. Reuse unchanged content that remains in context. See [Narrative](narrative.md) for the character lock and portrayal limits: Alice's actual input followed by Chat still protects Alice; Director input clears protection; Bob's actual input then protects Bob.

## Dynamic State

Experiences, relationships, injuries, possessions, abilities, knowledge, secrets, and psychological changes belong to the current story. Maintain one complete Markdown document per character per story, not state keys or arrays of JSON operations.

Read `chats/<chat-id>/characters/<character-name>.md` directly; a missing file means no state document exists yet. State is not stored in history. Do not read other stories' state or write it back to base cards. Legacy message `meta` and key-value `operations` are not supported.

Before updating, read the current state, preserve what remains valid, revise what has changed, and submit the complete document. Record only established state or state explicitly requested by the user. Distinguish speculation, character misunderstandings, and objective facts; identify who holds secrets or knowledge. State updates must not make unauthorized decisions for a protected character.

Use this server instance's credentials for the following subcommands, explicitly passing `--project`, `--chat`, and `--json`, with the server connection supplied through `--connection`. See [Callbacks and CLI](callback.md) for full invocation binding:

| Intent | Subcommand |
| --- | --- |
| Read the current state document | `character state "Character name"` |
| Save the complete state document | `character dynamic update "Character name" --file state.md` |
| Clear the state document | `character dynamic delete "Character name"` |

`state.md` is a temporary UTF-8 Markdown file; no frontmatter is needed. The server atomically saves a separate Markdown file; clearing state deletes that file. State can be maintained even in an empty story, independently of input/output. Queries return the current document, not a snapshot at a historical point.

## Shared Settings and Media Maintenance

`character update "Character name" --file card.md` changes the complete base card, not dynamic state; `character create "Character name" --file card.md` creates a lasting character. Do this only when the user explicitly requests such maintenance, explaining that it may affect multiple stories. Do not automatically change cards during ordinary continuation. Availability is not user authorization.

Temporary NPCs may appear without cards. Manage avatars and character media only when explicitly requested. See [Callbacks and CLI](callback.md) for actual generation, storage, and referencing of story media.
