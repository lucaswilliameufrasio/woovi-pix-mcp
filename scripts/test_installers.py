import hashlib
import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
INSTALLER = ROOT / "scripts" / "woovi-pix-mcp-installer.sh"


class ShellInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.fixture = self.root / "fixture"
        self.fixture.mkdir()
        self.stubs = self.root / "stubs"
        self.stubs.mkdir()

        self.asset = "woovi-pix-mcp_1.2.3_linux_amd64.tar.gz"
        binary = self.fixture / "woovi-pix-mcp"
        binary.write_text("#!/bin/sh\necho installed\n")
        binary.chmod(0o755)
        with tarfile.open(self.fixture / self.asset, "w:gz") as archive:
            archive.add(binary, arcname="woovi-pix-mcp")
        digest = hashlib.sha256((self.fixture / self.asset).read_bytes()).hexdigest()
        (self.fixture / "checksums.txt").write_text(f"{digest}  {self.asset}\n")

        (self.stubs / "uname").write_text(
            '#!/bin/sh\ncase "$1" in -s) echo "$INSTALL_TEST_OS";; '
            '-m) echo "$INSTALL_TEST_ARCH";; esac\n'
        )
        (self.stubs / "curl").write_text(
            "#!/usr/bin/env python3\n"
            "import os, pathlib, shutil, sys\n"
            "args = sys.argv[1:]\n"
            "url = next(arg for arg in args if arg.startswith('http'))\n"
            "if url.endswith('/releases/latest'):\n"
            ' print(\'{"tag_name":"v1.2.3"}\')\n'
            "else:\n"
            " name = url.rsplit('/', 1)[-1]\n"
            " source = pathlib.Path(os.environ['INSTALL_TEST_FIXTURE']) / name\n"
            " if not source.is_file(): sys.exit(22)\n"
            " output = next(\n"
            "  (args[i + 1] for i, arg in enumerate(args[:-1]) if arg == '-o'), None)\n"
            " if output is None: sys.stdout.buffer.write(source.read_bytes())\n"
            " else: shutil.copyfile(source, output)\n"
        )
        for stub in self.stubs.iterdir():
            stub.chmod(0o755)

        self.env = os.environ | {
            "PATH": f"{self.stubs}:{os.environ['PATH']}",
            "INSTALL_TEST_FIXTURE": str(self.fixture),
            "INSTALL_TEST_OS": "Linux",
            "INSTALL_TEST_ARCH": "x86_64",
        }

    def run_installer(self, *args):
        return subprocess.run(
            ["sh", str(INSTALLER), *args],
            capture_output=True,
            text=True,
            env=self.env,
            check=False,
        )

    def test_installs_pinned_release_after_checksum_verification(self):
        result = self.run_installer("--tag", "v1.2.3", "--bin-dir", str(self.bin))

        self.assertEqual(result.returncode, 0, result.stderr)
        installed = self.bin / "woovi-pix-mcp"
        self.assertTrue(installed.is_file())
        self.assertTrue(os.access(installed, os.X_OK))
        self.assertIn("Checksum verified", result.stdout)

    def test_resolves_latest_release(self):
        result = self.run_installer("--bin-dir", str(self.bin))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Installed woovi-pix-mcp v1.2.3", result.stdout)

    def test_rejects_corrupt_archive_without_installing(self):
        (self.fixture / "checksums.txt").write_text(f"{'0' * 64}  {self.asset}\n")

        result = self.run_installer("--tag", "v1.2.3", "--bin-dir", str(self.bin))

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Checksum mismatch", result.stderr)
        self.assertFalse((self.bin / "woovi-pix-mcp").exists())

    def test_preserves_existing_installation_non_interactively(self):
        existing = self.bin / "woovi-pix-mcp"
        existing.write_text("keep me")

        result = self.run_installer("--tag", "v1.2.3", "--bin-dir", str(self.bin))

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(existing.read_text(), "keep me")

    def test_rejects_unsupported_platform(self):
        self.env["INSTALL_TEST_OS"] = "FreeBSD"

        result = self.run_installer("--tag", "v1.2.3", "--bin-dir", str(self.bin))

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Unsupported OS", result.stderr)


if __name__ == "__main__":
    unittest.main()
