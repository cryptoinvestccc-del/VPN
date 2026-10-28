'use strict';
/* What drifts over the panel's backdrop: snow in winter, dandelion seeds in
   summer (data-season on <html>, set by the panel). Both are rendered in
   Blender: snowflakes.webp holds 24 ice crystals, fluff.webp 12 seeds, each
   at three sharpnesses for a dark sky and the same with a faint grey rim
   for a bright one. Far ones are small and crisp, a few near the lens large
   and out of focus; each turns and tips as it goes, in its own eddy, with
   slow gusts of wind. It stops while the tab is hidden, stays off when the
   system asks for reduced motion, and the panel can turn it off
   (window.axmSnow, remembered in this browser). */
(function () {
  var KEY = 'axm_snow';
  var CELL = 96, COLS = 6;
  var KINDS = {
    winter: { src: 'assets/snowflakes.webp', kinds: 24, rows: 4 },
    summer: { src: 'assets/fluff.webp', kinds: 12, rows: 2 }
  };
  var reduced = window.matchMedia ? matchMedia('(prefers-reduced-motion: reduce)') : { matches: false };
  var darkQuery = window.matchMedia ? matchMedia('(prefers-color-scheme: dark)') : { matches: false };
  var canvas, ctx, flakes = [], w = 0, h = 0, dpr = 1, raf = 0, last = 0, t = 0, onLight = false;
  var season = 'winter', sheets = {};

  function stored() { try { return localStorage.getItem(KEY) !== 'off'; } catch (e) { return true; } }
  function store(on) { try { localStorage.setItem(KEY, on ? 'on' : 'off'); } catch (e) { /* storage off */ } }
  function sheet(name) {
    if (!sheets[name]) {
      var img = new Image(), s = { img: img, ready: false };
      img.onload = function () { s.ready = true; };
      img.src = KINDS[name].src;
      sheets[name] = s;
    }
    return sheets[name];
  }
  function readRoot() {
    var root = document.documentElement;
    var th = root.getAttribute('data-theme');
    onLight = th ? th === 'light' : !darkQuery.matches;
    var se = root.getAttribute('data-season') === 'summer' ? 'summer' : 'winter';
    if (se !== season) {
      season = se;
      flakes = [];
      if (canvas) size();
    }
    sheet(season);
  }

  // depth: 0 far (crisp), 1 middle, 2 near the lens (blurred)
  function snowflake(anyY) {
    var roll = Math.random(), f;
    if (roll < 0.58) f = { size: 4 + Math.random() * 4, vy: 26 + Math.random() * 18, sway: 8 + Math.random() * 8, a: 0.75, lv: 0, par: 0.35 };
    else if (roll < 0.9) f = { size: 11 + Math.random() * 8, vy: 50 + Math.random() * 30, sway: 16 + Math.random() * 14, a: 0.95, lv: Math.random() < 0.6 ? 0 : 1, par: 0.7 };
    else f = { size: 26 + Math.random() * 20, vy: 120 + Math.random() * 70, sway: 30 + Math.random() * 30, a: 0.6, lv: 2, par: 1.4 };
    f.spin = (Math.random() < 0.5 ? -1 : 1) * (0.25 + Math.random() * 1.1);
    f.tipv = 0.4 + Math.random() * 1.2;
    return f;
  }
  // seeds float: slow, carried by the wind, now and then lifted
  function seed(anyY) {
    var roll = Math.random(), f;
    if (roll < 0.5) f = { size: 12 + Math.random() * 8, vy: 6 + Math.random() * 8, sway: 10 + Math.random() * 10, a: 0.8, lv: 0, par: 0.6 };
    else if (roll < 0.9) f = { size: 24 + Math.random() * 14, vy: 10 + Math.random() * 12, sway: 18 + Math.random() * 16, a: 0.95, lv: Math.random() < 0.6 ? 0 : 1, par: 1.0 };
    else f = { size: 60 + Math.random() * 40, vy: 22 + Math.random() * 18, sway: 30 + Math.random() * 30, a: 0.55, lv: 2, par: 1.8 };
    f.spin = (Math.random() < 0.5 ? -1 : 1) * (0.05 + Math.random() * 0.25);
    f.tipv = 0.15 + Math.random() * 0.35;
    f.lift = 0.5 + Math.random() * 0.8;
    return f;
  }
  function particle(anyY) {
    var f = season === 'summer' ? seed(anyY) : snowflake(anyY);
    f.kind = Math.floor(Math.random() * KINDS[season].kinds);
    f.x = Math.random() * (w + 200) - 100;
    f.y = anyY ? Math.random() * h : -f.size - Math.random() * 80;
    f.rot = Math.random() * 6.283;
    f.tip = Math.random() * 6.283;
    f.p1 = Math.random() * 6.283; f.p2 = Math.random() * 6.283;
    f.f1 = 0.25 + Math.random() * 0.5; f.f2 = 0.9 + Math.random() * 1.4;
    return f;
  }
  function size() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    w = window.innerWidth; h = window.innerHeight;
    canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
    var per = season === 'summer' ? 26000 : 4300, cap = season === 'summer' ? 70 : 320;
    var want = Math.min(cap, Math.round(w * h / per));
    while (flakes.length < want) flakes.push(particle(true));
    flakes.length = want;
  }
  function wind(time) {
    var g = 10 + Math.sin(time * 0.21) * 16 + Math.sin(time * 0.057 + 1.3) * 22 + Math.sin(time * 0.83) * 4;
    return season === 'summer' ? g * 1.6 + 12 : g;
  }
  function frame(now) {
    raf = requestAnimationFrame(frame);
    var dt = Math.min(0.05, (now - (last || now)) / 1000);
    last = now; t += dt;
    var wv = wind(t), summer = season === 'summer';
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    var sh = sheets[season];
    if (!sh || !sh.ready) return;
    var rows = KINDS[season].rows, block = onLight ? 3 : 0;
    // ice and down on a dark sky glow: add light instead of covering
    ctx.globalCompositeOperation = onLight ? 'source-over' : 'screen';
    for (var i = 0; i < flakes.length; i++) {
      var f = flakes[i];
      var dx = Math.sin(t * f.f1 + f.p1) * f.sway + Math.sin(t * f.f2 + f.p2) * f.sway * 0.25;
      var vy = f.vy + Math.sin(t * f.f2 * 0.7 + f.p1) * f.vy * 0.12;
      if (summer) vy -= Math.max(0, Math.sin(t * 0.3 * f.lift + f.p2)) * f.vy * 1.6;   // a thermal lifts it
      f.y += vy * dt;
      f.x += (wv * f.par + dx) * dt;
      f.rot += f.spin * dt;
      f.tip += f.tipv * dt;
      if (f.y > h + f.size || f.y < -f.size * 3) { flakes[i] = particle(false); continue; }
      if (f.x > w + 100) f.x -= w + 200; else if (f.x < -100) f.x += w + 200;
      // a flat crystal or a seed's parachute seen edge-on as it tips: squash one axis
      var sq = summer ? 0.55 + 0.45 * Math.abs(Math.cos(f.tip)) : 0.3 + 0.7 * Math.abs(Math.cos(f.tip));
      var c = Math.cos(f.rot), s = Math.sin(f.rot), k = f.size / CELL * dpr;
      ctx.setTransform(c * k, s * k, -s * k * sq, c * k * sq, f.x * dpr, f.y * dpr);
      ctx.globalAlpha = f.a * (0.85 + 0.15 * sq);
      var sx = (f.kind % COLS) * CELL, sy = ((block + f.lv) * rows + Math.floor(f.kind / COLS)) * CELL;
      ctx.drawImage(sh.img, sx, sy, CELL, CELL, -CELL / 2, -CELL / 2, CELL, CELL);
    }
    ctx.globalAlpha = 1;
    ctx.globalCompositeOperation = 'source-over';
  }
  function start() {
    if (raf || !canvas || reduced.matches || !stored() || document.hidden) return;
    canvas.hidden = false;
    last = 0;
    raf = requestAnimationFrame(frame);
  }
  function stop(hide) {
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    if (hide && canvas) { canvas.hidden = true; ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.clearRect(0, 0, canvas.width, canvas.height); }
  }

  function init() {
    canvas = document.createElement('canvas');
    canvas.className = 'snow';
    canvas.setAttribute('aria-hidden', 'true');
    canvas.hidden = true;
    document.body.insertBefore(canvas, document.body.firstChild);
    ctx = canvas.getContext('2d');
    if (!ctx) return;
    readRoot();
    new MutationObserver(readRoot).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-season'] });
    if (darkQuery.addEventListener) darkQuery.addEventListener('change', readRoot);
    size();
    window.addEventListener('resize', size);
    document.addEventListener('visibilitychange', function () { if (document.hidden) stop(false); else start(); });
    if (reduced.addEventListener) reduced.addEventListener('change', function () { if (reduced.matches) stop(true); else start(); });
    start();
  }

  window.axmSnow = {
    available: function () { return !reduced.matches; },
    enabled: function () { return stored() && !reduced.matches; },
    set: function (on) { store(on); if (on) start(); else stop(true); }
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init); else init();
})();
