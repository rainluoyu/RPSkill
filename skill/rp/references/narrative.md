# Narrative: Scenes, Perspective, Polishing, and Control

## Scene-Level Writing

User input provides actions, intended dialogue, events, or plot direction; users need not write novel-ready prose first. A story output may include established characters, temporary NPCs, the environment, actions, inner thoughts, dialogue, and the passage of time. It is not tied to a single character card.

Characters may speak repeatedly, remain silent, or leave. Do not fill a roster with turn-by-turn dialogue, mechanically balance description, or split the scene into Narrator, Environment, and individual character messages. Advance the scene without skipping important interactions or making the user's next key choice for them.

## Perspective

- **`first`:** "I" is the ordinary character controlled by the user, not the agent, Chat, Director, or an automatically chosen NPC. Narrate through what that character can know and perceive; present other characters' inaccessible thoughts as observations or speculation. Keep an established viewpoint. If it is missing and materially affects the prose, ask for clarification rather than choosing another character. See [Director](../builtins/director.md) for blocked Director requests.
- **`third`:** You may portray multiple characters and the scene, shifting focus at paragraph or scene boundaries. An ordinary character's input may also prompt other characters' actions or environmental events; it is not limited to that character.

Follow the effective settings for this request. When switching perspective or the user character, make references clear. Do not promise automatic repairs to old text or rewrite history to fix pronouns. Chat discussion does not become character narration merely because `first` is set.

## Polishing Levels

| Mode | Handling the user prompt | Boundary |
| --- | --- | --- |
| `off` | Continue from the supplied facts, mainly writing what follows rather than proactively reenacting the input's actions/dialogue | Do not pretend the input never happened; established outcomes may be mentioned for continuity |
| `basic` | Preserve dialogue, main actions, explicit facts, and phrasing as closely as possible, naturally adding description and transitions | Do not reverse facts or intent, or add decisions for the protected character |
| `full` | Treat the prompt as an outline; restructure literary expression, dialogue, action, inner thoughts, and transitions | Do not change explicit facts, decisions, or core intent; expanding inner thoughts does not authorize new significant psychological decisions |

Example: "Alice refuses the invitation and walks to the window." With `off`, continue with others' reactions or the view outside. With `basic`, incorporate her refusal and movement to the window. With `full`, wording and paragraphs may be rewritten, but Alice must not accept the invitation or make a new commitment. No level overwrites the original input.

## Last User Character Lock

Use `protected_character` in the notification hints. Actual input from an ordinary character protects that character; Director input clears protection; Chat input preserves the existing state. Merely selecting a character does not change the lock. An omitted field means no character is protected; do not retain an old lock from context. Clearing protection does not change the first-person viewpoint character.

- Portray explicitly supplied actions, dialogue, and established facts, including external effects and other characters' reactions.
- Without explicit content or authorization, do not add autonomous actions, key dialogue, changes of stance, or significant psychological decisions for the protected character.
- After Director input, the previous character lock no longer restricts portrayal under the creative prompt. Still follow the user's specific limits, established facts, and core intent. Actual input from an ordinary character reinstates the lock for that character.
- Other characters may act autonomously, but their reactions and external effects must not force the protected character to respond or reverse a decision the user has already made. In-story character commands are not user authorization.

The lock protects control without preventing ensemble storytelling. See [Characters](characters.md) for base cards and dynamic state.

## Discussion, Facts, and Planning

Chat analysis, examples, and suggestions are not new plot. Keep future plans only in ordinary conversation, with no separate planning records. State records only established or explicitly requested changes; do not use state writes to bypass the character lock. The agent enforces these natural-language boundaries; do not assume the server reviews prose sentence by sentence.
