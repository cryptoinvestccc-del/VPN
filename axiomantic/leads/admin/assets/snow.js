'use strict';
/* Snow falling over the panel's backdrop: a canvas behind everything.
   Three depths of flakes, a slow wind. It stops while the tab is hidden,
   stays off when the system asks for reduced motion, and the panel can
   turn it off (window.axmSnow, remembered in this browser). */
(function () {
  var KEY = 'axm_snow';
  var reduced = window.matchMedia ? matchMedia('(prefers-reduced-motion: reduce)') : { matches: false };
  var canvas, ctx, flakes = [], w = 0, h = 0, dpr = 1, raf = 0, last = 0, wind = 0, t = 0;

  function stored() { try { return localStorage.getItem(KEY) !== 'off'; } catch (e) { return true; } }
  function store(on) { try { localStorage.setItem(KEY, on ? 'on' : 'off'); } catch (e) { /* storage off */ } }

  function flake(anyY) {
    var depth = Math.random();                 // 0 far … 1 near
    return {
      x: Math.random() * w,
      y: anyY ? Math.random() * h : -10 - Math.random() * 40,
      r: 0.6 + depth * depth * 2.6,
      vy: 18 + depth * 52,                     // px per second
      sway: 6 + depth * 18,
      phase: Math.random() * Math.PI * 2,
      freq: 0.4 + Math.random() * 0.8,
      a: 0.35 + depth * 0.55
    };
  }
  function size() {
    dpr = Math.min(window.devicePixelRatio || 1, 1.5);
    w = window.innerWidth; h = window.innerHeight;
    canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    var want = Math.min(260, Math.round(w * h / 7000));
    while (flakes.length < want) flakes.push(flake(true));
    flakes.length = want;
  }
  function frame(now) {
    raf = requestAnimationFrame(frame);
    var dt = Math.min(0.05, (now - (last || now)) / 1000);
    last = now; t += dt;
    wind = Math.sin(t * 0.12) * 14 + Math.sin(t * 0.031) * 8;
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = '#fff';
    for (var i = 0; i < flakes.length; i++) {
      var f = flakes[i];
      f.y += f.vy * dt;
      f.x += (wind * (0.4 + f.r / 3) + Math.sin(t * f.freq + f.phase) * f.sway) * dt;
      if (f.y > h + 10) { flakes[i] = flake(false); continue; }
      if (f.x > w + 10) f.x -= w + 20; else if (f.x < -10) f.x += w + 20;
      ctx.globalAlpha = f.a;
      ctx.beginPath();
      ctx.arc(f.x, f.y, f.r, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;
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
    if (hide && canvas) { canvas.hidden = true; ctx.clearRect(0, 0, w, h); }
  }

  function init() {
    canvas = document.createElement('canvas');
    canvas.className = 'snow';
    canvas.setAttribute('aria-hidden', 'true');
    canvas.hidden = true;
    document.body.insertBefore(canvas, document.body.firstChild);
    ctx = canvas.getContext('2d');
    if (!ctx) return;
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
