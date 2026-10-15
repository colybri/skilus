#!/usr/bin/env node
// Builds skilus.dev: the home and download pages from site/ and the
// documentation from the docs/ folder of each release tag.
//
// Every minor release (v1.0, v1.1…) gets its own copy of the documentation,
// taken from its newest patch tag and published as /doc/vX.Y.0/. Each page
// is a real file, /doc/<version>/<lang>/<page>/index.html, so links work
// without a fragment. /doc/<lang>/<page>/ and /doc/ redirect to the newest
// version. The home and download pages, /<lang>/ and /<lang>/download/, and
// the install scripts, /install and /install.ps1, come from site/ in the
// checked-out tree and point at the newest release; / and /download/ send the
// reader to their language.
//
// Usage: node scripts/build-site.mjs [--out _site] [--worktree vX.Y.Z]
//   --worktree publishes the working tree's docs/ as that version, to preview
//   documentation that is not tagged yet.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const args = process.argv.slice(2);
const opt = (name, def) => { const i = args.indexOf(name); return i >= 0 ? args[i + 1] : def; };
const out = path.resolve(opt('--out', '_site'));
const worktree = opt('--worktree', '');
const repo = execFileSync('git', ['rev-parse', '--show-toplevel'], { encoding: 'utf8' }).trim();
const semver = /^v(\d+)\.(\d+)\.(\d+)$/;

if (worktree && !semver.test(worktree)) throw new Error(`--worktree needs a version such as v1.0.0, not ${worktree}`);

// The newest patch of every minor version from 1.0 on.
const versions = new Map();
for (const tag of execFileSync('git', ['tag', '--list', 'v*'], { cwd: repo, encoding: 'utf8' }).split('\n')) {
  const m = semver.exec(tag.trim());
  if (!m || Number(m[1]) < 1) continue;
  const label = `v${m[1]}.${m[2]}.0`;
  const prev = versions.get(label);
  if (!prev || Number(m[3]) > prev.patch) versions.set(label, { label, tag: m[0], patch: Number(m[3]) });
}
if (worktree) {
  const m = semver.exec(worktree);
  versions.set(`v${m[1]}.${m[2]}.0`, { label: `v${m[1]}.${m[2]}.0`, tag: null });
}
if (versions.size === 0) throw new Error('no release tag from v1.0.0 on; use --worktree to publish the working tree');

const order = [...versions.values()].sort((a, b) => cmp(b.label, a.label));
const latest = order[0].label;

fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(path.join(out, 'doc'), { recursive: true });

let langs = [], pages = [], latestDir = '', shared = null, langMeta = [];
for (const v of order) {
  const dir = path.join(out, 'doc', v.label);
  extractDocs(v.tag, dir);
  fs.rmSync(path.join(dir, 'CNAME'), { force: true });
  // content.js is an ES module; say so, so Node reads it without guessing.
  fs.writeFileSync(path.join(dir, 'package.json'), '{"type":"module"}\n');
  const content = (await import(pathToFileURL(path.join(dir, 'content.js')).href)).default;
  if (v.label === latest) shared = await import(pathToFileURL(path.join(dir, 'i18n', 'shared.js')).href);
  fs.rmSync(path.join(dir, 'package.json'));
  const codes = content.langs.map(l => l.code);
  if (v.label === latest) { langs = codes; pages = content.order; latestDir = dir; langMeta = content.langs; }

  const shell = fs.readFileSync(path.join(dir, 'index.html'), 'utf8').replace('<head>',
    `<head>\n<base href="/doc/${v.label}/">\n<meta name="skilus-doc-version" content="${v.label}">`);
  if (shell.indexOf('<base ') < 0) throw new Error(`${v.label}: index.html has no <head>`);
  fs.writeFileSync(path.join(dir, 'index.html'), shell);
  for (const lang of codes) {
    for (const page of content.order) write(path.join(dir, lang, page, 'index.html'), shell);
    write(path.join(dir, lang, 'index.html'), redirect(`/doc/${v.label}/${lang}/overview/`));
  }
  console.log(`${v.label} (${v.tag || 'working tree'}): ${codes.length} languages × ${content.order.length} pages`);
}

fs.writeFileSync(path.join(out, 'doc', 'versions.json'), JSON.stringify(order.map(v => v.label)) + '\n');

// Version-less links, such as the README's, always open the newest version.
for (const lang of langs) {
  for (const page of pages) write(path.join(out, 'doc', lang, page, 'index.html'), redirect(`/doc/${latest}/${lang}/${page}/`));
  write(path.join(out, 'doc', lang, 'index.html'), redirect(`/doc/${latest}/${lang}/overview/`));
}
write(path.join(out, 'doc', 'index.html'), redirect(`/doc/${latest}/{lang}/overview/`, langs));

// Home and download pages. The binaries they offer are the newest release's;
// before the first tag, the version being previewed stands in for it.
const site = await import(pathToFileURL(path.join(repo, 'site', 'pages.mjs')).href);
const newest = [...versions.values()].filter(v => v.tag).sort((a, b) => cmp(b.tag, a.tag))[0];
const release = (newest ? newest.tag : worktree).slice(1);
const goVersion = /^go (\d+\.\d+)/m.exec(fs.readFileSync(path.join(repo, 'go.mod'), 'utf8'))[1];
for (const l of langMeta) {
  const file = path.join(repo, 'site', 'i18n', `${l.code}.json`);
  if (!fs.existsSync(file)) throw new Error(`site/i18n/${l.code}.json is missing`);
  const ctx = {
    lang: l.code, dir: l.dir, s: JSON.parse(fs.readFileSync(file, 'utf8')), langs: langMeta, version: release,
    docs: (page) => `/doc/${latest}/${l.code}/${page}/`, agents: shared.AGENT_ROWS.length, go: goVersion, cosign: shared.COSIGN,
  };
  write(path.join(out, l.code, 'index.html'), site.home(ctx));
  write(path.join(out, l.code, 'download', 'index.html'), site.download(ctx));
}
// Old links of the form /#/<lang>/<page> still open that documentation page.
write(path.join(out, 'index.html'), redirect('/{lang}/', langs, latest));
write(path.join(out, 'download', 'index.html'), redirect('/{lang}/download/', langs));
fs.cpSync(path.join(repo, 'site', 'assets'), path.join(out, 'assets'), { recursive: true });
fs.cpSync(path.join(repo, 'site', 'demo'), path.join(out, 'assets', 'demo'), { recursive: true });
for (const name of ['install', 'install.sh']) fs.copyFileSync(path.join(repo, 'site', 'install.sh'), path.join(out, name));
fs.copyFileSync(path.join(repo, 'site', 'install.ps1'), path.join(out, 'install.ps1'));
fs.copyFileSync(path.join(latestDir, 'favicon.ico'), path.join(out, 'favicon.ico'));
fs.cpSync(path.join(latestDir, 'icons'), path.join(out, 'icons'), { recursive: true });

write(path.join(out, '404.html'), notFound(order.map(v => v.label), langs));
fs.copyFileSync(path.join(repo, 'docs', 'CNAME'), path.join(out, 'CNAME'));

function extractDocs(tag, dir) {
  if (!tag) { fs.cpSync(path.join(repo, 'docs'), dir, { recursive: true }); return; }
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'skilus-docs-'));
  const tar = path.join(tmp, 'docs.tar');
  execFileSync('git', ['archive', '--format=tar', '-o', tar, tag, 'docs'], { cwd: repo });
  execFileSync('tar', ['-xf', tar, '-C', tmp]);
  fs.cpSync(path.join(tmp, 'docs'), dir, { recursive: true });
  fs.rmSync(tmp, { recursive: true, force: true });
}

function write(file, text) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, text);
}

function cmp(a, b) {
  const x = semver.exec(a).slice(1).map(Number), y = semver.exec(b).slice(1).map(Number);
  return x[0] - y[0] || x[1] - y[1] || x[2] - y[2];
}

// redirect returns a page that sends the browser to target. With langs,
// {lang} becomes the reader's language: the one chosen on the site before,
// else the browser's, else English. With hashVersion, an old /#/<lang>/<page>
// link goes to that page in that version.
function redirect(target, langs = [], hashVersion = '') {
  const fallback = target.replace('{lang}', 'en');
  const script = langs.length ? `<script>
(function () {
  var langs = ${JSON.stringify(langs)}, lang = '';
  try { lang = JSON.parse(localStorage.getItem('skilus-docs') || '{}').lang || ''; } catch (e) {}
  var hash = ${hashVersion ? `location.hash.replace(/^#\\/?/, '').split('/')` : '[]'};
  if (hash[0] && langs.indexOf(hash[0]) >= 0) {
    location.replace('/doc/${hashVersion}/' + hash[0] + '/' + (hash[1] || 'overview') + '/');
    return;
  }
  if (langs.indexOf(lang) < 0) {
    lang = 'en';
    var wanted = navigator.languages || [navigator.language || ''];
    for (var i = 0; i < wanted.length; i++) {
      var code = String(wanted[i]).toLowerCase().split('-')[0];
      if (code === 'nb' || code === 'nn') code = 'no';
      if (langs.indexOf(code) >= 0) { lang = code; break; }
    }
  }
  location.replace(${JSON.stringify(target)}.replace('{lang}', lang));
})();
</script>` : `<script>location.replace(${JSON.stringify(target)} + location.hash);</script>`;
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>skilus</title>
<meta name="robots" content="noindex">
<link rel="canonical" href="${fallback}">
<noscript><meta http-equiv="refresh" content="0; url=${fallback}"></noscript>
${script}
</head>
<body><a href="${fallback}">skilus documentation</a></body>
</html>
`;
}

// notFound sends a page missing from one version, for example one added in
// a later release, to that version's overview, other documentation links to
// /doc/ and anything else to the home page.
function notFound(labels, langs) {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>skilus</title>
<meta name="robots" content="noindex">
<script>
(function () {
  var parts = location.pathname.split('/').filter(Boolean), labels = ${JSON.stringify(labels)}, langs = ${JSON.stringify(langs)};
  if (parts[0] === 'doc' && labels.indexOf(parts[1]) >= 0 && langs.indexOf(parts[2]) >= 0) {
    location.replace('/doc/' + parts[1] + '/' + parts[2] + '/overview/');
  } else if (parts[0] === 'doc') {
    location.replace('/doc/');
  } else {
    location.replace('/');
  }
})();
</script>
</head>
<body><a href="/">skilus</a></body>
</html>
`;
}
