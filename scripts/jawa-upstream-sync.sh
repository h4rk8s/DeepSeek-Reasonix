#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/jawa-upstream-sync.sh [options]

Default behavior:
  - fetch origin/main-v2
  - create an isolated temporary worktree from the current local branch
  - rebase that temp branch onto origin/main-v2
  - run the fast local verification gate
  - leave the production checkout, ~/.reasonix/config.toml, and ~/.local/bin untouched

Options:
  --apply        Move the current checkout to the verified rebased commit.
  --install      Build current checkout and install bin/reasonix to ~/.local/bin/reasonix.
                 Implies --apply.
  --push         Push the applied branch with --force-with-lease to h4rk8s.
                 Implies --apply.
  --full-test    Run go test -count=1 ./... after the fast verification gate.
  --keep         Keep the temporary worktree after a successful run.
  --help         Show this help.

Environment overrides:
  UPSTREAM_REMOTE=origin
  UPSTREAM_BRANCH=main-v2
  PUSH_REMOTE=h4rk8s
  INSTALL_PATH=$HOME/.local/bin/reasonix
  WORKTREE_ROOT=<optional override; must remain inside <repo>/.worktree>
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

run() {
  printf '+ %s\n' "$*"
  "$@"
}

apply=0
install_bin=0
push_branch=0
full_test=0
keep_worktree=0

while (($#)); do
  case "$1" in
    --apply)
      apply=1
      ;;
    --install)
      install_bin=1
      apply=1
      ;;
    --push)
      push_branch=1
      apply=1
      ;;
    --full-test)
      full_test=1
      ;;
    --keep)
      keep_worktree=1
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
  shift
done

repo=$(git rev-parse --show-toplevel)
cd "$repo"

upstream_remote=${UPSTREAM_REMOTE:-origin}
upstream_branch=${UPSTREAM_BRANCH:-main-v2}
push_remote=${PUSH_REMOTE:-h4rk8s}
install_path=${INSTALL_PATH:-"$HOME/.local/bin/reasonix"}
local_branch=$(git branch --show-current)

[[ -n "$local_branch" ]] || die "current checkout is detached; switch to the local integration branch first"
[[ -x scripts/verify-tui-regression.sh ]] || die "scripts/verify-tui-regression.sh is missing or not executable"

if ! git diff --quiet || ! git diff --cached --quiet; then
  die "tracked changes are present in $repo; commit/stash them before syncing upstream"
fi

printf '== Reasonix upstream sync ==\n'
printf 'repo: %s\n' "$repo"
printf 'local_branch: %s\n' "$local_branch"
printf 'upstream: %s/%s\n' "$upstream_remote" "$upstream_branch"
printf 'push_remote: %s\n' "$push_remote"
printf '\n'

run git config rerere.enabled true
run git config rerere.autoupdate true
run git fetch "$upstream_remote" "$upstream_branch" --tags

upstream_ref="$upstream_remote/$upstream_branch"
read -r ahead behind < <(git rev-list --left-right --count "HEAD...$upstream_ref")
printf 'delta: local ahead %s, upstream ahead %s\n' "$ahead" "$behind"

if [[ "$behind" == "0" ]]; then
  printf 'already up to date with %s\n' "$upstream_ref"
  exit 0
fi

stamp=$(date -u +%Y%m%d%H%M%S)
worktree_root=${WORKTREE_ROOT:-"$repo/.worktree"}
case "$worktree_root/" in
  "$repo/.worktree/"*) ;;
  *) die "WORKTREE_ROOT must be inside $repo/.worktree" ;;
esac
case "/$worktree_root/" in
  *"/../"*) die "WORKTREE_ROOT must not contain parent-directory traversal" ;;
esac
mkdir -p "$repo/.worktree" "$worktree_root"
repo_worktree_root=$(cd "$repo/.worktree" && pwd -P)
worktree_root=$(cd "$worktree_root" && pwd -P)
case "$worktree_root/" in
  "$repo_worktree_root/"*) ;;
  *) die "WORKTREE_ROOT must be inside $repo/.worktree" ;;
esac
sync_branch="jawa/reasonix-upstream-sync-${stamp}-$$"
sync_dir="$worktree_root/2026-07-14-reasonix-upstream-sync-${stamp}-$$"
sync_ok=0

cleanup() {
  status=$?
  if [[ "$sync_ok" == "1" && "$keep_worktree" == "0" ]]; then
    git -C "$repo" worktree remove "$sync_dir" >/dev/null 2>&1 || true
    git -C "$repo" branch -D "$sync_branch" >/dev/null 2>&1 || true
  else
    printf '\nkept sync worktree for inspection: %s\n' "$sync_dir" >&2
    printf 'sync branch: %s\n' "$sync_branch" >&2
  fi
  exit "$status"
}
trap cleanup EXIT

printf '\n== Create isolated worktree ==\n'
run git worktree add -b "$sync_branch" "$sync_dir" "$local_branch"
cd "$sync_dir"

printf '\n== Rebase local patch stack ==\n'
if ! git rebase "$upstream_ref"; then
  printf '\nrebase stopped with conflicts.\n' >&2
  printf 'Resolve inside: %s\n' "$sync_dir" >&2
  printf 'Then run: git rebase --continue\n' >&2
  exit 2
fi

printf '\n== Patch stack range-diff ==\n'
git range-diff "$upstream_ref...$local_branch" "$upstream_ref...HEAD" || true

printf '\n== Fast verification gate ==\n'
run scripts/verify-tui-regression.sh
run go test -count=1 ./internal/config ./internal/control ./internal/agent

if [[ "$full_test" == "1" ]]; then
  printf '\n== Full Go test suite ==\n'
  run go test -count=1 ./...
fi

new_head=$(git rev-parse HEAD)
new_head_short=$(git rev-parse --short HEAD)
printf '\nverified rebased head: %s\n' "$new_head_short"

if [[ "$apply" == "1" ]]; then
  printf '\n== Apply verified head to production checkout ==\n'
  cd "$repo"
  current_branch=$(git branch --show-current)
  [[ "$current_branch" == "$local_branch" ]] || die "production checkout moved to $current_branch; expected $local_branch"
  if ! git diff --quiet || ! git diff --cached --quiet; then
    die "tracked changes appeared in production checkout; refusing to reset"
  fi
  run git reset --keep "$new_head"
  run make build

  if [[ "$install_bin" == "1" ]]; then
    printf '\n== Install production binary ==\n'
    mkdir -p "$(dirname "$install_path")"
    run install -m 0755 bin/reasonix "$install_path"
    run "$install_path" --version
  fi

  if [[ "$push_branch" == "1" ]]; then
    printf '\n== Push local integration branch ==\n'
    run git push --force-with-lease "$push_remote" "HEAD:$local_branch"
  fi
fi

sync_ok=1
printf '\nupstream sync finished successfully.\n'
