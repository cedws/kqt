"""Protect formula publishing against invalid tags and corrupt release assets."""
import hashlib
import tempfile
import unittest
from pathlib import Path

from homebrew_formula import homebrew_formula


class HomebrewFormulaTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        checksums = []
        for arch in ("arm64", "amd64"):
            filename = f"kqt-darwin-{arch}.tar.gz"
            content = f"archive for {arch}".encode()
            (self.directory / filename).write_bytes(content)
            checksums.append(f"{hashlib.sha256(content).hexdigest()}  {filename}\n")
        (self.directory / "SHA256SUMS").write_text("".join(checksums))

    def test_both_architectures_use_verified_archives(self):
        formula = homebrew_formula("v1.2.3", self.directory)
        self.assertIn('version "1.2.3"', formula)
        for arch in ("arm64", "amd64"):
            content = (self.directory / f"kqt-darwin-{arch}.tar.gz").read_bytes()
            self.assertIn(f"/v1.2.3/kqt-darwin-{arch}.tar.gz", formula)
            self.assertIn(f'sha256 "{hashlib.sha256(content).hexdigest()}"', formula)
        self.assertIn('bin.install "kqt"', formula)
        self.assertIn('internal/tui/licenses/*.txt', formula)

    def test_invalid_and_prerelease_tags_are_rejected(self):
        for tag in ("v1.2.3-rc.1", "v1.02.3", 'v1.2.3"; system("bad")'):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                homebrew_formula(tag, self.directory)

    def test_corrupt_or_missing_archives_are_rejected(self):
        archive = self.directory / "kqt-darwin-arm64.tar.gz"
        archive.write_bytes(b"corrupt download")
        with self.assertRaisesRegex(ValueError, "mismatched checksum"):
            homebrew_formula("v1.2.3", self.directory)
        archive.unlink()
        with self.assertRaises(FileNotFoundError):
            homebrew_formula("v1.2.3", self.directory)

    def test_missing_or_duplicate_checksums_are_rejected(self):
        manifest = self.directory / "SHA256SUMS"
        content = manifest.read_text()
        manifest.write_text(content.splitlines(keepends=True)[1])
        with self.assertRaisesRegex(ValueError, "Missing or mismatched checksum"):
            homebrew_formula("v1.2.3", self.directory)
        manifest.write_text(content + content)
        with self.assertRaisesRegex(ValueError, "Duplicate checksum"):
            homebrew_formula("v1.2.3", self.directory)


if __name__ == "__main__":
    unittest.main()
