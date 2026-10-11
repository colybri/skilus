// Block helpers and shared code snippets for every language file.
export const h2 = (id, text) => ({ t: 'h2', id, text });
export const h3 = (text) => ({ t: 'h3', text });
export const p = (text) => ({ t: 'p', text });
export const code = (code, lang = 'console') => ({ t: 'code', code, lang });
export const ul = (...items) => ({ t: 'ul', items });
export const ol = (...items) => ({ t: 'ol', items });
export const note = (kind, text) => ({ t: 'note', kind, text });
export const table = (head, rows) => ({ t: 'table', head, rows });
export const cards = (...items) => ({ t: 'cards', items });
export const faq = (...items) => ({ t: 'faq', items });
export const dl = () => ({ t: 'dl' });
export const tabs = () => ({ t: 'tabs' });
export const only = (os, block) => ({ ...block, os });

export const COSIGN = String.raw`cosign verify-blob checksums.txt \
  --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/colybri/skilus/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt`;
export const TRUST = `trust:
  - github.com/anthropics
  - github.com/obra/superpowers`;
export const PROFILES = `profiles:
  backend:
    skills: [systematic-debugging, mcp-builder]
    agents: [claude-code]
  web:
    skills: [systematic-debugging, web-design-guidelines]`;
export const AGENTS = `version: 1
agents:
  - id: acme
    name: Acme Agent
    project_dir: .acme/skills
    global_dir: ~/.acme/skills
    detect: ~/.acme`;
// AGENT_ROWS lists the bundled agents (internal/adapter/catalog/agents.yaml);
// a test in that package keeps both in step.
export const AGENT_ROWS = [
  ['`claude-code`', 'Claude Code', '`.claude/skills`'],
  ['`codex`', 'Codex', '`.agents/skills`'],
  ['`cursor`', 'Cursor', '`.agents/skills`'],
  ['`github-copilot`', 'GitHub Copilot', '`.agents/skills`'],
  ['`adal`', 'AdaL', '`.adal/skills`'],
  ['`aider-desk`', 'AiderDesk', '`.aider-desk/skills`'],
  ['`amp`', 'Amp', '`.agents/skills`'],
  ['`antigravity`', 'Antigravity', '`.agents/skills`'],
  ['`antigravity-cli`', 'Antigravity CLI', '`.agents/skills`'],
  ['`augment`', 'Augment', '`.augment/skills`'],
  ['`autohand-code`', 'Autohand Code CLI', '`.autohand/skills`'],
  ['`bob`', 'IBM Bob', '`.bob/skills`'],
  ['`cline`', 'Cline', '`.agents/skills`'],
  ['`codearts-agent`', 'CodeArts Agent', '`.codeartsdoer/skills`'],
  ['`codebuddy`', 'CodeBuddy', '`.codebuddy/skills`'],
  ['`codemaker`', 'Codemaker', '`.codemaker/skills`'],
  ['`codestudio`', 'Code Studio', '`.codestudio/skills`'],
  ['`command-code`', 'Command Code', '`.commandcode/skills`'],
  ['`continue`', 'Continue', '`.continue/skills`'],
  ['`cortex`', 'Cortex Code', '`.cortex/skills`'],
  ['`crush`', 'Crush', '`.crush/skills`'],
  ['`deepagents`', 'Deep Agents', '`.agents/skills`'],
  ['`devin`', 'Devin for Terminal', '`.devin/skills`'],
  ['`dexto`', 'Dexto', '`.agents/skills`'],
  ['`droid`', 'Droid', '`.agents/skills`'],
  ['`firebender`', 'Firebender', '`.agents/skills`'],
  ['`forgecode`', 'ForgeCode', '`.forge/skills`'],
  ['`fx`', 'fx', '`.agents/skills`'],
  ['`gemini-cli`', 'Gemini CLI', '`.agents/skills`'],
  ['`goose`', 'Goose', '`.goose/skills`'],
  ['`grok`', 'Grok Build', '`.grok/skills`'],
  ['`hermes-agent`', 'Hermes Agent', '`.hermes/skills`'],
  ['`iflow-cli`', 'iFlow CLI', '`.iflow/skills`'],
  ['`inference-sh`', 'inference.sh', '`.inferencesh/skills`'],
  ['`jazz`', 'Jazz', '`.jazz/skills`'],
  ['`junie`', 'Junie', '`.junie/skills`'],
  ['`kilo`', 'Kilo Code', '`.agents/skills`'],
  ['`kimchi`', 'Kimchi', '`.kimchi/skills`'],
  ['`kimi-code-cli`', 'Kimi Code CLI', '`.agents/skills`'],
  ['`kiro-cli`', 'Kiro CLI', '`.kiro/skills`'],
  ['`kode`', 'Kode', '`.kode/skills`'],
  ['`lingma`', 'Lingma', '`.lingma/skills`'],
  ['`loaf`', 'Loaf', '`.agents/skills`'],
  ['`mcpjam`', 'MCPJam', '`.mcpjam/skills`'],
  ['`minimax-code`', 'MiniMax Code', '`.minimax/skills`'],
  ['`mistral-vibe`', 'Mistral Vibe', '`.vibe/skills`'],
  ['`moxby`', 'Moxby', '`.moxby/skills`'],
  ['`mux`', 'Mux', '`.mux/skills`'],
  ['`neovate`', 'Neovate', '`.neovate/skills`'],
  ['`ona`', 'Ona', '`.ona/skills`'],
  ['`openclaw`', 'OpenClaw', '`skills`'],
  ['`opencode`', 'OpenCode', '`.agents/skills`'],
  ['`openhands`', 'OpenHands', '`.openhands/skills`'],
  ['`pi`', 'Pi', '`.agents/skills`'],
  ['`pochi`', 'Pochi', '`.pochi/skills`'],
  ['`posit-assistant`', 'Posit Assistant', '`.posit/assistant/skills`'],
  ['`qoder`', 'Qoder', '`.qoder/skills`'],
  ['`qoder-cn`', 'Qoder CN', '`.qoder/skills`'],
  ['`qwen-code`', 'Qwen Code', '`.qwen/skills`'],
  ['`reasonix`', 'Reasonix', '`.reasonix/skills`'],
  ['`roo`', 'Roo Code', '`.roo/skills`'],
  ['`rovodev`', 'Rovo Dev', '`.rovodev/skills`'],
  ['`sarvam-code`', 'Sarvam Code', '`.agents/skills`'],
  ['`tabnine-cli`', 'Tabnine CLI', '`.tabnine/agent/skills`'],
  ['`terramind`', 'Terramind', '`.terramind/skills`'],
  ['`tinycloud`', 'Tinycloud', '`.tinycloud/skills`'],
  ['`trae`', 'Trae', '`.trae/skills`'],
  ['`trae-cn`', 'Trae CN', '`.trae/skills`'],
  ['`warp`', 'Warp', '`.agents/skills`'],
  ['`windsurf`', 'Windsurf', '`.windsurf/skills`'],
  ['`zcode`', 'ZCode', '`.zcode/skills`'],
  ['`zed`', 'Zed', '`.agents/skills`'],
  ['`zencoder`', 'Zencoder', '`.zencoder/skills`'],
  ['`zenflow`', 'Zenflow', '`.zencoder/skills`'],
];
export const REQUIRES = (d) => `---
name: report
description: ${d}
metadata:
  requires: pdf anthropics/skills#docx
---`;
export const MANIFEST = `version: 1
skills:
  - name: pdf
    source: anthropics/skills@v1.0.0
  - name: systematic-debugging
    source: obra/superpowers
    allow: [scripts]
profiles:
  docs:
    skills: [pdf]
    agents: [claude-code]
trust:
  - github.com/anthropics
indexes:
  - https://example.com/skills-index.yaml`;
export const INDEX = (d) => `version: 1
skills:
  - name: pdf
    source: anthropics/skills@v1.0.0
    description: ${d}`;

// Install scripts per OS. __VER__ is replaced with the latest release version when the page renders.
export const INSTALL_LINUX = `VERSION=__VER__
ARCH=$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)
FILE=skilus_\${VERSION}_linux_\${ARCH}.tar.gz
curl -fLO https://github.com/colybri/skilus/releases/download/v\${VERSION}/\${FILE}
tar -xzf "$FILE" skilus
sudo install -m 0755 skilus /usr/local/bin/skilus
skilus agents`;
export const INSTALL_MAC = `VERSION=__VER__
ARCH=$(uname -m | sed -e s/x86_64/amd64/)
FILE=skilus_\${VERSION}_darwin_\${ARCH}.tar.gz
curl -fLO https://github.com/colybri/skilus/releases/download/v\${VERSION}/\${FILE}
tar -xzf "$FILE" skilus
sudo mkdir -p /usr/local/bin
sudo install -m 0755 skilus /usr/local/bin/skilus
skilus agents`;
export const INSTALL_WIN = `$Version = "__VER__"
$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$Zip = "$env:TEMP\\skilus.zip"
Invoke-WebRequest "https://github.com/colybri/skilus/releases/download/v$Version/skilus_\${Version}_windows_$Arch.zip" -OutFile $Zip
$Dest = "$env:LOCALAPPDATA\\Programs\\skilus"
Expand-Archive $Zip -DestinationPath $Dest -Force
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$Dest*") { [Environment]::SetEnvironmentVariable("Path", "$UserPath;$Dest", "User") }
$env:Path += ";$Dest"
skilus agents`;
