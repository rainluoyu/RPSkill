# Troubleshooting

## Build fails in a fresh checkout

Compiled frontend files and native executables are deliberately not tracked in Git. Run `make build` to build both the frontend and the CLI. If running Go commands directly, first run `make build-web`; otherwise the embed directory is missing. Install the resulting executable with `install-skill` rather than copying the source-only Skill folder as a complete installation.

## Startup fails

- Supply `--callback codex` and bind the actual originating conversation that will handle the roleplay.
- Missing owner: start from that Codex conversation or explicitly pass `--owner-thread`. Do not guess the most recent conversation. Run `codex queue --help` to check whether the installed CLI supports queue delivery.
- Another service owns the project: stop the previous process first. Changing the runtime directory does not bypass the store's single-writer lock.
- Port already in use: choose another loopback port, or use `--listen 127.0.0.1:0` to let the OS choose one.

## The input was saved, but the conversation did not receive it

`input_saved_delivery_failed` means saving succeeded but delivery failed or its result is uncertain. Do not save the same input again. Ask the originating conversation to read the story's existing input, or fix the host inbox before continuing. There is no execution-status tracker, retry endpoint, or hidden recovery queue.

Successful `codex queue` delivery only means the input entered the original conversation's queue. The conversation may be busy or may not be consuming that queue. RPSkill will not create another session or infer that generation is in progress. Use the story and input line number in the notification to locate the input; they are not persistent database request records.

## The conversation replied, but no text appeared in the browser

Ordinary agent replies are not formal content. Use the current Skill's CLI with `emit --connection ... --chat ... --input ... --kind ...` and confirm success. Chat requires Assistant; other input identities require Story. Check that the input line is still the latest input and that its text has not been edited. Do not rebind output to a different input or blindly resend it. The server does not detect changes to text on the same line or deduplicate output.

## Write errors or interrupted connections

There is no idempotency receipt ledger and no automatic write retry. If a response is lost, inspect the current history, media, or settings before writing again. Output for an earlier input cannot be written across a newer input. Copy a branch when you need another direction.

If `recovery_required` occurs, stop the server and reopen the workspace through the normal store recovery process. Do not manually delete an unfinished transaction journal to hide a partial write.

## Authentication and media

The token changes on each server start. Old browser cookies and connection files cannot be reused; open the new startup URL. Authentication errors must not be worked around by bypassing loopback or same-origin checks. Never print the connection file or put it in story data. Normal shutdown removes it; forced termination may leave an invalid temporary file.

Media uploads are limited to 32 MiB and must contain actual file bytes. The frontend previews or downloads according to safe MIME types; scripts and HTML are not executed. References are relative to the story or character directory. Upload a file first, then use the returned ID rather than inventing a path. Editing or deleting history does not automatically remove media or character-state documents.

## Test results versus real model behavior

Go, frontend, and controlled-inbox CLI tests are separate from actual model consumption. A test fixture cannot prove that Codex successfully wrote a story. When reporting problems, include the command, exit code, and redacted error—not tokens, connection files, or private story text.
