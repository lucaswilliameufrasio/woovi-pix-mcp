"""Pure validation helpers used by the release workflows (no publishing)."""

import argparse
import re
import subprocess
from pathlib import Path

VERSION = re.compile(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\Z")


def normalize_version(raw):
    if not VERSION.fullmatch(raw):
        raise ValueError("version must use stable SemVer X.Y.Z or vX.Y.Z, without leading zeros")

    return raw.removeprefix("v")


def validate_next(raw, tags):
    version = normalize_version(raw)
    wanted = tuple(map(int, version.split(".")))
    stable = [
        tuple(map(int, normalize_version(tag).split(".")))
        for tag in tags
        if tag.startswith("v") and VERSION.fullmatch(tag)
    ]

    if stable and wanted <= max(stable):
        raise ValueError("version must be newer than every existing stable release tag")

    return version


def validate_tag(raw, changelog):
    version = normalize_version(raw)

    if raw != "v" + version:
        raise ValueError("release tags must start with v")

    heading = rf"^## \[{re.escape(version)}\] - \d{{4}}-\d{{2}}-\d{{2}}$"

    if not re.search(heading, changelog, re.MULTILINE):
        raise ValueError("tag version must have a reviewed entry in CHANGELOG.md")

    return version


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["next", "tag"])
    parser.add_argument("version")
    args = parser.parse_args()

    try:
        if args.mode == "next":
            tags = subprocess.check_output(["git", "tag", "--list"], text=True).splitlines()
            print(validate_next(args.version, tags))
        else:
            print(validate_tag(args.version, Path("CHANGELOG.md").read_text()))
    except ValueError as exc:
        parser.exit(1, str(exc) + "\n")


if __name__ == "__main__":
    main()
