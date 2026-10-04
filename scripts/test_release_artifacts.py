import hashlib
import io
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path

from check_release_artifacts import DOCUMENTATION, verify_checksums, verify_target_archive


class ChecksumTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)

    def test_validates_real_artifact(self):
        artifact = self.root / "binary.tar.gz"
        artifact.write_bytes(b"artifact content")
        digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
        (self.root / "checksums.txt").write_text(f"{digest}  binary.tar.gz\n")

        self.assertEqual(verify_checksums(self.root), {"binary.tar.gz": artifact})

    def test_rejects_missing_manifest(self):
        with self.assertRaisesRegex(ValueError, "checksums.txt missing"):
            verify_checksums(self.root)

    def test_rejects_corruption_missing_files_and_path_traversal(self):
        artifact = self.root / "binary.tar.gz"
        artifact.write_bytes(b"content")

        for name in ["binary.tar.gz", "missing.tar.gz", "../outside.tar.gz"]:
            (self.root / "checksums.txt").write_text(f"{'0' * 64}  {name}\n")

            with self.subTest(name=name), self.assertRaises(ValueError):
                verify_checksums(self.root)

    def test_rejects_duplicate_entries(self):
        artifact = self.root / "binary.tar.gz"
        artifact.write_bytes(b"content")
        digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
        entry = f"{digest}  binary.tar.gz\n"
        (self.root / "checksums.txt").write_text(entry * 2)

        with self.assertRaisesRegex(ValueError, "duplicate"):
            verify_checksums(self.root)


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)

    def write_archive(self, system, members):
        extension = ".zip" if system == "windows" else ".tar.gz"
        name = f"woovi-pix-mcp_0.1.0_{system}_amd64{extension}"
        path = self.root / name

        if system == "windows":
            with zipfile.ZipFile(path, "w") as archive:
                for member in members:
                    archive.writestr(member, b"fixture")
        else:
            with tarfile.open(path, "w:gz") as archive:
                for member in members:
                    entry = tarfile.TarInfo(member)
                    entry.size = len(b"fixture")
                    archive.addfile(entry, io.BytesIO(b"fixture"))

        return {name: path}

    def test_verifies_real_zip_and_tar_archives(self):
        for system in ["linux", "darwin", "windows"]:
            binary = "woovi-pix-mcp.exe" if system == "windows" else "woovi-pix-mcp"
            artifacts = self.write_archive(system, [binary, *DOCUMENTATION])

            with self.subTest(system=system):
                verify_target_archive(artifacts, system, "amd64")

    def test_rejects_missing_target(self):
        with self.assertRaisesRegex(ValueError, "exactly one archive"):
            verify_target_archive({}, "linux", "arm64")

    def test_rejects_incomplete_archive(self):
        artifacts = self.write_archive("linux", ["README.md"])

        with self.assertRaisesRegex(ValueError, "missing binary or documentation"):
            verify_target_archive(artifacts, "linux", "amd64")


if __name__ == "__main__":
    unittest.main()
