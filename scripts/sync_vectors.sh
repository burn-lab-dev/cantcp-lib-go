#!/bin/sh
# Sync testdata/vectors.json with the protocol canon in cantcp-spec.
#
# Usage:
#   scripts/sync_vectors.sh                 # sibling ../cantcp-spec checkout
#   scripts/sync_vectors.sh --url URL       # raw canon from GitHub
#   scripts/sync_vectors.sh --source PATH   # explicit path
#   scripts/sync_vectors.sh --check         # fail on a difference
#
# Exit codes: 0 ok, 1 drift with --check, 2 error.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
default_source="$root/../cantcp-spec/vectors.json"
default_url="https://raw.githubusercontent.com/burn-lab-dev/cantcp-spec/main/vectors.json"
target="$root/testdata/vectors.json"
checksum="$root/testdata/vectors.sha256"

usage() {
	echo "usage: scripts/sync_vectors.sh [--source PATH | --url URL] [--check]" >&2
}

source_path=
url=
check=0

while [ $# -gt 0 ]; do
	case $1 in
	--source)
		[ $# -ge 2 ] || { usage; exit 2; }
		source_path=$2
		shift 2
		;;
	--url)
		[ $# -ge 2 ] || { usage; exit 2; }
		url=$2
		shift 2
		;;
	--check)
		check=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "sync_vectors: unknown argument $1" >&2
		usage
		exit 2
		;;
	esac
done

if [ -z "$source_path" ] && [ -z "$url" ]; then
	source_path=$default_source
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

if [ -n "$url" ]; then
	if ! command -v curl >/dev/null 2>&1; then
		echo "sync_vectors: curl is required for --url" >&2
		exit 2
	fi
	if ! curl -fsSL "$url" -o "$tmp"; then
		echo "sync_vectors: cannot fetch the canon at $url" >&2
		exit 2
	fi
else
	if [ ! -f "$source_path" ]; then
		echo "sync_vectors: cannot read the canon at $source_path" >&2
		exit 2
	fi
	cp "$source_path" "$tmp"
fi

# Validate the canon marker and the sections. python3 is present on the
# development machines and on the CI runners; without it the marker check is
# skipped and only the byte comparison runs.
if command -v python3 >/dev/null 2>&1; then
	if ! python3 - "$tmp" <<'PY'
import json
import sys

path = sys.argv[1]
try:
    data = json.load(open(path, encoding="utf-8"))
except Exception as exc:  # noqa: BLE001 - report any parse problem the same way
    print(f"sync_vectors: the canon is not valid JSON: {exc}", file=sys.stderr)
    sys.exit(1)
if data.get("protocol") != "cantcp" or data.get("version") != "v0":
    print(
        f"sync_vectors: unexpected canon {data.get('protocol')!r} {data.get('version')!r}",
        file=sys.stderr,
    )
    sys.exit(1)
for section in ("crc8", "frames", "packets"):
    if not data.get(section):
        print(f"sync_vectors: the canon section {section!r} is empty", file=sys.stderr)
        sys.exit(1)
PY
	then
		exit 2
	fi
fi

hash=$(sha256sum "$tmp" | cut -d' ' -f1)

if [ "$check" = 1 ]; then
	if [ -f "$target" ] && cmp -s "$tmp" "$target"; then
		echo "sync_vectors: vectors.json is in sync with the canon, sha256=$hash"
		exit 0
	fi
	echo "sync_vectors: vectors.json differs from the canon, run without --check" >&2
	exit 1
fi

if [ -f "$target" ] && cmp -s "$tmp" "$target"; then
	echo "sync_vectors: already current, sha256=$hash"
else
	cp "$tmp" "$target"
	echo "sync_vectors: updated, sha256=$hash"
fi
printf '%s  vectors.json\n' "$hash" >"$checksum"
