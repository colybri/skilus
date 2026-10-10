#!/usr/bin/env bash
# Phase 1 gate: install 10 real skills from three public repositories,
# pinned to commits, and check that the lock matches the one committed in
# this directory. CI runs it on Linux, macOS and Windows, so passing means
# every OS produces the same lock.
#
# Usage: test/gate/run.sh <path to the skilus binary>
# GATE_UPDATE=1 rewrites the committed lock instead of comparing.
set -euo pipefail

bin=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/home" "$work/project"
export HOME="$work/home" USERPROFILE="$work/home" XDG_CONFIG_HOME= CLAUDE_CONFIG_DIR= CODEX_HOME=
cd "$work/project"

add() {
  "$bin" add "$@" --agent universal --yes --allow-scripts
}

add anthropics/skills@dbd4588f9e1033efb41dad4bef2f7947c8993d44 \
  --skill pdf --skill docx --skill skill-creator --skill mcp-builder
add vercel-labs/agent-skills@063bee94c3f4df8453406c830b0a7df0f2860278 \
  --skill vercel-react-best-practices --skill deploy-to-vercel --skill web-design-guidelines
add obra/superpowers@bb92a77741419a4ab5f06e711a283343f1ada0c3 \
  --skill brainstorming --skill systematic-debugging --skill test-driven-development

"$bin" list
test "$("$bin" list --json | grep -c '"name"')" -eq 10

for s in pdf docx skill-creator mcp-builder vercel-react-best-practices deploy-to-vercel \
  web-design-guidelines brainstorming systematic-debugging test-driven-development; do
  test -f ".agents/skills/$s/SKILL.md" || { echo "missing .agents/skills/$s/SKILL.md" >&2; exit 1; }
done

if [ "${GATE_UPDATE:-}" = 1 ]; then
  cp skilus.lock "$here/skilus.lock"
  echo "Lock de referencia actualizado."
  exit 0
fi

# The committed lock was produced on Linux; any difference means the hash or
# the lock depends on the OS.
diff --strip-trailing-cr "$here/skilus.lock" skilus.lock
echo "Puerta de la fase 1 superada: 10 skills, mismo lock."
