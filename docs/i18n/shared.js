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
  - id: windsurf
    name: Windsurf
    project_dir: .windsurf/skills
    global_dir: ~/.codeium/windsurf/skills
    detect: ~/.codeium/windsurf`;
export const REQUIRES = (d) => `---
name: report
description: ${d}
metadata:
  requires: pdf anthropics/skills#docx
---`;
export const INDEX = (d) => `version: 1
skills:
  - name: pdf
    source: anthropics/skills@v1.0.0
    description: ${d}`;
