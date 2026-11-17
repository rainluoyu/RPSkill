# Installation and startup

## Build from source

Use the Go toolchain specified in `go.mod` and a Node.js version accepted by `web/package.json`. Building also requires npm, GNU Make, and Bash. The source repository does not ship executables or compiled frontend assets.

```sh
make build
./build/rps install-skill --target /path/to/skills
```

`make build` runs `npm ci`, builds the Vue frontend, synchronizes it into the Go embed directory, and builds the CLI. Before running Go commands directly in a fresh checkout, run `make build-web` to populate that directory. No toolchain installation is required on the machine that runs the resulting binary.

Without Make:

```sh
npm --prefix web ci
npm --prefix web run build
bash scripts/sync-web.sh
go build -trimpath -o build/rps ./cmd/rps
./build/rps install-skill --target /path/to/skills
```

On Windows, use a Bash environment for the synchronization script, build `build/rps.exe`, and invoke that executable. Cross-compilation is available through `make cross`.

Installation creates `<skills>/rp`, containing only the Skill documents and the running platform's `bin/rps` (`bin/rps.exe` on Windows). It does not modify the system `PATH` or copy development sources, frontend dependencies, or runtime records into the Skill. An existing installation is not overwritten by default. Back up customizations before explicitly upgrading with `--force`.

`make release` produces candidate packages for Linux, macOS, and Windows on amd64 and arm64. Use the package matching your OS and CPU; verify its checksum manifest and source. Cross-compilation is not native runtime validation. Existing packages under `dist/` are not refreshed automatically when source files change.

## Bind to the originating conversation

Start the service from the Codex conversation that will perform the roleplay:

```sh
/path/to/skills/rp/bin/rps init /path/to/MyRP
/path/to/skills/rp/bin/rps serve /path/to/MyRP --callback codex --listen 127.0.0.1:8717
```

The launch environment must provide `CODEX_THREAD_ID`. For a manual terminal launch, explicitly pass `--owner-thread <existing-thread-id>`. The default dispatcher requires a Codex CLI that supports `codex queue`; check your installed CLI with `codex queue --help`. It queues to that conversation without resuming or creating another execution instance. The originating host controls the model, permissions, and tools.

If the host supplies an inbox bridge to an existing Codex conversation, select it with `--dispatch-command /path/to/existing-owner-inbox`; supply arguments with repeated `--dispatch-arg` flags. The bridge reads notification JSON from stdin and exits with status 0 after accepting it, or a nonzero status on failure. It must not create another execution instance. Continue to use `--callback codex`.

The server prints a token-bearing browser URL. Share it only with trusted users; never store it in Git, workspace files, or public logs. Stop the service with Ctrl+C when finished. RPSkill does not install a system service or start at boot. A closed conversation or one that does not consume its queue is not guaranteed to wake automatically; RPSkill does not monitor conversation liveness.

## CLI connection and migration

Host-side administration can use `RPS_SERVER_URL`/`RPS_SERVER_TOKEN` or `--server`/`--token-file`. Input notifications include the path to a private connection file; the CLI reads it with `--connection`, so the token does not need to appear in model text or command-line arguments. This file is valid only for the current server run.

Before upgrading, stop the service and back up the workspace. Replace the installed Skill's CLI and documents, then restart from the owning conversation. The service does not restore old tasks or receipts. Once stopped, a workspace can be copied directly to another directory or machine; relative media references need no rewriting. Use `validate --offline` to check it. Only the current v3 flat-story format is accepted; there is no compatibility converter for the old tree/event-state format.

Keep real workspaces, connection files, tokens, local environment configuration, and diagnostic logs outside the source repository. Only the fictional example workspace under `examples/` is intended for distribution.
