# Synthetic media fixtures

- `tone.wav`: a locally synthesized 0.25-second, 440 Hz, low-amplitude mono tone; not generated dialogue or a soundscape.
- `signal.mp4`: a locally encoded one-second blue-gray frame, 96×96 at 4 fps, without audio; not a generated story video.
- For an image input, use `../harbor-academy/characters/alice/avatar.png`. Bob's `notes.txt` can be used as an attachment.

These are upload inputs, not formal messages in the starter workspace, and they have no preassigned media IDs. Upload with `rps media upload` and use the returned record to attach a resource to an input or formal output. Do not label playback demonstrations as agent-generated media. Recreate the files with `../tools/generate_media.py`; running RP does not require Python or ffmpeg.
