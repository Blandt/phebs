#!/bin/sh
# Stage the neutral-demo receipt bundles at the fixed, documented receipt
# fixture root so the dev instance derives the same `local/...` repository
# identities on every machine (macOS checkout, Ubuntu CI, or any other
# checkout location).
#
# The fixture root is defined once, in ui/receipts/fixtureRoot.ts
# (RECEIPT_FIXTURE_ROOT); this script extracts it from there and fails
# closed if the extraction yields nothing usable, so the shell and
# TypeScript sides cannot drift apart.
#
# Safety contract (the fixture root is a shared /tmp path, so every write
# is validated before it happens):
#   * the root must be a real directory owned by the current user; a
#     symlink, a non-directory, or a foreign-owned root is refused;
#   * a missing root is created with plain mkdir (never mkdir -p, which
#     would silently follow a planted symlink), then re-validated;
#   * each destination is inspected BEFORE writing: symlinks, non-regular
#     files, and files not owned by the current user are refused;
#   * a staged bundle whose bytes already match the source is reused
#     untouched (content, mtime, and mode preserved);
#   * a staged bundle whose bytes differ is refused, never truncated or
#     overwritten in place -- remove it by hand if restaging is intended;
#   * first publication copies to a temp file in the same directory and
#     renames it into place, so a concurrent reader never sees a partial
#     bundle.
#
# Usage:
#   scripts/stage-receipt-fixtures.sh             stage, print a summary
#   scripts/stage-receipt-fixtures.sh --env       stage, print `export` lines for eval
#   scripts/stage-receipt-fixtures.sh --print-root print the fixture root only
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
fixture_ts="$repo_root/ui/receipts/fixtureRoot.ts"

# Single source of truth: the canonical root lives in fixtureRoot.ts.
# shellcheck disable=SC2016
fixture_root="$(sed -n "s/^export const RECEIPT_FIXTURE_ROOT = '\([^']*\)'.*/\1/p" "$fixture_ts")"
case "$fixture_root" in
  /*) ;;
  *)
    printf 'stage-receipt-fixtures: could not read an absolute RECEIPT_FIXTURE_ROOT from %s\n' "$fixture_ts" >&2
    exit 1
    ;;
esac
case "$fixture_root" in
  */)
    printf 'stage-receipt-fixtures: refusing fixture root with trailing slash: %s\n' "$fixture_root" >&2
    exit 1
    ;;
esac

t307_src="$repo_root/docs/fixtures/t30.7-neutral-service/t307-neutral-service.bundle"
t323_src="$repo_root/spike/t323/t323-neutral-corpus.bundle"
t307_dst="$fixture_root/t307-neutral-service.bundle"
t323_dst="$fixture_root/t323-neutral-corpus.bundle"

for src in "$t307_src" "$t323_src"; do
  if [ ! -f "$src" ]; then
    printf 'stage-receipt-fixtures: missing fixture bundle %s\n' "$src" >&2
    exit 1
  fi
done

mode="${1---stage}"
case "$mode" in
  --print-root)
    printf '%s\n' "$fixture_root"
    exit 0
    ;;
  --env | --stage) ;;
  *)
    printf 'usage: stage-receipt-fixtures.sh [--env|--print-root]\n' >&2
    exit 2
    ;;
esac

refuse() {
  printf 'stage-receipt-fixtures: refusing: %s\n' "$1" >&2
  exit 1
}

owned_by_me() {
  # $1: an existing path that is not a symlink. True iff its owner uid is
  # the current uid. Any lookup failure compares unequal, i.e. fail closed.
  [ "$(ls -ldn "$1" 2>/dev/null | awk '{print $3}')" = "$(id -u)" ]
}

# --- Fixture root: must be a real directory owned by the current user. ---
if [ -L "$fixture_root" ]; then
  refuse "fixture root $fixture_root is a symlink"
fi
if [ -e "$fixture_root" ]; then
  if [ ! -d "$fixture_root" ]; then
    refuse "fixture root $fixture_root is not a directory"
  fi
  if ! owned_by_me "$fixture_root"; then
    refuse "fixture root $fixture_root is not owned by uid $(id -u)"
  fi
else
  # Plain mkdir, not mkdir -p: mkdir fails on an existing symlink instead of
  # silently following it. A lost creation race re-validates whatever is
  # there now instead of trusting the earlier absence check.
  if ! mkdir "$fixture_root" 2>/dev/null; then
    if [ -L "$fixture_root" ]; then
      refuse "fixture root $fixture_root appeared as a symlink"
    fi
    if [ ! -d "$fixture_root" ]; then
      refuse "fixture root $fixture_root is not a directory"
    fi
    if ! owned_by_me "$fixture_root"; then
      refuse "fixture root $fixture_root is not owned by uid $(id -u)"
    fi
  fi
fi

stage_one() {
  # $1: source bundle, $2: destination path. Never overwrites in place.
  src="$1"
  dst="$2"
  if [ -L "$dst" ]; then
    refuse "destination $dst is a symlink"
  fi
  if [ -e "$dst" ]; then
    if [ ! -f "$dst" ]; then
      refuse "destination $dst is not a regular file"
    fi
    if ! owned_by_me "$dst"; then
      refuse "destination $dst is not owned by uid $(id -u)"
    fi
    if cmp -s "$src" "$dst"; then
      printf 'stage-receipt-fixtures: reusing identical staged bundle %s\n' "$dst" >&2
      return 0
    fi
    refuse "destination $dst exists with different bytes; refusing to overwrite in place (remove it first to restage)"
  fi
  # Publish via a temp file in the same directory, then rename into place:
  # concurrent readers only ever see the complete bundle, never a partial
  # copy. mktemp creates the temp file mode 600; it becomes 644 below.
  tmp="$(mktemp "$fixture_root/.stage-bundle-XXXXXX")" || refuse "could not create temp file in $fixture_root"
  if cp "$src" "$tmp" && chmod 644 "$tmp" && mv "$tmp" "$dst"; then
    :
  else
    rc=$?
    rm -f "$tmp"
    printf 'stage-receipt-fixtures: failed to publish %s\n' "$dst" >&2
    exit "$rc"
  fi
}

stage_one "$t307_src" "$t307_dst"
stage_one "$t323_src" "$t323_dst"

if [ "$mode" = "--env" ]; then
  # Stdout carries ONLY the export lines in --env mode: every diagnostic
  # above goes to stderr so eval sees exactly the two assignments.
  printf 'export PHEBS_T307_NEUTRAL_SERVICE_REPO=%s\n' "'$t307_dst'"
  printf 'export PHEBS_T344_SERVICE_SEARCH_REPO=%s\n' "'$t323_dst'"
else
  printf 'staged receipt fixtures at %s\n' "$fixture_root"
  printf '  %s\n  %s\n' "$t307_dst" "$t323_dst"
fi
