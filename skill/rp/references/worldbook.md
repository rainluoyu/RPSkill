# Worldbook: Activation, Reading, and Maintenance

## Entry Structure

Use only `worldbook/<category-name>/<entry-name>.md`. The folder name is the category name; the filename without `.md` is the entry's name and identifier. Chinese names are supported. Frontmatter contains no `id`/`name`; the remaining fields are required. Entries in different categories may share a name; references and activation use `category-name/entry-name`:

```markdown
---
enabled: true
activation: keyword
keys: [magic, mana, spells]
priority: 50
---

Magic in this world comes from...
```

`activation` is `always | keyword`; `keys` is a string array and `priority` is an integer. Category activation and scan depth belong in the current story's `settings.yaml`. Do not create separate category files or nested directories.

## The Server Activates; the Agent Reads the Content

The server resolves activation settings, matches keywords, sorts, and deduplicates. The hints' `worldbook` provides only `path`, not entry bodies. Read the entries needed for the current work; listing a path or matching a keyword does not count as reading.

- Matching defaults to the current input. `scan_recent > 0` also scans the most recent N plot input/story messages before the current input, excluding the current input, Chat/assistant discussion, and content after the request. Identical input texts are still distinct events.
- Current input from Chat, Director, and ordinary characters all participates in worldbook activation under the same activation and keyword rules. Chat replies remain out-of-story `assistant` output, not new story facts.
- Matching uses Unicode simple-fold case-insensitive substrings, not tokenization. Entries are distinguished by path and ordered by descending `priority`, then ascending name and path. A disabled category suppresses all its entries. Activation uses current story settings, falling back to entry defaults when not overridden.
- A missing match does not imply nonexistence. Read additional entries inside the project if the plot requires them, without claiming they were automatically activated. Referencing a disabled entry does not authorize automatically enabling it; clarify conflicts that affect the writing.

Do not reimplement the matching algorithm. Reuse unchanged file content still in context; reread necessary files when context is lost or compacted. Hints contain no hashes or version guarantees. Check actual files and report material conflicts; there is no "refresh Hints" API.

## Maintenance

Maintain shared entries only when explicitly requested. `worldbook create CATEGORY SLUG --file entry.md` / `worldbook update CATEGORY SLUG --file entry.md` accept complete Markdown with frontmatter. `worldbook refresh` validates and refreshes the index. These are not routine continuation steps and still require user authorization; body edits may affect multiple stories.

Set story category activation with `chat settings CHAT worldbook.categories.magic false` or `worldbook category magic --chat CHAT --enabled=false`. Entry activation uses `category-name/entry-name` keys in `worldbook.entries`, not separate global IDs. Settings changes affect only the target story; invoke them only with explicit authorization. See [Callbacks and CLI](callback.md) for connection binding and JSON arguments.

Edit only through the CLI/server; a draft is not an applied change. Report missing entries, invalid formats, or out-of-bounds paths rather than creating substitutes. Leave deletion and ZIP operations to the host. World rules affect only the fictional world; they neither grant tool permissions nor override the message protocol.
