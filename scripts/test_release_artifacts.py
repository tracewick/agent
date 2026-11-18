"""Negative controls against real GoReleaser/Syft snapshot output."""

import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("release_check", ROOT / "scripts/check-release-artifacts.py")
CHECK = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECK)


class ReleaseArtifactsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.version = json.loads((ROOT / "dist/metadata.json").read_text())["version"]
        for pattern in ("*.tar.gz", "*.sbom.json", "checksums.txt"):
            for path in (ROOT / "dist").glob(pattern):
                shutil.copyfile(path, self.directory / path.name)
        self.sbom = next(self.directory.glob("*amd64*.sbom.json"))

    def rehash(self):
        paths = sorted(self.directory.glob("*.tar.gz")) + sorted(self.directory.glob("*.sbom.json"))
        (self.directory / "checksums.txt").write_text("".join(
            f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in paths))

    def edit_sbom(self, edit):
        document = json.loads(self.sbom.read_text())
        edit(document)
        self.sbom.write_text(json.dumps(document))
        self.rehash()

    def check(self):
        CHECK.check(self.directory, self.version, unsigned=True)

    def test_complete_snapshot_passes(self):
        self.check()

    def test_changed_archive_fails(self):
        with next(self.directory.glob("*.tar.gz")).open("ab") as stream:
            stream.write(b"tampered")
        with self.assertRaisesRegex(ValueError, "Checksum mismatch"):
            self.check()

    def test_swapped_platform_sboms_fail_even_with_updated_checksums(self):
        other = next(self.directory.glob("*arm64*.sbom.json"))
        first, second = self.sbom.read_bytes(), other.read_bytes()
        self.sbom.write_bytes(second)
        other.write_bytes(first)
        self.rehash()
        with self.assertRaisesRegex(ValueError, "architecture"):
            self.check()

    def test_wrong_binary_hash_fails_even_with_updated_checksums(self):
        def edit(document):
            for component in document["components"]:
                for digest in component.get("hashes", []):
                    digest["content"] = "0" * 64
        self.edit_sbom(edit)
        with self.assertRaisesRegex(ValueError, "does not describe"):
            self.check()

    def test_wrong_release_identity_fails(self):
        self.edit_sbom(lambda d: d["metadata"]["component"].update(version="unrelated-release"))
        with self.assertRaisesRegex(ValueError, "release identity"):
            self.check()

    def test_missing_standard_library_fails(self):
        self.edit_sbom(lambda d: d.update(components=[c for c in d["components"] if c["name"] != "stdlib"]))
        with self.assertRaisesRegex(ValueError, "standard library"):
            self.check()

    def test_missing_sbom_fails(self):
        self.sbom.unlink()
        self.rehash()
        with self.assertRaisesRegex(ValueError, "missing from checksums"):
            self.check()

    def test_path_traversal_and_duplicate_entries_fail(self):
        manifest = self.directory / "checksums.txt"
        original = manifest.read_text()
        for extra in ("0" * 64 + "  ../outside\n", original.splitlines()[0] + "\n"):
            with self.subTest(extra=extra):
                manifest.write_text(original + extra)
                with self.assertRaisesRegex(ValueError, "Malformed or duplicate"):
                    self.check()

    def test_signature_failure_is_fatal_in_default_mode(self):
        error = subprocess.CalledProcessError(1, "cosign verify-blob")
        with patch.object(CHECK.subprocess, "run", side_effect=error):
            with self.assertRaises(subprocess.CalledProcessError):
                CHECK.check(self.directory, self.version)


if __name__ == "__main__":
    unittest.main()
