"""Structural checks for the RP Skill; these do not test Harness behavior.

Run from any directory with: python3 /path/to/tests/skill/test_structure.py
Use skill-creator's quick_validate.py separately for YAML frontmatter validation.
"""

from pathlib import Path
import os
import re
import sys
import subprocess
import tempfile
import unittest


SKILL = Path(__file__).resolve().parents[2] / "skill" / "rp"
REFERENCES = {
    "workspace.md",
    "characters.md",
    "worldbook.md",
    "history.md",
    "narrative.md",
    "messages.md",
    "callback.md",
}


class SkillStructureTests(unittest.TestCase):
    def test_skill_tree_contains_only_distributable_files(self):
        self.assertEqual({p.name for p in SKILL.parent.iterdir()}, {"rp"})
        self.assertEqual({p.name for p in SKILL.iterdir()} - {"bin"},
                         {"SKILL.md", "builtins", "references"})
        self.assertEqual({p.name for p in (SKILL / "references").iterdir()}, REFERENCES)
        for path in SKILL.rglob("*"):
            self.assertFalse(path.is_symlink(), str(path))
            if path.is_file():
                self.assertIn(path.suffix, {".md", ".exe", ""}, str(path))

    def test_required_documents_are_nonempty_utf8(self):
        required = [SKILL / "SKILL.md"]
        required += [SKILL / "builtins" / name for name in ("chat.md", "director.md")]
        required += [SKILL / "references" / name for name in sorted(REFERENCES)]
        for path in required:
            with self.subTest(path=path.relative_to(SKILL)):
                self.assertTrue(path.is_file(), f"Missing document: {path}")
                self.assertTrue(path.read_text(encoding="utf-8").strip())

    def test_only_chat_and_director_are_builtins(self):
        self.assertEqual(
            {p.name for p in (SKILL / "builtins").iterdir()},
            {"chat.md", "director.md"},
        )
        forbidden = {"assistant.md", "story.md", "narrator.md", "environment.md"}
        for path in SKILL.rglob("*.md"):
            self.assertNotIn(path.name.casefold(), forbidden)

    def test_document_links_resolve_inside_skill(self):
        # The Skill intentionally uses simple relative Markdown file links.
        # Images and dynamic CLI targets are not treated as documentation links.
        linked = set()
        for path in SKILL.rglob("*.md"):
            text = path.read_text(encoding="utf-8")
            for target in re.findall(r"(?<!!)\[[^\]]+\]\(([^)]+)\)", text):
                with self.subTest(document=path.name, target=target):
                    self.assertNotIn("://", target, "References must be bundled")
                    destination = (path.parent / target.split("#", 1)[0]).resolve()
                    self.assertTrue(destination.is_relative_to(SKILL.resolve()))
                    self.assertTrue(destination.is_file(), f"Broken link: {target}")
                    if path == SKILL / "SKILL.md":
                        linked.add(destination)
        self.assertTrue(
            {SKILL / "references" / name for name in REFERENCES} <= linked,
            "Every required reference must be discoverable from the entrypoint",
        )

    def test_bin_contains_no_fake_executables(self):
        if not (SKILL / "bin").exists():
            return  # A source checkout contains only Skill text.
        for path in (SKILL / "bin").iterdir():
            with self.subTest(file=path.name):
                if path.name == ".gitkeep":
                    self.assertEqual(path.read_bytes(), b"")
                    self.assertFalse(path.is_symlink())
                    self.assertFalse(path.stat().st_mode & 0o111)
                    continue
                self.assertIn(path.name, {"rps", "rps.exe"})
                self.assertTrue(path.is_file())
                self.assertFalse(path.is_symlink(), "Bundle the binary itself")
                with path.open("rb") as stream:
                    header = stream.read(4)
                self.assertTrue(
                    header == b"\x7fELF"
                    or header[:2] == b"MZ"
                    or header in {
                        b"\xfe\xed\xfa\xce", b"\xce\xfa\xed\xfe",
                        b"\xfe\xed\xfa\xcf", b"\xcf\xfa\xed\xfe",
                        b"\xca\xfe\xba\xbe", b"\xbe\xba\xfe\xca",
                        b"\xca\xfe\xba\xbf", b"\xbf\xba\xfe\xca",
                    },
                    "Expected a native binary header, not a script or placeholder",
                )

    def test_current_platform_binary_present(self):
        binary = SKILL / "bin" / ("rps.exe" if sys.platform == "win32" else "rps")
        if not binary.is_file():
            self.skipTest("Source checkout has no native CLI; installation is tested separately")
        with binary.open("rb") as stream:
            header = stream.read(4)
        if sys.platform.startswith("linux"):
            self.assertEqual(header, b"\x7fELF")
        elif sys.platform == "win32":
            self.assertEqual(header[:2], b"MZ")
        elif sys.platform == "darwin":
            self.assertIn(header, {
                b"\xfe\xed\xfa\xce", b"\xce\xfa\xed\xfe",
                b"\xfe\xed\xfa\xcf", b"\xcf\xfa\xed\xfe",
                b"\xca\xfe\xba\xbe", b"\xbe\xba\xfe\xca",
                b"\xca\xfe\xba\xbf", b"\xbf\xba\xfe\xca",
            })
        else:
            self.skipTest(f"Binary header check unavailable for {sys.platform}")
        if sys.platform != "win32":
            self.assertTrue(os.access(binary, os.X_OK), "Binary is not executable")


    def test_packaged_binary_runs_from_unrelated_directory(self):
        binary = SKILL / "bin" / ("rps.exe" if sys.platform == "win32" else "rps")
        if not binary.is_file():
            self.skipTest("No native binary supplied; execution unverified")
        with tempfile.TemporaryDirectory(prefix="rps-skill-package-") as working:
            for argument, expected in (("--help", "Usage:"), ("version", "protocol")):
                with self.subTest(argument=argument):
                    result = subprocess.run([str(binary), argument], cwd=working,
                                            capture_output=True, text=True, timeout=10, check=False)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn(expected, result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
