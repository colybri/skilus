#!/usr/bin/env bash
# Phase gates, run by CI on Linux, macOS and Windows.
#
# Phase 1: install 12 real skills from four public repositories, pinned to
# commits, and check that the lock matches the one committed in this
# directory, so every OS produces the same lock.
#
# Phase 2: with only the lock left, sync rebuilds the agents' directories
# and verify passes; a file changed by hand makes verify fail.
#
# Phase 3: a team that only has skilus.yaml, with a backend and a web
# profile, deploys backend and then moves to web with one command.
#
# Phase 4: search finds a real skill on skills.sh, and audit verifies the
# signatures of the installed ones and fails on an unsigned, untrusted one.
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
# openai/skills keeps its skills in skills/.curated/.
add openai/skills@49f948faa9258a0c61caceaf225e179651397431 \
  --skill define-goal --skill playwright

"$bin" list
test "$("$bin" list --json | grep -c '"name"')" -eq 12

for s in pdf docx skill-creator mcp-builder vercel-react-best-practices deploy-to-vercel \
  web-design-guidelines brainstorming systematic-debugging test-driven-development \
  define-goal playwright; do
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
echo "Puerta de la fase 1 superada: 12 skills, mismo lock."

"$bin" verify

# Start from the lock alone: no agents' directories and an empty store.
rm -rf .agents "$HOME/.skilus/store"
cp skilus.lock "$work/before.lock"
"$bin" sync
"$bin" verify
diff "$work/before.lock" skilus.lock

# A file changed by hand is caught (exit code 6) and named.
echo "# cambiado a mano" >> .agents/skills/pdf/SKILL.md
set +e
"$bin" verify > "$work/verify.out"
code=$?
set -e
cat "$work/verify.out"
test "$code" -eq 6 || { echo "verify exited $code, want 6" >&2; exit 1; }
grep -q 'modificado: SKILL.md' "$work/verify.out"

"$bin" sync --force
"$bin" verify

# The same commit read as a GitHub archive gives the same content hash.
archive=https://github.com/anthropics/skills/archive/dbd4588f9e1033efb41dad4bef2f7947c8993d44.tar.gz
from_archive=$("$bin" inspect "$archive" --skill pdf --json | sed -n 's/.*"tree_sha256": "\([0-9a-f]*\)".*/\1/p')
from_lock=$(awk '/^  pdf:/{p=1} p && /tree_sha256:/{print $2; exit}' skilus.lock)
test -n "$from_archive" && test "$from_archive" = "$from_lock" ||
  { echo "pdf from the archive: $from_archive; from git: $from_lock" >&2; exit 1; }
echo "Puerta de la fase 2 superada: sync reproduce el entorno, verify detecta el cambio y el tar.gz coincide con git."

# Phase 3: a new checkout of a team's repository holds only skilus.yaml,
# with the sources recorded above and two profiles.
mkdir "$work/team"
cp skilus.yaml "$work/team/skilus.yaml"
cd "$work/team"
cat >> skilus.yaml <<'YAML'
profiles:
  backend:
    skills: [systematic-debugging, test-driven-development, mcp-builder, skill-creator]
    agents: [claude-code]
  web:
    skills: [systematic-debugging, test-driven-development, vercel-react-best-practices, web-design-guidelines, playwright]
    agents: [universal]
YAML
cp skilus.yaml "$work/team.yaml"

installed() { "$bin" list --json | sed -n 's/.*"name": "\(.*\)".*/\1/p' | sort | tr '\n' ' '; }

# Scripts were accepted when the skills were added (allow: [scripts]), so
# --yes needs no --allow-scripts.
"$bin" profile use backend --yes
test "$(installed)" = "mcp-builder skill-creator systematic-debugging test-driven-development " ||
  { echo "backend installed: $(installed)" >&2; exit 1; }
test -f .claude/skills/mcp-builder/SKILL.md
test ! -e .agents/skills
"$bin" verify

"$bin" profile use web --yes
test "$(installed)" = "playwright systematic-debugging test-driven-development vercel-react-best-practices web-design-guidelines " ||
  { echo "web installed: $(installed)" >&2; exit 1; }
for s in systematic-debugging test-driven-development vercel-react-best-practices web-design-guidelines playwright; do
  test -f ".agents/skills/$s/SKILL.md" || { echo "missing .agents/skills/$s" >&2; exit 1; }
done
for s in mcp-builder skill-creator systematic-debugging test-driven-development; do
  test ! -e ".claude/skills/$s" || { echo ".claude/skills/$s should be gone" >&2; exit 1; }
done
"$bin" verify
"$bin" profile list | grep -q '^\* *web ' || { "$bin" profile list; echo "web is not active" >&2; exit 1; }
diff "$work/team.yaml" skilus.yaml
echo "Puerta de la fase 3 superada: el equipo pasa del perfil backend al web con un solo comando."

# Phase 4: search finds a real skill on skills.sh, which the gate installs.
"$bin" search pdf --owner anthropics --limit 5
"$bin" search pdf --owner anthropics --limit 5 --json | grep -q '"source": "anthropics/skills"' ||
  { echo "skills.sh did not return anthropics/skills for pdf" >&2; exit 1; }

# Phase 4: audit checks the 12 installed skills. Their pinned commits were
# signed on GitHub, so every signature verifies.
cd "$work/project"
"$bin" audit --scope project
"$bin" audit --scope project --json > "$work/audit.json"
verified=$(grep -c '"state": "verified"' "$work/audit.json" || true)
test "$verified" -eq 12 || { echo "$verified of 12 signatures verified" >&2; exit 1; }

# A skill from an unsigned commit outside the trust: list fails the audit
# in CI (code 5) and is named.
mkdir "$work/rogue"
printf -- '---\nname: rogue\ndescription: Sin firma\n---\n' > "$work/rogue/SKILL.md"
git -C "$work/rogue" init --quiet --initial-branch=main
git -C "$work/rogue" add .
git -C "$work/rogue" -c user.name=gate -c user.email=gate@example.com commit --quiet --no-gpg-sign -m v1
rogue="$work/rogue"
if command -v cygpath > /dev/null; then rogue="/$(cygpath -m "$rogue")"; fi
"$bin" add "file://$rogue" --agent universal --yes
cat >> skilus.yaml <<'YAML'
trust:
  - github.com/anthropics
  - github.com/vercel-labs
  - github.com/obra
  - github.com/openai
YAML
set +e
"$bin" audit --scope project --strict > "$work/audit.out" 2>&1
code=$?
set -e
cat "$work/audit.out"
test "$code" -eq 5 || { echo "audit --strict exited $code, want 5" >&2; exit 1; }
grep -q 'rogue: 2 warnings under --strict' "$work/audit.out"
grep -q 'aviso \[unsigned-commit\]' "$work/audit.out"
grep -q 'aviso \[untrusted-source\]' "$work/audit.out"
echo "Puerta de la fase 4 superada: search encuentra una skill real y audit detecta la que no tiene firma ni confianza."
