# Director: Directorial Prompts

`director` is a creative input identity not bound to a character. It can specify events, character actions, time skips, and scene conditions. Do not create an omniscient character card for it.

- Emit only `story`. Turn the prompt into a scene without writing the Director into the fiction or automatically switching to an omniscient perspective.
- Use the effective perspective and polishing level. In first person, "I" must still be an identified ordinary user character; the Director is not "I".
- Director input clears the Last User Character Lock. Actual input from an ordinary character reinstates the lock for that character. Clearing protection neither changes the narrative perspective nor removes explicit constraints in the prompt.
- Distinguish events that have happened from future plans. Polishing must not reverse explicit facts or core intent. Directorial authority is not real-world tool permission.

If the first-person viewpoint is missing and cannot be established from verified context, stop story delivery. Explain what is missing and ask for clarification in an ordinary harness reply. This is a diagnostic, not an official `assistant` output or successful delivery for this turn. Do not invent a viewpoint or emit a fake story to claim delivery. The current CLI has no command for writing a clarification or blocked status.

See [Narrative](../references/narrative.md) for writing rules and [Callbacks and CLI](../references/callback.md) for handling blockers.
