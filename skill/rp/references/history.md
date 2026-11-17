# History: One Complete Record per Story

`chats/<chat>/history.jsonl` stores only sequential input/output. Experiences in other stories are not facts in this story. There is no parent inheritance or execution log.

The notification's `history` is a project-relative file path, and `input_line` is 1-based. `total_lines` is the line count at notification time, not a guarantee that the file remains unchanged. The server maintains no unread range, session, turn state, or completion cursor. Read the history you need; rebuild context from files when necessary. Verified, unchanged settings may be reused.

Actual input from an ordinary character protects that character. Chat leaves protection unchanged; Director clears it. A character's current portrayal combines their base card with this story's `characters/<character-name>.md`; state is not replayed from history. Future discussion does not automatically become story fact, and there are no separate planning files.

Users can edit or delete history; there is no content-version check to prevent overwriting a concurrent edit. The frontend's "Delete back to here" action deletes the selected message and all following messages. Editing does not automatically generate a reply or change character state, attachment files, or later prose. There is no hidden audit trail; restoring the original text requires an external backup.

Branching copies the complete history through the specified message and its referenced media, plus current settings and current character state (not historical snapshots), into an independent sibling story. It retains no parent-child relationship, invokes no model, and neither creates nor copies any agent session.

Conversation memory is not the source of story truth. After edits, files take precedence. Old notifications cannot be automatically replayed, and old replies cannot borrow a new input's position. Restarting does not resume tasks. To continue, the originating conversation must process existing input, or the user must send a new prompt.
