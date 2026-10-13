"""Validate v-prefixed SemVer tags and emit GitHub Actions version outputs."""
import re
import sys


SEMVER_TAG = re.compile(
    r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
    r"(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
)


def release_version(tag):
    match = SEMVER_TAG.fullmatch(tag)
    if match:
        prerelease = match.group(4)
        if not prerelease or all(
            not part.isdigit() or len(part) == 1 or not part.startswith("0")
            for part in prerelease.split(".")
        ):
            return tag, bool(prerelease)
    raise ValueError(
        f"Invalid release tag {tag!r}: expected vMAJOR.MINOR.PATCH "
        "with optional SemVer prerelease/build metadata"
    )


def main(args):
    if len(args) != 1:
        print("usage: release_version.py vMAJOR.MINOR.PATCH", file=sys.stderr)
        return 1
    try:
        version, prerelease = release_version(args[0])
    except ValueError as error:
        print(error, file=sys.stderr)
        return 1
    print(f"version={version}")
    print(f"prerelease={str(prerelease).lower()}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
