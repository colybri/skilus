// Renders skilus.dev's home and download pages for one language.
// scripts/build-site.mjs calls these with the strings of site/i18n/<lang>.json
// and the facts of the newest release.

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
// md turns `code` into <code>, **text** into <strong> and fills {placeholders};
// everything else is text.
const md = (s, vars = {}) => esc(s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? vars[k] : m)))
  .replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');

const GITHUB = 'https://github.com/colybri/skilus';
const INSTALL = [
  ['curl', 'curl -fsSL https://skilus.dev/install | sh'],
  ['powershell', 'irm https://skilus.dev/install.ps1 | iex'],
  ['go', 'go install github.com/colybri/skilus/cmd/skilus@latest'],
];
const BINARIES = [
  ['darwin', 'arm64', 'macOS', 'Apple Silicon', 'tar.gz'],
  ['darwin', 'amd64', 'macOS', 'Intel', 'tar.gz'],
  ['linux', 'amd64', 'Linux', 'x64', 'tar.gz'],
  ['linux', 'arm64', 'Linux', 'ARM64', 'tar.gz'],
  ['windows', 'amd64', 'Windows', 'x64', 'zip'],
  ['windows', 'arm64', 'Windows', 'ARM64', 'zip'],
];
// The checksums and their cosign signature. build-site.mjs reads them from the
// release so the page shows them: GitHub only offers them as downloads.
export const SIGNED = ['checksums.txt', 'checksums.txt.sig', 'checksums.txt.pem'];
const AGENTS = [
  ['Claude Code', '.claude/skills', 'claude-code'],
  ['Codex', '.agents/skills', 'codex'],
  ['Cursor', '.agents/skills', 'cursor'],
  ['GitHub Copilot', '.agents/skills', 'github-copilot'],
  ['Windsurf', '.windsurf/skills', 'windsurf'],
];

const LOGO = '<svg width="20" height="20" viewBox="0 0 32 32" fill="none" aria-hidden="true"><path d="M21 11 A5 5 0 1 0 16 16 A5 5 0 1 1 11 21" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/><circle cx="21" cy="11" r="2.3" fill="currentColor"/><circle cx="11" cy="21" r="2.3" fill="var(--bg)" stroke="currentColor" stroke-width="1.6"/></svg>';
const ICONS = {
  darwin: '<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M16.4 12.6c0-2.4 2-3.6 2.1-3.7-1.1-1.7-2.9-1.9-3.5-1.9-1.5-.2-2.9.9-3.7.9-.8 0-1.9-.9-3.2-.8-1.6 0-3.1 1-4 2.4-1.7 3-.4 7.3 1.2 9.7.8 1.2 1.8 2.5 3 2.4 1.2 0 1.7-.8 3.1-.8 1.5 0 1.9.8 3.2.8 1.3 0 2.2-1.2 3-2.4.9-1.4 1.3-2.7 1.3-2.8 0 0-2.5-1-2.5-3.8zM14 5.5c.7-.8 1.1-1.9 1-3-1 0-2.1.7-2.8 1.5-.6.7-1.2 1.8-1 2.9 1.1.1 2.1-.6 2.8-1.4z"/></svg>',
  linux: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="M12 3c-2.2 0-3.5 1.9-3.5 4.4 0 1.6-.6 2.6-1.6 4-1.2 1.6-2.4 3.5-2.4 5.6 0 .8.3 1.4.9 1.8M12 3c2.2 0 3.5 1.9 3.5 4.4 0 1.6.6 2.6 1.6 4 1.2 1.6 2.4 3.5 2.4 5.6 0 .8-.3 1.4-.9 1.8M8 21h8"/><circle cx="10.4" cy="8" r=".8" fill="currentColor"/><circle cx="13.6" cy="8" r=".8" fill="currentColor"/></svg>',
  windows: '<svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M3 5.5 10.5 4.4v7.1H3zM11.5 4.3 21 3v8.5h-9.5zM3 12.5h7.5v7.1L3 18.5zM11.5 12.5H21V21l-9.5-1.3z"/></svg>',
};

// ctx: { lang, dir, s (strings), langs [{code, native}], version ('1.0.0'),
//        docs (page) => url, agents (count), go ('1.25'), cosign }
export function home(ctx) {
  const { s } = ctx;
  const vars = { version: 'v' + ctx.version, more: ctx.agents - 4, n: ctx.agents };
  const body = `
<main>
  <section class="sec">
    <a class="badge" href="/${ctx.lang}/download/"><span>${esc(s.new)}</span> ${md(s.badge, vars)} →</a>
    <h1>${md(s.h1)}</h1>
    <p class="lead">${md(s.lead, vars)}</p>
    <div class="install">
      <div class="tabs" role="tablist" aria-label="${esc(s.installMethod)}">
        ${INSTALL.map(([k, c], i) => `<button role="tab" type="button" aria-selected="${i === 0}" data-cmd="${esc(c)}">${k}</button>`).join('')}
      </div>
      <div class="cmd"><code><span class="pr">$ </span><span id="cmdtext">${esc(INSTALL[0][1])}</span></code><button class="copy" type="button" data-copy="cmdtext">${esc(s.copy)}</button></div>
    </div>
    <div class="player">
      <div class="term" role="img" aria-label="${esc(s.recording)}">
        <div class="tbar"><span class="dots"><i></i><i></i><i></i></span><span class="ttl">~/acme-app — skilus</span></div>
        <div class="screen" id="screen" data-src="/assets/demo/${ctx.lang}.json" data-fallback="/assets/demo/en.json"></div>
      </div>
      <div class="ctrl" id="ctrl"></div>
    </div>
  </section>
  <section class="sec what">
    <h2>${md(s.whatTitle)}</h2>
    <p>${md(s.whatText, vars)}</p>
    <ul class="feats">
      ${s.features.map(([title, text]) => `<li><span><b>${md(title, vars)}</b> <em>${md(text, vars)}</em></span></li>`).join('\n      ')}
    </ul>
    <a class="btn" href="${ctx.docs('overview')}">${esc(s.readDocs)} →</a>
  </section>
</main>`;
  return page(ctx, { title: s.title, path: '', current: 'home', body });
}

export function download(ctx) {
  const { s } = ctx;
  const vars = { version: 'v' + ctx.version, go: ctx.go, n: ctx.agents };
  const base = `${GITHUB}/releases/download/v${ctx.version}`;
  const labels = { curl: [s.unix, '', '$'], powershell: [s.windows, 'PowerShell', '&gt;'], go: [s.goLater, '', 'go'] };
  const body = `
<main>
  <section class="sec dlhead">
    <h1>${md(s.dlTitle)}</h1>
    <p>${md(s.dlLead, vars)}</p>
  </section>
  <section class="sec dsec">
    <h2><span class="n">[1]</span> ${md(s.terminalTitle)}</h2>
    <p class="sub">${md(s.terminalText)}</p>
    <div class="rows">
      ${INSTALL.map(([k, c]) => {
        const [label, small, icon] = labels[k];
        return `<div class="row"><span class="ico">${icon}</span><span class="lbl">${md(label, vars)}${small ? `<small>${small}</small>` : ''}</span><code id="i-${k}">${esc(c)}</code><span class="act"><button class="copy" type="button" data-copy="i-${k}">${esc(s.copy)}</button></span></div>`;
      }).join('\n      ')}
    </div>
  </section>
  <section class="sec dsec">
    <h2><span class="n">[2]</span> ${md(s.binTitle)}</h2>
    <p class="sub">${md(s.binText)}</p>
    <div class="rows">
      ${BINARIES.map(([os, arch, name, variant, ext]) => {
        const file = `skilus_${ctx.version}_${os}_${arch}.${ext}`;
        return `<div class="row" data-os="${os}" data-arch="${arch}"><span class="ico">${ICONS[os]}</span><span class="lbl">${name} (${variant})</span><span class="file">${file}</span><span class="act"><a class="dlbtn" href="${base}/${file}">${esc(s.downloadBtn)}</a></span></div>`;
      }).join('\n      ')}
    </div>
    <div class="extra"><span>${esc(s.allFiles)}</span>${SIGNED.map((name, i) => ctx.files
      ? `<button type="button" class="show" data-show="f-${i}" aria-expanded="false" aria-controls="f-${i}">${name}</button>`
      : `<a href="${base}/${name}">${name}</a>`).join('')}</div>
    ${ctx.files ? SIGNED.map((name, i) => `<div class="viewer" id="f-${i}" hidden><div class="vbar"><span>${name}</span><a href="${base}/${name}">${esc(s.downloadBtn)}</a><button class="copy" type="button" data-copy="fv-${i}">${esc(s.copy)}</button></div><pre id="fv-${i}"${name === 'checksums.txt' ? '' : ' class="wrap"'}>${esc(ctx.files[name].trimEnd())}</pre></div>`).join('\n    ') : ''}
  </section>
  <section class="sec dsec">
    <h2><span class="n">[3]</span> ${md(s.verifyTitle)}</h2>
    <p class="sub">${md(s.verifyText)}</p>
    <div class="block"><pre id="cosign">${esc(ctx.cosign)}</pre><button class="copy" type="button" data-copy="cosign">${esc(s.copy)}</button></div>
  </section>
  <section class="sec dsec">
    <h2><span class="n">[4]</span> ${md(s.agentsTitle)}</h2>
    <p class="sub">${md(s.agentsText)}</p>
    <div class="rows">
      ${AGENTS.map(([name, dir, id]) => `<div class="row"><span class="lbl">${name}</span><span class="file">${dir}</span><span class="act"><code>--agent ${id}</code></span></div>`).join('\n      ')}
    </div>
    <div class="extra"><a href="${ctx.docs('agents')}">${md(s.seeAll, vars)} →</a></div>
  </section>
</main>`;
  return page(ctx, { title: md(s.dlPageTitle), path: 'download/', current: 'download', body });
}

function page(ctx, { title, path, current, body }) {
  const { s, lang } = ctx;
  const ui = { copy: s.copy, copied: s.copied, pause: s.pause, play: s.play, dark: s.dark, light: s.light, thisDevice: s.thisDevice };
  return `<!DOCTYPE html>
<html lang="${lang}" dir="${ctx.dir || 'ltr'}" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>${title}</title>
<meta name="description" content="${esc(s.description)}">
<link rel="canonical" href="https://skilus.dev/${lang}/${path}">
${ctx.langs.map((l) => `<link rel="alternate" hreflang="${l.code}" href="https://skilus.dev/${l.code}/${path}">`).join('\n')}
<link rel="alternate" hreflang="x-default" href="https://skilus.dev/${path}">
<link rel="icon" href="/favicon.ico" sizes="any">
<link rel="icon" href="/icons/icon.svg" type="image/svg+xml">
<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">
<script>try{if(JSON.parse(localStorage.getItem('skilus-docs')||'{}').dark===false)document.documentElement.dataset.theme='light'}catch(e){}</script>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=Inter:wght@400;500&family=Noto+Sans+Arabic:wght@400;500&family=Noto+Sans+Devanagari:wght@400;500&family=Noto+Sans+JP:wght@400;500&family=Noto+Sans+SC:wght@400;500&display=swap">
<link rel="stylesheet" href="/assets/site.css">
</head>
<body>
<div class="site">
<header class="top">
  <a class="brand" href="/${lang}/"><span class="logo">${LOGO}</span>skilus</a>
  <nav class="links" aria-label="skilus">
    <a href="${ctx.docs('overview')}">${esc(s.docs)}</a>
    <a href="${GITHUB}">GitHub</a>
    <a href="/${lang}/download/" class="dl"${current === 'download' ? ' aria-current="page"' : ''}>${esc(s.download)}</a>
  </nav>
</header>
${body}
<footer class="foot">
  <a href="${GITHUB}">GitHub</a><a href="${ctx.docs('overview')}">${esc(s.docs)}</a><a href="${GITHUB}/blob/main/LICENSE">PolyForm Noncommercial 1.0.0</a>
  <span class="sp"></span>
  <button type="button" id="theme"></button>
  <select id="lang" aria-label="${esc(s.language)}">
    ${ctx.langs.map((l) => `<option value="${l.code}" data-href="/${l.code}/${path}"${l.code === lang ? ' selected' : ''}>${esc(l.native)}</option>`).join('\n    ')}
  </select>
</footer>
</div>
<script type="application/json" id="ui">${JSON.stringify(ui).replace(/</g, '\\u003c')}</script>
<script src="/assets/site.js"></script>
</body>
</html>
`;
}
