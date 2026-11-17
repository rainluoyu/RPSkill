"""Black-box checks of a real rps install-skill command.

RPS_TEST_BIN=/absolute/path/to/rps python3 tests/skill/test_install.py
No executable or install API is mocked. The selected binary must include the
current Skill sources; tests install only into disposable temporary folders.
"""

from hashlib import sha256
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SKILL = Path(__file__).resolve().parents[2] / "skill" / "rp"


def digest(path):
    return sha256(path.read_bytes()).hexdigest()


def manifest(root):
    return {p.relative_to(root).as_posix(): digest(p)
            for p in root.rglob("*") if p.is_file()}


class InstallSkillTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        executable = os.environ.get("RPS_TEST_BIN")
        if not executable:
            raise unittest.SkipTest("Set RPS_TEST_BIN to the real CLI under test")
        cls.executable = Path(executable).resolve(strict=True)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="rps-skill-install-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.env = dict(os.environ)
        self.env["RPS_RUNTIME_DIR"] = str(self.root / "runtime")

    def command(self, *args, executable=None, env=None):
        return subprocess.run([str(executable or self.executable), *map(str, args)],
                              cwd=self.root, env=env or self.env, text=True,
                              capture_output=True, timeout=30, check=False)

    def install(self, target):
        result = self.command("install-skill", "--target", target)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        installed = target / "rp"
        binary = installed / "bin" / ("rps.exe" if sys.platform == "win32" else "rps")
        return installed, binary

    def test_installs_exact_current_text_and_running_binary(self):
        original = digest(self.executable)
        installed, binary = self.install(self.root / "含空格 skills")
        expected = {"SKILL.md"}
        expected.update(p.relative_to(SKILL).as_posix() for p in (SKILL / "builtins").glob("*.md"))
        expected.update(p.relative_to(SKILL).as_posix() for p in (SKILL / "references").glob("*.md"))
        for relative in expected:
            self.assertEqual((installed / relative).read_bytes(), (SKILL / relative).read_bytes(), relative)
        expected.add(binary.relative_to(installed).as_posix())
        self.assertEqual(set(manifest(installed)), expected)
        self.assertEqual(digest(binary), original)
        self.assertEqual(digest(self.executable), original)
        self.assertFalse(binary.is_symlink())
        if sys.platform != "win32":
            self.assertTrue(os.access(binary, os.X_OK))
        for argument in ("--help", "version"):
            result = self.command(argument, executable=binary)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_installed_binary_can_install_again_without_repo_or_recursive_growth(self):
        self.env["PATH"] = str(self.root / "empty-path")
        _, first = self.install(self.root / "first skills")
        result = self.command("install-skill", "--target", self.root / "second skills", executable=first)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        second = self.root / "second skills" / "rp" / "bin" / first.name
        self.assertEqual(digest(first), digest(second))
        self.assertEqual(first.stat().st_size, second.stat().st_size)
        self.assertEqual((self.root / "first skills/rp/SKILL.md").read_bytes(),
                         (self.root / "second skills/rp/SKILL.md").read_bytes())

    def test_does_not_overwrite_existing_install_without_explicit_option(self):
        installed, _ = self.install(self.root / "skills")
        marker = installed / "user-note.txt"
        marker.write_text("Keep user content.\n", encoding="utf-8")
        before = manifest(installed)
        result = self.command("install-skill", "--target", installed.parent)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(manifest(installed), before)

    def test_explicit_force_replaces_old_text_without_copying_stale_binary(self):
        target = self.root / "skills"
        installed, binary = self.install(target)
        (installed / "SKILL.md").write_text("Local obsolete instructions.\n", encoding="utf-8")
        result = self.command("install-skill", "--target", target, "--force")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((installed / "SKILL.md").read_bytes(), (SKILL / "SKILL.md").read_bytes())
        self.assertEqual(digest(binary), digest(self.executable))
        self.assertEqual({p.name for p in target.iterdir()}, {"rp"})

    def test_symlink_target_is_rejected_without_touching_destination(self):
        real = self.root / "real"
        real.mkdir()
        marker = real / "keep.txt"
        marker.write_text("Do not follow install target symlinks.\n", encoding="utf-8")
        target = self.root / "linked-skills"
        try:
            target.symlink_to(real, target_is_directory=True)
        except (OSError, NotImplementedError) as error:
            self.skipTest(f"Symlink setup unavailable: {error}")
        before = manifest(real)
        result = self.command("install-skill", "--target", target)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(manifest(real), before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
