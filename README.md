# RPSkill

English · [简体中文](README.zh-CN.md)

A local-first workspace for novel-style roleplay, built with a Go server/CLI, an embedded Vue frontend, and an Agent Skill.

**Write a prompt in the browser. The same Codex conversation that started the server reads the workspace, writes the scene, and saves it through the Skill's bundled CLI.** RPSkill does not create another agent session or maintain a task/event ledger.

## Build and install

The repository contains source code, not prebuilt executables or compiled frontend assets. Use the Go toolchain specified in `go.mod`, a Node.js version accepted by `web/package.json`, npm, GNU Make, and Bash.

```sh
make build
./build/rps install-skill --target /path/to/skills
```

`make build` installs the locked frontend dependencies, builds Vue, embeds the generated assets, and builds the native CLI. Installation creates `<skills>/rp` with the Skill documents and `bin/rps` (`rps.exe` on Windows). Nothing is installed into the system `PATH`.

For a manual build without Make:

```sh
npm --prefix web ci
npm --prefix web run build
bash scripts/sync-web.sh
go build -trimpath -o build/rps ./cmd/rps
./build/rps install-skill --target /path/to/skills
```

On Windows, use a Bash environment for the synchronization script and build `build/rps.exe`. Once built, the deployed program needs neither Go nor Node.js. See [installation](docs/INSTALL.md) for packaging and upgrades.

## Start a workspace

Run these commands from the Codex conversation that will handle the story:

```sh
/path/to/skills/rp/bin/rps init /path/to/MyRP
/path/to/skills/rp/bin/rps serve /path/to/MyRP --callback codex
```

The server reads the recipient from `CODEX_THREAD_ID`. For a manual launch, pass `--owner-thread <existing-thread-id>`. Open the token-bearing URL printed at startup; stop the server with Ctrl+C. There is no permanent server configuration, system service, or automatic startup.

The default dispatcher requires a Codex CLI with `codex queue --thread ... --message ...` support. RPSkill queues a notification to the existing conversation; it never silently starts an App Server or resumes a separate agent. Queue acceptance does not prove that the conversation has consumed the input. A host-provided inbox bridge can be selected explicitly; see [installation](docs/INSTALL.md).

## Use the workbench

- **Input identities:** Chat for out-of-story communication, Director for scene instructions, or an ordinary character.
- **Output types:** Assistant for Chat; Story for Director and ordinary characters. A Story can contain multiple characters, dialogue, actions, and environmental description.
- **Story settings:** perspective, polishing (`off`, `basic`, `full`), character and worldbook overrides, and keyword lookback depth. Media management is part of the story dialog.
- **Character control:** an ordinary character's input protects that character; Director clears protection; Chat leaves it unchanged. Merely selecting a character does not change the lock.
- **History:** edit a message, delete from that message through the newest message, or copy a prefix into a separate story. Newest messages load first; older messages load on upward scroll.
- **Language:** English and Simplified Chinese are available from the theme/language dialog.

The server saves an input before dispatching it. If delivery fails, the error explicitly says the input was saved; ask the original conversation to process that existing input rather than submitting it again. Only the latest input accepts output. There is no task recovery queue, generation-status monitor, history-version check, or output deduplication. Check the files before repeating a write whose response was lost.

## Workspace layout

Keep your real workspaces **outside this source repository**.

```text
MyRP/
  rp.yaml                         # last_chat only
  worldbook/<category>/<entry>.md
  characters/<character>/card.md  # optional avatar and attachments
  chats/<story>/
    settings.yaml
    history.jsonl                 # input and output only
    characters/<character>.md     # current story-specific state
    media/                        # created when needed
```

Names come from directory/file names, not duplicate `id` or `name` fields. Media references are relative to the story or character directory. Branching copies a history prefix and **current** settings/state into another flat story; it does not reconstruct past character state or build a history tree.

**Read from files, write through CLI.** While running, the server is the sole formal writer. Stop it before copying a workspace to another machine. Story data does not contain Codex session IDs, API tokens, request ledgers, or a database.

## CLI and checks

The notification supplies a private connection file. Pass it with `--connection`; do not copy its token into prompts or source files.

```sh
/path/to/skills/rp/bin/rps status --project /path/to/MyRP --connection /path/to/connection.json --json
/path/to/skills/rp/bin/rps chat settings main polishing.level '"full"' --project /path/to/MyRP --connection /path/to/connection.json
```

Formal output requires `emit --chat --input --kind`; see the [Skill](skill/rp/SKILL.md). Ordinary agent replies do not become story text.

After `make build`, run:

```sh
go test ./...
go vet ./...
npm --prefix web test
```

Tests with controlled inbox fixtures verify the server/CLI contract, not actual model generation. Real conversation delivery, literary quality, and native operation on each target platform require separate checks. The current version is `1.0.0-rc.1`.

- [Installation](docs/INSTALL.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Example workspace](examples/README.md)

MIT licensed. See [LICENSE](LICENSE).
