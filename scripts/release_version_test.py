"""Release tags must obey SemVer and classify prereleases correctly."""
import subprocess
import sys
import unittest
from pathlib import Path

from release_version import release_version


class ReleaseVersionTest(unittest.TestCase):
    def test_stable_versions_and_build_metadata(self):
        for tag in ["v0.0.1", "v1.2.3", "v10.20.30", "v1.2.3+001", "v1.2.3+build.5"]:
            with self.subTest(tag=tag):
                self.assertEqual(release_version(tag), (tag, False))

    def test_prereleases(self):
        for tag in ["v1.2.3-0", "v1.2.3-alpha", "v1.2.3-rc.1", "v1.2.3-01a", "v1.2.3-rc.1+build.5"]:
            with self.subTest(tag=tag):
                self.assertEqual(release_version(tag), (tag, True))

    def test_invalid_tags_are_rejected(self):
        for tag in ["1.2.3", "V1.2.3", "v1.2", "v01.2.3", "v1.02.3", "v1.2.03",
                    "v1.2.3-01", "v1.2.3-rc.01", "v1.2.3-", "v1.2.3+", "v1.2.3-a..b",
                    "v1.2.3-hello_world", "v1.2.3+build..5", "v1.2.3 ", "v1.2.3\nlatest=true"]:
            with self.subTest(tag=tag):
                with self.assertRaises(ValueError):
                    release_version(tag)

    def test_cli_outputs_and_exit_status(self):
        script = str(Path(__file__).with_name("release_version.py"))
        valid = subprocess.run([sys.executable, script, "v0.0.1"], capture_output=True, text=True)
        self.assertEqual(valid.returncode, 0)
        self.assertEqual(valid.stdout, "version=v0.0.1\nprerelease=false\n")
        invalid = subprocess.run([sys.executable, script, "v0.0.01"], capture_output=True, text=True)
        self.assertNotEqual(invalid.returncode, 0)
        self.assertEqual(invalid.stdout, "")
        self.assertIn("Invalid release tag", invalid.stderr)


if __name__ == "__main__":
    unittest.main()
