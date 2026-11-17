"""Example asset checks, plus opt-in validation by the actual Go CLI.

Run with a Python environment containing PyYAML:
    python3 examples/tests/test_examples.py
To include real core validation:
    RPS_VALIDATE_BIN=/path/to/rps python3 examples/tests/test_examples.py
No model, media generation service or Mock validation is used.
"""

from hashlib import sha256
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import struct
import subprocess
import tempfile
import unittest
import wave
import zlib

import yaml


EXAMPLES = Path(__file__).resolve().parents[1]
WORKSPACE = EXAMPLES / "harbor-academy"
SPEC = importlib.util.spec_from_file_location("generate_avatars", EXAMPLES / "tools/generate_avatars.py")
GENERATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GENERATOR)


class UniqueLoader(yaml.SafeLoader):
    pass


def unique_mapping(loader, node):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if key in result:
            raise ValueError(f"Duplicate YAML key: {key}")
        result[key] = loader.construct_object(value_node)
    return result


UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping)


def load_yaml(path):
    return yaml.load(path.read_text(encoding="utf-8"), Loader=UniqueLoader)


def frontmatter(path):
    data = path.read_text(encoding="utf-8")
    if not data.startswith("---\n"):
        raise ValueError(f"Missing frontmatter: {path}")
    header, body = data[4:].split("\n---\n", 1)
    if not body.strip():
        raise ValueError(f"Empty body: {path}")
    return yaml.load(header, Loader=UniqueLoader)


def manifest(root):
    return {p.relative_to(root).as_posix(): sha256(p.read_bytes()).hexdigest()
            for p in root.rglob("*") if p.is_file()}


class ExampleTests(unittest.TestCase):
    def test_workspace_only_contains_portable_data(self):
        self.assertEqual({p.name for p in WORKSPACE.iterdir()},
                         {"rp.yaml", "characters", "worldbook", "chats"})
        for path in WORKSPACE.rglob("*"):
            with self.subTest(path=path.relative_to(WORKSPACE)):
                self.assertFalse(path.is_symlink())
                self.assertNotIn(path.name, {"server.json", ".venv", "node_modules", "AGENTS.md"})
                if path.is_file() and path.suffix != ".png":
                    data = path.read_text(encoding="utf-8")
                    for forbidden in ("/home/", "/mnt/", "RPS_ATTEMPT_TOKEN", "attempt_id", "session_id", "thread_id"):
                        self.assertNotIn(forbidden, data)

    def test_initial_settings_match_empty_history(self):
        project = load_yaml(WORKSPACE / "rp.yaml")
        self.assertEqual(project, {"last_chat": "main"})
        settings = load_yaml(WORKSPACE / "chats/main/settings.yaml")
        self.assertEqual(settings["narrative"]["perspective"], "third")
        self.assertEqual(settings["polishing"]["level"], "basic")
        self.assertFalse((WORKSPACE / "chats/main/timeline.yaml").exists())
        self.assertEqual((WORKSPACE / "chats/main/history.jsonl").read_bytes(), b"")
        self.assertFalse((WORKSPACE / "chats/main/media").exists())

    def test_two_ordinary_characters_with_real_avatars(self):
        folders = {p.name for p in (WORKSPACE / "characters").iterdir()}
        self.assertEqual(folders, {"alice", "bob"})
        initial = load_yaml(WORKSPACE / "chats/main/settings.yaml")
        self.assertEqual(initial["characters"], {"alice": True, "bob": True})
        for character in sorted(folders):
            directory = WORKSPACE / "characters" / character
            card = frontmatter(directory / "card.md")
            self.assertNotIn("id", card)
            self.assertNotIn("name", card)
            self.assertIs(card["enabled"], True)
            avatar = PurePosixPath(card["avatar"])
            self.assertFalse(avatar.is_absolute())
            self.assertNotIn("..", avatar.parts)
            self.assertTrue((directory / avatar).is_file())
            self.assertTrue(any((directory / "expressions").glob("*.png")))
        self.assertIn("tide lamp", (WORKSPACE / "characters/bob/notes.txt").read_text())

    def test_worldbook_modes_and_story_overrides(self):
        entries = {}
        for path in (WORKSPACE / "worldbook").rglob("*"):
            if not path.is_file():
                continue
            relative = path.relative_to(WORKSPACE / "worldbook")
            self.assertEqual(len(relative.parts), 2)
            self.assertEqual(path.suffix, ".md")
            entry = frontmatter(path)
            self.assertEqual(set(entry), {"enabled", "activation", "keys", "priority"})
            key = relative.with_suffix("").as_posix()
            self.assertNotIn(key, entries)
            self.assertIs(entry["enabled"], True)
            self.assertIsInstance(entry["priority"], int)
            self.assertIsInstance(entry["keys"], list)
            entries[key] = entry
        self.assertEqual({entry["activation"] for entry in entries.values()}, {"always", "keyword"})
        self.assertEqual(entries["magic/tide-signal"]["keys"], ["tide lamp", "mana", "tideglass"])
        initial = load_yaml(WORKSPACE / "chats/main/settings.yaml")
        self.assertEqual(initial["worldbook"]["entries"], {key: True for key in entries})

    def test_png_bytes_crc_dimensions_and_pixel_payload(self):
        for path in WORKSPACE.rglob("*.png"):
            with self.subTest(path=path.relative_to(WORKSPACE)):
                data = path.read_bytes()
                self.assertLess(len(data), 4096)
                self.assertEqual(data[:8], b"\x89PNG\r\n\x1a\n")
                offset, kinds, compressed = 8, [], bytearray()
                while offset < len(data):
                    length, kind = struct.unpack(">I4s", data[offset:offset + 8])
                    payload = data[offset + 8:offset + 8 + length]
                    crc = struct.unpack(">I", data[offset + 8 + length:offset + 12 + length])[0]
                    self.assertEqual(crc, zlib.crc32(kind + payload) & 0xFFFFFFFF)
                    kinds.append(kind)
                    if kind == b"IHDR":
                        self.assertEqual(struct.unpack(">IIBBBBB", payload), (96, 96, 8, 2, 0, 0, 0))
                    elif kind == b"IDAT":
                        compressed.extend(payload)
                    elif kind == b"IEND":
                        self.assertEqual(payload, b"")
                    offset += 12 + length
                self.assertEqual(offset, len(data))
                self.assertEqual(kinds, [b"IHDR", b"IDAT", b"IEND"])
                pixels = zlib.decompress(compressed)
                self.assertEqual(len(pixels), 96 * (1 + 96 * 3))
                self.assertTrue(all(pixels[y * 289] == 0 for y in range(96)))

    def test_avatar_generator_reproduces_committed_assets(self):
        for relative, data in GENERATOR.assets().items():
            self.assertEqual(data, (WORKSPACE / relative).read_bytes(), relative)
        self.assertNotEqual(GENERATOR.draw("alice"), GENERATOR.draw("bob"))
        self.assertNotEqual(GENERATOR.draw("bob"), GENERATOR.draw("bob", "smile"))

    def test_copy_is_self_contained_without_source_paths(self):
        expected = manifest(WORKSPACE)
        with tempfile.TemporaryDirectory(prefix="rps-example-copy-") as temporary:
            copy = Path(temporary) / "雨港 副本"
            shutil.copytree(WORKSPACE, copy)
            self.assertEqual(manifest(copy), expected)
            self.assertEqual(load_yaml(copy / "rp.yaml"), {"last_chat": "main"})
            for card_path in (copy / "characters").glob("*/card.md"):
                self.assertTrue((card_path.parent / frontmatter(card_path)["avatar"]).is_file())
        self.assertEqual(manifest(WORKSPACE), expected)

    def test_synthetic_audio_video_and_reading_labels(self):
        audio = EXAMPLES / "assets/tone.wav"
        with wave.open(str(audio), "rb") as stream:
            self.assertEqual((stream.getnchannels(), stream.getsampwidth(),
                              stream.getframerate(), stream.getnframes()), (1, 2, 8000, 2000))
            self.assertEqual(len(stream.readframes(2000)), 4000)
        spec = importlib.util.spec_from_file_location("generate_media", EXAMPLES / "tools/generate_media.py")
        generator = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(generator)
        with tempfile.TemporaryDirectory() as directory:
            recreated = Path(directory) / "tone.wav"
            generator.tone(recreated)
            self.assertEqual(audio.read_bytes(), recreated.read_bytes())
        video = (EXAMPLES / "assets/signal.mp4").read_bytes()
        self.assertLess(len(video), 32768)
        offset, boxes = 0, []
        while offset < len(video):
            size, kind = struct.unpack(">I4s", video[offset:offset + 8])
            self.assertGreaterEqual(size, 8)
            self.assertLessEqual(offset + size, len(video))
            boxes.append(kind)
            offset += size
        self.assertEqual(offset, len(video))
        self.assertTrue({b"ftyp", b"moov", b"mdat"} <= set(boxes))
        demo = (EXAMPLES / "reading/scene-demo.md").read_text()
        self.assertIn("Hand-written sample chapter", demo)
        self.assertIn("not Codex output", demo)
        self.assertEqual((WORKSPACE / "chats/main/history.jsonl").read_bytes(), b"")

    def test_actual_cli_validation_original_and_copied_workspace(self):
        executable = os.environ.get("RPS_VALIDATE_BIN")
        if not executable:
            self.skipTest("Set RPS_VALIDATE_BIN to a real Phase 1+ CLI; local checks are not core validation")
        executable = str(Path(executable).resolve(strict=True))
        before = manifest(WORKSPACE)
        with tempfile.TemporaryDirectory(prefix="rps-example-cli-") as temporary:
            copied = Path(temporary) / "迁移 副本"
            shutil.copytree(WORKSPACE, copied)
            for workspace in (WORKSPACE, copied):
                with self.subTest(workspace=workspace):
                    result = subprocess.run([executable, "validate", "--project", str(workspace), "--json"],
                                            capture_output=True, text=True, timeout=30, check=False)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    response = json.loads(result.stdout)
                    self.assertIsInstance(response, dict)
                    self.assertNotIn("error", response)
                    self.assertIs(response["data"]["valid"], True)
                    self.assertEqual(response["data"]["issues"], [])
                    self.assertEqual(response["data"]["chats"], 1)
                    self.assertEqual(response["data"]["events"], 0)
                    self.assertEqual(Path(response["data"]["project"]).resolve(), workspace.resolve())
                    self.assertEqual(manifest(workspace), before, "validate changed formal data")
        self.assertEqual(manifest(WORKSPACE), before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
