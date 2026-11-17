# Rainharbor Observatory: a portable starter workspace

`harbor-academy/` is a self-contained workspace with two adult characters, Alice and Bob, avatars and example attachments, three worldbook categories, and an empty `main` story. Chat and Director are built-in input identities; Story and Assistant are output types, not character cards.

- `worldbook/world/history.md`: always active.
- `worldbook/magic/tide-signal.md`: keywords `tide lamp`, `mana`, and `tideglass`.
- `worldbook/locations/observatory.md`: keywords `observatory` and `observation room`.
- `chats/main/settings.yaml`: third-person perspective and basic polishing; `history.jsonl` is empty, with no real turns.
- `rp.yaml`: contains only `last_chat`. Story-specific character documents are created under `chats/main/characters/<character>.md` when needed.

Copy only `harbor-academy/` as your workspace, not `tests/` or `tools/`. It contains no tokens, sessions, or machine-specific absolute paths. `assets/tone.wav` and `assets/signal.mp4` are synthetic local fixtures; `reading/scene-demo.md` is a hand-written layout sample. They are not model-generated results or evidence of literary compliance.

## Start a copy

Copy `harbor-academy/` to a separate `MyRP` directory. After building the CLI, validate it offline and install the Skill:

```sh
./build/rps validate --project /path/to/MyRP --offline --json
./build/rps install-skill --target /path/to/skills --json
/path/to/skills/rp/bin/rps serve /path/to/MyRP --callback codex
```

Run `serve` in the Codex conversation that will handle the roleplay. `CODEX_THREAD_ID` identifies the recipient; alternatively, pass `--owner-thread` explicitly. The service uses `codex queue` without creating a session. The CLI is distributed inside the Skill, not installed into the global `PATH`. Installation does not overwrite by default; back up customizations before using `--force`. See [installation](../docs/INSTALL.md).

## Manage state and media

Here, `$RPS` is the installed Skill CLI and `$CONNECTION` is the current server's private connection file outside the workspace. All writes go through the running server:

```sh
"$RPS" chat settings main polishing.level '"full"' --project /path/to/MyRP --connection "$CONNECTION"
"$RPS" character dynamic update alice --file ./alice-state.md --project /path/to/MyRP --chat main --connection "$CONNECTION"
"$RPS" worldbook category magic --chat main --enabled=false --project /path/to/MyRP --connection "$CONNECTION"
"$RPS" media upload --project /path/to/MyRP --chat main --file ./examples/assets/tone.wav --connection "$CONNECTION" --json
```

Character state is a complete Markdown document, separate from the base card and history. Uploads return media records; formal output uses `emit --chat main --input <latest-input-line> --kind story --file response.md --media <returned-id>` with the same connection. Uploading is not emitting, and an ordinary agent reply is not formal text. There is no `final` flag, revision check, task ledger, or automatic retry. Read history before repeating an uncertain write. The connection grants local management access; never store its credentials in a story.

Uploads are limited to 32 MiB, with type detection based on file bytes. References are relative to the owning story/character directory. The character UI manages avatars only; example expressions and attachments can still be managed through the CLI.

## Branch and migrate

`chat branch main <message-position> --name <new-story>` copies the history prefix through that message, plus current settings, character state, and associated media. Stories are flat and independent, without parent-tree links. Branching does not reconstruct character state as it was at the historical branch point. Message positions start at 1 and can change after deletion.

Stop the server before copying the complete workspace. CLI `project export/import` and `chat export/import` retain archive support; the frontend has no import/export buttons. Export to a new file outside the workspace. Import validates name collisions and resource boundaries; do not unpack archives over a running project. Migration does not carry the Codex session; the new service binds to its launcher.

## Sample prompts

- **Director:** Bob is repairing the tide lamp in the observation room. Alice opens the door and asks why he has not rested. Develop the interaction without skipping to the next day.
- **Alice:** I stop at the door and ask Bob, “What is wrong with this tide lamp now?”
- **Bob:** I slide the record sheet toward Alice and wait for her to finish reading.
- **Chat:** Discuss two possible directions without writing them into the story yet.

An ordinary character's actual input changes protection. Director clears it; Chat leaves it unchanged. First-person perspective is independent of the protection lock. `off` continues from the given facts without reenacting them, `basic` preserves the user's expression, and `full` may restructure the prose without reversing explicit facts.
