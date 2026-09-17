#!/bin/sh
# Regression tests for scripts/stage-receipt-fixtures.sh (R3 defects 6 & 7).
#
# Self-contained: every case builds a disposable repo skeleton under a temp
# dir, copies the staging script into the skeleton (so the tested bytes are
# the committed bytes), and points the skeleton's fixtureRoot.ts at a fake
# fixture root under that temp dir. Nothing here ever touches the real
# /tmp/phebs-receipts-fixtures.
#
# Run: sh scripts/test-stage-receipt-fixtures.sh
# Exit 0 only if every case passes; any failure prints its case name.
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
stage_src="$repo_root/scripts/stage-receipt-fixtures.sh"

work="$(mktemp -d "${TMPDIR:-/tmp}/stage-fixtures-test-XXXXXX")"
trap 'rm -rf "$work"' EXIT

n=0
pass=0
fail=0
ok() { n=$((n + 1)); pass=$((pass + 1)); printf 'ok %d - %s\n' "$n" "$1"; }
bad() { n=$((n + 1)); fail=$((fail + 1)); printf 'not ok %d - %s\n' "$n" "$1: $2"; }

# make_skeleton <name> [spike-dir-override]
# Builds $work/<name> as a fake repo root whose scripts/stage-receipt-fixtures.sh
# is a fresh copy of the committed script, whose docs/ and spike/ resolve to
# the real source bundles, and whose ui/receipts/fixtureRoot.ts points at a
# fake fixture root. Prints the skeleton path.
make_skeleton() {
  name="$1"
  skel="$work/$name"
  mkdir -p "$skel/scripts" "$skel/ui/receipts"
  cp "$stage_src" "$skel/scripts/stage-receipt-fixtures.sh"
  ln -s "$repo_root/docs" "$skel/docs"
  if [ "${2-}" != "" ]; then
    ln -s "$2" "$skel/spike"
  else
    ln -s "$repo_root/spike" "$skel/spike"
  fi
  fakeroot="$skel/fixture-root"
  printf "export const RECEIPT_FIXTURE_ROOT = '%s'\n" "$fakeroot" >"$skel/ui/receipts/fixtureRoot.ts"
  printf '%s' "$skel"
}

# staged_paths <skeleton> ; prints "<t307-dst> <t323-dst> <fakeroot>"
staged_paths() {
  skel="$1"
  fakeroot="$skel/fixture-root"
  printf '%s %s %s' \
    "$fakeroot/t307-neutral-service.bundle" \
    "$fakeroot/t323-neutral-corpus.bundle" \
    "$fakeroot"
}

mode644() {
  # $1: path. True iff it is a regular file with exactly -rw-r--r--.
  case "$(ls -ld "$1" 2>/dev/null)" in
    -rw-r--r--*) return 0 ;;
    *) return 1 ;;
  esac
}

no_temp_leftovers() {
  # $1: fixture root. True iff no .stage-bundle-* temp files remain.
  [ -z "$(find "$1" -maxdepth 1 -name '.stage-bundle-*' -print 2>/dev/null)" ]
}

# ---------------------------------------------------------------- defect 6

# 1: successful staging returns 0 and emits exactly the expected exports.
skel="$(make_skeleton d6-success)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
out="$work/d6-success.out"
if sh "$skel/scripts/stage-receipt-fixtures.sh" --env >"$out" 2>"$work/d6-success.err"; then
  if [ "$(wc -l <"$out")" -eq 2 ] \
    && grep -qx "export PHEBS_T307_NEUTRAL_SERVICE_REPO='$t307_dst'" "$out" \
    && grep -qx "export PHEBS_T344_SERVICE_SEARCH_REPO='$t323_dst'" "$out" \
    && [ -f "$t307_dst" ] && [ -f "$t323_dst" ] \
    && cmp -s "$repo_root/docs/fixtures/t30.7-neutral-service/t307-neutral-service.bundle" "$t307_dst" \
    && cmp -s "$repo_root/spike/t323/t323-neutral-corpus.bundle" "$t323_dst" \
    && mode644 "$t307_dst" && mode644 "$t323_dst" \
    && no_temp_leftovers "$fakeroot"; then
    ok "d6: successful staging returns 0 and emits the expected exports"
  else
    bad "d6: successful staging returns 0 and emits the expected exports" "output or staged bytes wrong"
  fi
else
  bad "d6: successful staging returns 0 and emits the expected exports" "nonzero exit"
fi

# 2: a failing staging run makes the CI wrapper exit nonzero BEFORE eval
# (and therefore before make dev could start). The wrapper below mirrors the
# checked capture-then-eval pattern in .github/workflows/ci.yml verbatim.
mkdir -p "$work/empty-spike"
skel="$(make_skeleton d6-failure "$work/empty-spike")"
script="$skel/scripts/stage-receipt-fixtures.sh"
if sh "$script" --env >/dev/null 2>&1; then
  bad "d6: failing staging script exits nonzero" "script exited 0 with a missing bundle"
else
  ok "d6: failing staging script exits nonzero"
fi
PHEBS_T307_NEUTRAL_SERVICE_REPO=""
PHEBS_T344_SERVICE_SEARCH_REPO=""
export PHEBS_T307_NEUTRAL_SERVICE_REPO PHEBS_T344_SERVICE_SEARCH_REPO
if (
  receipt_env="$(sh "$script" --env)" || exit 1
  eval "$receipt_env"
  touch "$work/d6-dev-started"
); then
  bad "d6: failing staging makes the CI wrapper exit nonzero before eval" "wrapper exited 0"
else
  if [ ! -e "$work/d6-dev-started" ] \
    && [ -z "$PHEBS_T307_NEUTRAL_SERVICE_REPO" ] \
    && [ -z "$PHEBS_T344_SERVICE_SEARCH_REPO" ]; then
    ok "d6: failing staging makes the CI wrapper exit nonzero before eval"
  else
    bad "d6: failing staging makes the CI wrapper exit nonzero before eval" "eval ran or dev sentinel exists"
  fi
fi

# ---------------------------------------------------------------- defect 7

# 3: missing root is created safely (real dir, owned by us, bundles staged).
skel="$(make_skeleton d7-missing-root)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
if sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>&1 \
  && [ -d "$fakeroot" ] && [ ! -L "$fakeroot" ] \
  && [ -f "$t307_dst" ] && [ -f "$t323_dst" ] \
  && mode644 "$t307_dst" && mode644 "$t323_dst" \
  && no_temp_leftovers "$fakeroot"; then
  ok "d7: missing root created safely"
else
  bad "d7: missing root created safely" "root not created or bundles wrong"
fi

# 4: identical bytes are reused; mtime and content are preserved.
skel="$(make_skeleton d7-identical)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>&1
touch "$work/d7-ref"
sleep 1
if sh "$skel/scripts/stage-receipt-fixtures.sh" --env >/dev/null 2>"$work/d7-identical.err"; then
  if [ "$t307_dst" -ot "$work/d7-ref" ] && [ "$t323_dst" -ot "$work/d7-ref" ] \
    && cmp -s "$repo_root/docs/fixtures/t30.7-neutral-service/t307-neutral-service.bundle" "$t307_dst" \
    && cmp -s "$repo_root/spike/t323/t323-neutral-corpus.bundle" "$t323_dst" \
    && no_temp_leftovers "$fakeroot"; then
    ok "d7: identical bytes reused, mtime and content preserved"
  else
    bad "d7: identical bytes reused, mtime and content preserved" "bundles were rewritten"
  fi
else
  bad "d7: identical bytes reused, mtime and content preserved" "restage exited nonzero"
fi

# 5: mismatched bytes are refused; the staged file is left untouched.
skel="$(make_skeleton d7-mismatch)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>&1
printf 'tampered-by-test-not-the-source-bundle\n' >"$t307_dst"
cp "$t307_dst" "$work/d7-tampered-copy"
if sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>"$work/d7-mismatch.err"; then
  bad "d7: mismatched bytes refused, staged file untouched" "restage exited 0 over differing bytes"
else
  if cmp -s "$t307_dst" "$work/d7-tampered-copy" \
    && grep -q "different bytes" "$work/d7-mismatch.err" \
    && no_temp_leftovers "$fakeroot"; then
    ok "d7: mismatched bytes refused, staged file untouched"
  else
    bad "d7: mismatched bytes refused, staged file untouched" "staged file was modified"
  fi
fi

# 6: a symlink at a destination is refused and its target is untouched.
skel="$(make_skeleton d7-dst-symlink)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>&1
printf 'decoy-target-content\n' >"$work/d7-decoy"
rm "$t307_dst"
ln -s "$work/d7-decoy" "$t307_dst"
if sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>"$work/d7-dst-symlink.err"; then
  bad "d7: symlink at destination refused, target untouched" "restage exited 0 over a symlink"
else
  if [ -L "$t307_dst" ] \
    && [ "$(cat "$work/d7-decoy")" = "decoy-target-content" ] \
    && grep -q "is a symlink" "$work/d7-dst-symlink.err"; then
    ok "d7: symlink at destination refused, target untouched"
  else
    bad "d7: symlink at destination refused, target untouched" "symlink followed or target modified"
  fi
fi

# 7: a symlink at the fixture root is refused and never traversed.
skel="$(make_skeleton d7-root-symlink)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
mkdir -p "$work/d7-root-target"
rm -rf "$fakeroot"
ln -s "$work/d7-root-target" "$fakeroot"
if sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>"$work/d7-root-symlink.err"; then
  bad "d7: symlink at fixture root refused, never traversed" "staging exited 0 through a symlinked root"
else
  if [ -z "$(ls -A "$work/d7-root-target" 2>/dev/null)" ] \
    && grep -q "is a symlink" "$work/d7-root-symlink.err"; then
    ok "d7: symlink at fixture root refused, never traversed"
  else
    bad "d7: symlink at fixture root refused, never traversed" "wrote through the symlink"
  fi
fi

# 8: a non-directory at the fixture root is refused.
skel="$(make_skeleton d7-root-file)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
printf 'not-a-directory\n' >"$fakeroot"
if sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>&1; then
  bad "d7: non-directory at fixture root refused" "staging exited 0 over a regular file"
else
  ok "d7: non-directory at fixture root refused"
fi

# 9: a root owned by another user is refused (needs privilege to set up).
skel="$(make_skeleton d7-root-owner)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
mkdir -p "$fakeroot"
if [ "$(id -u)" -eq 0 ] && id nobody >/dev/null 2>&1; then
  chown nobody:nogroup "$fakeroot"
  if sh "$skel/scripts/stage-receipt-fixtures.sh" >/dev/null 2>"$work/d7-root-owner.err"; then
    bad "d7: wrong-owner root refused" "staging exited 0 on a foreign-owned root"
  else
    if grep -q "not owned by" "$work/d7-root-owner.err"; then
      ok "d7: wrong-owner root refused"
    else
      bad "d7: wrong-owner root refused" "refused for the wrong reason"
    fi
  fi
  chown root:root "$fakeroot"
else
  printf 'SKIP d7: wrong-owner root refused (needs uid 0 to chown; running as uid %s)\n' "$(id -u)"
fi

# 10: --print-root is a read-only probe: exits 0, prints the root, stages nothing.
skel="$(make_skeleton d7-print-root)"
# shellcheck disable=SC2086
set -- $(staged_paths "$skel"); t307_dst="$1"; t323_dst="$2"; fakeroot="$3"
if [ "$(sh "$skel/scripts/stage-receipt-fixtures.sh" --print-root 2>/dev/null)" = "$fakeroot" ] \
  && [ ! -e "$fakeroot" ]; then
  ok "d7: --print-root is a read-only probe"
else
  bad "d7: --print-root is a read-only probe" "wrong output or staged something"
fi

# 11: unknown flags still exit 2.
skel="$(make_skeleton d7-usage)"
rc=0
sh "$skel/scripts/stage-receipt-fixtures.sh" --bogus >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then
  ok "d7: unknown flag exits 2"
else
  bad "d7: unknown flag exits 2" "exit was $rc"
fi

# ---------------------------------------------------------------- summary
printf '%s\n' "----"
printf 'passed %d/%d\n' "$pass" "$n"
if [ "$fail" -ne 0 ]; then
  printf '%d case(s) FAILED\n' "$fail" >&2
  exit 1
fi
exit 0
