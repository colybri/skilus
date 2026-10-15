// skilus.dev home and download pages: install tabs, copy buttons, theme and
// language, the visitor's system on the download page and the terminal
// recording on the home page (frames recorded by scripts/record-demo.py).
(function () {
  var doc = document.documentElement;
  var t = JSON.parse(document.getElementById('ui').textContent);

  function saved() { try { return JSON.parse(localStorage.getItem('skilus-docs') || '{}'); } catch (e) { return {}; } }
  function save(patch) {
    try { var s = saved(); for (var k in patch) s[k] = patch[k]; localStorage.setItem('skilus-docs', JSON.stringify(s)); } catch (e) {}
  }
  // The documentation reads the same key, so both remember one language and theme.
  save({ lang: doc.lang });

  // Theme
  var themeBtn = document.getElementById('theme');
  function paintTheme() { themeBtn.textContent = doc.dataset.theme === 'light' ? t.dark : t.light; }
  themeBtn.addEventListener('click', function () {
    doc.dataset.theme = doc.dataset.theme === 'light' ? 'dark' : 'light';
    save({ dark: doc.dataset.theme === 'dark' });
    paintTheme();
  });
  paintTheme();

  // Language
  document.getElementById('lang').addEventListener('change', function (e) {
    save({ lang: e.target.value });
    location.href = e.target.selectedOptions[0].dataset.href;
  });

  // Install tabs
  var tabs = [].slice.call(document.querySelectorAll('.tabs [role="tab"]'));
  tabs.forEach(function (b) {
    b.addEventListener('click', function () {
      tabs.forEach(function (x) { x.setAttribute('aria-selected', x === b); });
      document.getElementById('cmdtext').textContent = b.dataset.cmd;
    });
  });
  // Windows visitors start on the PowerShell tab.
  if (tabs.length && /Windows/.test(navigator.userAgent)) tabs[1].click();

  // Copy buttons
  document.querySelectorAll('[data-copy]').forEach(function (b) {
    b.addEventListener('click', function () {
      var el = document.getElementById(b.dataset.copy);
      function done() { b.textContent = t.copied; setTimeout(function () { b.textContent = t.copy; }, 1400); }
      function select() { var r = document.createRange(); r.selectNodeContents(el); var s = getSelection(); s.removeAllRanges(); s.addRange(r); }
      try { navigator.clipboard.writeText(el.textContent).then(done, select); } catch (e) { select(); }
    });
  });

  // The visitor's system on the download page
  var ua = navigator.userAgent, os = /Windows/.test(ua) ? 'windows' : /Mac/.test(ua) ? 'darwin' : /Linux|X11/.test(ua) && !/Android/.test(ua) ? 'linux' : '';
  if (os) {
    var guess = document.querySelector('[data-os="' + os + '"][data-arch="' + (os === 'darwin' ? 'arm64' : 'amd64') + '"] .lbl');
    if (guess) { var tag = document.createElement('span'); tag.className = 'here'; tag.textContent = t.thisDevice; guess.appendChild(tag); }
    // Apple Silicon or Intel cannot be told apart from the user agent; the
    // high-entropy hints say it in Chromium browsers.
    if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
      navigator.userAgentData.getHighEntropyValues(['architecture']).then(function (h) {
        var arch = h.architecture === 'arm' ? 'arm64' : h.architecture === 'x86' ? 'amd64' : '';
        var row = arch && document.querySelector('[data-os="' + os + '"][data-arch="' + arch + '"] .lbl');
        if (row && tag) row.appendChild(tag);
      }, function () {});
    }
  }

  // Terminal recording
  var screen = document.getElementById('screen');
  if (!screen) return;
  var ctrl = document.getElementById('ctrl');
  var NAMES = { add: 'add', inspect: 'inspect', verify: 'verify · sync', update: 'update' };
  var PS = '<span class="ps">~/acme-app</span> <span class="d">$</span> ';
  var reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  var scenes = [], run = 0, cur = 0, paused = false, lines = [], chips = [], visible = true;

  function esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }
  function render(cursor) { screen.innerHTML = lines.join('\n') + (cursor ? '<span class="cur"></span>' : ''); screen.scrollTop = screen.scrollHeight; }
  function sleep(ms, id) {
    return new Promise(function (resolve) {
      var left = reduce ? 0 : ms;
      (function tick() {
        if (id !== run) return resolve(false);
        if (paused || !visible) return setTimeout(tick, 120);
        if (left <= 0) return resolve(true);
        var step = Math.min(left, 50); left -= step; setTimeout(tick, step);
      })();
    });
  }
  function progress(p) {
    chips.forEach(function (c, i) {
      c.setAttribute('aria-current', i === cur);
      c.querySelector('b').style.width = (i < cur ? 100 : i === cur ? Math.round(p * 100) : 0) + '%';
    });
  }

  async function play(i, id) {
    cur = i; lines = []; render(true);
    var steps = scenes[i].steps, start = 0;
    for (var k = 0; k < steps.length; k++) {
      if (id !== run) return;
      progress(k / steps.length);
      var s = steps[k];
      if (s[0] === 'cmd') {
        lines.push(PS);
        for (var c = 1; c <= s[1].length; c++) {
          lines[lines.length - 1] = PS + esc(s[1].slice(0, c)); render(true);
          if (!reduce && !(await sleep(25 + Math.random() * 40, id))) return;
        }
        if (!(await sleep(380, id))) return;
        start = lines.length;
      } else if (s[0] === 'frame') {
        lines.length = start;
        lines.push.apply(lines, s[1]); render(false);
        if (!(await sleep(s[2], id))) return;
      } else if (s[0] === 'clear') {
        lines = []; render(true);
        if (!(await sleep(250, id))) return;
      }
    }
    lines.push(PS); render(true); progress(1);
    if (reduce) return;
    if (await sleep(300, id)) play((i + 1) % scenes.length, id);
  }
  function start(i) { run++; play(i, run); }

  // Only play while the terminal is on screen.
  if ('IntersectionObserver' in window) {
    new IntersectionObserver(function (e) { visible = e[0].isIntersecting; }).observe(screen);
  }

  fetch(screen.dataset.src).then(function (r) { if (!r.ok) throw r; return r.json(); })
    .catch(function () { return fetch(screen.dataset.fallback).then(function (r) { return r.json(); }); })
    .then(function (demo) {
      scenes = demo.scenes;
      ctrl.innerHTML = scenes.map(function (s, i) {
        return '<button type="button" data-i="' + i + '"><span class="bar"><b></b></span>' + esc(NAMES[s.name] || s.name) + '</button>';
      }).join('') + '<button type="button" class="pp" id="pp"></button>';
      chips = [].slice.call(ctrl.querySelectorAll('[data-i]'));
      chips.forEach(function (c) { c.addEventListener('click', function () { start(+c.dataset.i); }); });
      var pp = document.getElementById('pp');
      function paintPP() { pp.textContent = paused ? '▶ ' + t.play : '❚❚ ' + t.pause; }
      pp.addEventListener('click', function () { paused = !paused; paintPP(); });
      paintPP();
      start(0);
    });
})();
