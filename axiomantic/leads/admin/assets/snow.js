'use strict';
/* Snow falling over the panel's backdrop: a canvas behind everything.
   The flakes are 24 ice crystals rendered in Blender (snowflakes.webp:
   three sharpnesses for a dark sky, the same with a faint grey rim for a
   bright one). Far flakes are small and crisp, a few near the lens large
   and out of focus; each turns and tips over as it falls, in its own eddy,
   drifting with slow gusts of wind. It stops while the tab is hidden,
   stays off when the system asks for reduced motion, and the panel can
   turn it off (window.axmSnow, remembered in this browser). */
(function () {
  var KEY = 'axm_snow';
  var ATLAS = 'assets/snowflakes.webp';
  var CELL = 96, COLS = 6, ROWS = 4, KINDS = 24;
  var reduced = window.matchMedia ? matchMedia('(prefers-reduced-motion: reduce)') : { matches: false };
  var darkQuery = window.matchMedia ? matchMedia('(prefers-color-scheme: dark)') : { matches: false };
  var canvas, ctx, atlas, ready = false, flakes = [], w = 0, h = 0, dpr = 1, raf = 0, last = 0, t = 0, onLight = false;

  function stored() { try { return localStorage.getItem(KEY) !== 'off'; } catch (e) { return true; } }
  function store(on) { try { localStorage.setItem(KEY, on ? 'on' : 'off'); } catch (e) { /* storage off */ } }
  function pickTheme() {
    var th = document.documentElement.getAttribute('data-theme');
    onLight = th ? th === 'light' : !darkQuery.matches;
  }

  // depth: 0 far (crisp), 1 middle (crisp), 2 near the lens (blurred)
  function flake(anyY) {
    var roll = Math.random(), f;
    if (roll < 0.58) f = { size: 7 + Math.random() * 6, vy: 26 + Math.random() * 18, sway: 8 + Math.random() * 8, a: 0.75, lv: 0, par: 0.35 };
    else if (roll < 0.9) f = { size: 20 + Math.random() * 14, vy: 50 + Math.random() * 30, sway: 16 + Math.random() * 14, a: 0.95, lv: Math.random() < 0.6 ? 0 : 1, par: 0.7 };
    else f = { size: 44 + Math.random() * 36, vy: 120 + Math.random() * 70, sway: 30 + Math.random() * 30, a: 0.6, lv: 2, par: 1.4 };
    f.kind = Math.floor(Math.random() * KINDS);
    f.x = Math.random() * (w + 200) - 100;
    f.y = anyY ? Math.random() * h : -f.size - Math.random() * 80;
    f.rot = Math.random() * 6.283;
    f.spin = (Math.random() < 0.5 ? -1 : 1) * (0.25 + Math.random() * 1.1);
    f.tip = Math.random() * 6.283;                 // tipping over as it falls
    f.tipv = 0.4 + Math.random() * 1.2;
    f.p1 = Math.random() * 6.283; f.p2 = Math.random() * 6.283;
    f.f1 = 0.25 + Math.random() * 0.5; f.f2 = 0.9 + Math.random() * 1.4;
    return f;
  }
  function size() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    w = window.innerWidth; h = window.innerHeight;
    canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
    var want = Math.min(260, Math.round(w * h / 5600));
    while (flakes.length < want) flakes.push(flake(true));
    flakes.length = want;
  }
  function wind(time) {
    return 10 + Math.sin(time * 0.21) * 16 + Math.sin(time * 0.057 + 1.3) * 22 + Math.sin(time * 0.83) * 4;
  }
  function frame(now) {
    raf = requestAnimationFrame(frame);
    var dt = Math.min(0.05, (now - (last || now)) / 1000);
    last = now; t += dt;
    var wv = wind(t);
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    if (!ready) return;
    var block = onLight ? 3 : 0;
    // ice on a dark sky glows: add light instead of covering
    ctx.globalCompositeOperation = onLight ? 'source-over' : 'screen';
    for (var i = 0; i < flakes.length; i++) {
      var f = flakes[i];
      var dx = Math.sin(t * f.f1 + f.p1) * f.sway + Math.sin(t * f.f2 + f.p2) * f.sway * 0.25;
      f.y += (f.vy + Math.sin(t * f.f2 * 0.7 + f.p1) * f.vy * 0.12) * dt;
      f.x += (wv * f.par + dx) * dt;
      f.rot += f.spin * dt;
      f.tip += f.tipv * dt;
      if (f.y > h + f.size) { flakes[i] = flake(false); continue; }
      if (f.x > w + 100) f.x -= w + 200; else if (f.x < -100) f.x += w + 200;
      // a flat crystal seen edge-on as it tips: squash one axis
      var sq = 0.3 + 0.7 * Math.abs(Math.cos(f.tip));
      var c = Math.cos(f.rot), s = Math.sin(f.rot), k = f.size / CELL * dpr;
      ctx.setTransform(c * k, s * k, -s * k * sq, c * k * sq, f.x * dpr, f.y * dpr);
      ctx.globalAlpha = f.a * (0.85 + 0.15 * sq);   // a glint when it faces the light
      var sx = (f.kind % COLS) * CELL, sy = ((block + f.lv) * ROWS + Math.floor(f.kind / COLS)) * CELL;
      ctx.drawImage(atlas, sx, sy, CELL, CELL, -CELL / 2, -CELL / 2, CELL, CELL);
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
    atlas = new Image();
    atlas.onload = function () { ready = true; };
    atlas.src = ATLAS;
    pickTheme();
    new MutationObserver(pickTheme).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    if (darkQuery.addEventListener) darkQuery.addEventListener('change', pickTheme);
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
