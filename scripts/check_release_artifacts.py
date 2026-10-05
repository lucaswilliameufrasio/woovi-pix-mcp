"""Verify a GoReleaser snapshot without publishing or opening provider connections."""

import argparse
import hashlib
import subprocess
import tarfile
import zipfile
from pathlib import Path

SYSTEMS = ("linux", "darwin", "windows")
ARCHITECTURES = ("amd64", "arm64")
DOCUMENTATION = ("LICENSE", "README.md", "CHANGELOG.md", "docs/sandbox.md", "docs/releases.md")
CHECKSUM_EXTRAS = {"woovi-pix-mcp-installer.sh", "woovi-pix-mcp-installer.ps1"}


def verify_checksums(root: Path) -> dict[str, Path]:
    manifest = root / "checksums.txt"
    if not manifest.is_file():
        raise ValueError("checksums.txt missing")

    artifacts = {}

    for line in manifest.read_text().splitlines():
        expected, name = line.split(maxsplit=1)
        name = name.strip().removeprefix("*")
        path = root / name

        if Path(name).name != name:
            raise ValueError("invalid or missing checksum artifact")

        if not path.is_file() and name in CHECKSUM_EXTRAS:
            path = Path(__file__).resolve().parent / name
        if not path.is_file():
            raise ValueError("invalid or missing checksum artifact")

        if name in artifacts:
            raise ValueError("duplicate checksum artifact")

        if hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise ValueError("artifact checksum mismatch")

        artifacts[name] = path

    return artifacts


def archive_members(path: Path) -> list[str]:
    if path.suffix == ".zip":
        with zipfile.ZipFile(path) as archive:
            return archive.namelist()

    with tarfile.open(path, "r:gz") as archive:
        return archive.getnames()


def verify_target_archive(artifacts: dict[str, Path], system: str, architecture: str) -> None:
    extension = ".zip" if system == "windows" else ".tar.gz"
    suffix = f"_{system}_{architecture}{extension}"
    matches = [path for name, path in artifacts.items() if name.endswith(suffix)]

    if len(matches) != 1:
        raise ValueError("exactly one archive per target is required")

    members = archive_members(matches[0])
    binary = "woovi-pix-mcp.exe" if system == "windows" else "woovi-pix-mcp"

    if any(required not in members for required in (binary, *DOCUMENTATION)):
        raise ValueError("archive is missing binary or documentation")


def smoke_test_native_binary(root: Path) -> str:
    binaries = list(root.glob("woovi-pix-mcp_linux_amd64*/woovi-pix-mcp"))
    if len(binaries) != 1:
        raise ValueError("native build missing or ambiguous")

    binary = str(binaries[0].resolve())
    output = subprocess.check_output([binary, "--version"], text=True)
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()

    valid_metadata = (
        output.startswith("woovi-pix-mcp ")
        and "woovi-pix-mcp dev " not in output
        and commit in output
    )

    if not valid_metadata:
        raise ValueError("release version/commit was not injected")

    subprocess.run([binary, "--help"], check=True, stdout=subprocess.DEVNULL)

    return output.strip()


def check_artifacts(root: Path) -> str:
    artifacts = verify_checksums(root)
    if missing := CHECKSUM_EXTRAS - artifacts.keys():
        raise ValueError(f"checksums.txt missing installer checksums: {', '.join(sorted(missing))}")

    for system in SYSTEMS:
        for architecture in ARCHITECTURES:
            verify_target_archive(artifacts, system, architecture)

    return smoke_test_native_binary(root)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()

    print(check_artifacts(args.directory))


if __name__ == "__main__":
    main()
