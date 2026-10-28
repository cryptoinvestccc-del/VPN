'use strict';
/* Snow falling over the panel's backdrop: a canvas behind everything.
   Flakes sit at three depths like a camera sees them: far ones tiny and
   crisp, middle ones soft, a few near the lens large and out of focus.
   They tumble in their own eddies and drift with slow gusts of wind.
   It stops while the tab is hidden, stays off when the system asks for
   reduced motion, and the panel can turn it off (window.axmSnow,
   remembered in this browser). */
(function () {
  var KEY = 'axm_snow';
  var reduced = window.matchMedia ? matchMedia('(prefers-reduced-motion: reduce)') : { matches: false };
  var canvas, ctx, flakes = [], w = 0, h = 0, dpr = 1, raf = 0, last = 0, t = 0;
  var sprites = [];

  // soft round flakes drawn once: a bright core fading out, blurrier for
  // the flakes nearer the lens
  // On a bright sky real flakes read as grey specks with a light core, on a
  // dark one as white points: two sets, picked by the panel's theme.
  function sprite(soft, onLight) {
    var n = 64, c = document.createElement('canvas');
    c.width = c.height = n;
    var g = c.getContext('2d'), r = n / 2;
    var grad = g.createRadialGradient(r, r, 0, r, r, r);
    var core = 0.62 - soft * 0.5;              // 0.62 crisp … 0.12 bokeh
    if (onLight) {
      grad.addColorStop(0, 'rgba(255,255,255,1)');
      grad.addColorStop(core * 0.8, 'rgba(250,252,255,' + (0.95 - soft * 0.4) + ')');
      grad.addColorStop(Math.min(0.9, core + 0.18 + soft * 0.3), 'rgba(96,108,132,' + (0.42 - soft * 0.22) + ')');
      grad.addColorStop(1, 'rgba(96,108,132,0)');
    } else {
      grad.addColorStop(0, 'rgba(255,255,255,1)');
      grad.addColorStop(core, 'rgba(255,255,255,' + (0.95 - soft * 0.45) + ')');
      grad.addColorStop(Math.min(0.98, core + 0.25 + soft * 0.3), 'rgba(255,255,255,' + (0.25 - soft * 0.1) + ')');
      grad.addColorStop(1, 'rgba(255,255,255,0)');
    }
    g.fillStyle = grad;
    g.fillRect(0, 0, n, n);
    return c;
  }
  var setLight = [], setDark = [];
  var darkQuery = window.matchMedia ? matchMedia('(prefers-color-scheme: dark)') : { matches: false };
  function pickSprites() {
    var th = document.documentElement.getAttribute('data-theme');
    var dark = th ? th === 'dark' : darkQuery.matches;
    sprites = dark ? setDark : setLight;
  }

  function stored() { try { return localStorage.getItem(KEY) !== 'off'; } catch (e) { return true; } }
  function store(on) { try { localStorage.setItem(KEY, on ? 'on' : 'off'); } catch (e) { /* storage off */ } }

  function flake(anyY) {
    var roll = Math.random(), f;
    if (roll < 0.62) {          // far: small, crisp, slow
      f = { size: 2.6 + Math.random() * 2.4, vy: 24 + Math.random() * 18, sway: 6 + Math.random() * 8, a: 0.55 + Math.random() * 0.3, sp: 0, par: 0.35 };
    } else if (roll < 0.95) {   // middle
      f = { size: 6 + Math.random() * 5, vy: 48 + Math.random() * 30, sway: 14 + Math.random() * 14, a: 0.7 + Math.random() * 0.25, sp: 1, par: 0.7 };
    } else {                    // near the lens: big, out of focus, fast
      f = { size: 20 + Math.random() * 24, vy: 120 + Math.random() * 70, sway: 30 + Math.random() * 30, a: 0.22 + Math.random() * 0.2, sp: 2, par: 1.4 };
    }
    f.x = Math.random() * (w + 200) - 100;
    f.y = anyY ? Math.random() * h : -f.size - Math.random() * 60;
    f.p1 = Math.random() * 6.283; f.p2 = Math.random() * 6.283;
    f.f1 = 0.25 + Math.random() * 0.5; f.f2 = 0.9 + Math.random() * 1.4;
    f.tw = Math.random() * 6.283;
    return f;
  }
  function size() {
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    w = window.innerWidth; h = window.innerHeight;
    canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    var want = Math.min(460, Math.round(w * h / 3300));
    while (flakes.length < want) flakes.push(flake(true));
    flakes.length = want;
  }
  // slow gusts: a few incommensurate sines read as unsteady wind
  function wind(time) {
    return 10 + Math.sin(time * 0.21) * 16 + Math.sin(time * 0.057 + 1.3) * 22 + Math.sin(time * 0.83) * 4;
  }
  function frame(now) {
    raf = requestAnimationFrame(frame);
    var dt = Math.min(0.05, (now - (last || now)) / 1000);
    last = now; t += dt;
    var wv = wind(t);
    ctx.clearRect(0, 0, w, h);
    for (var i = 0; i < flakes.length; i++) {
      var f = flakes[i];
      // each flake tumbles in its own eddy: a slow swing and a quick flutter
      var dx = Math.sin(t * f.f1 + f.p1) * f.sway + Math.sin(t * f.f2 + f.p2) * f.sway * 0.25;
      f.y += (f.vy + Math.sin(t * f.f2 * 0.7 + f.p1) * f.vy * 0.12) * dt;
      f.x += (wv * f.par + dx) * dt;
      if (f.y > h + f.size) { flakes[i] = flake(false); continue; }
      if (f.x > w + 100) f.x -= w + 200; else if (f.x < -100) f.x += w + 200;
      // a faint glint as the flake turns
      ctx.globalAlpha = f.a * (0.82 + 0.18 * Math.sin(t * 2.3 + f.tw));
      var s = f.size;
      ctx.drawImage(sprites[f.sp], f.x - s / 2, f.y - s / 2, s, s);
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
    setLight = [sprite(0.1, true), sprite(0.45, true), sprite(1, true)];
    setDark = [sprite(0.1, false), sprite(0.45, false), sprite(1, false)];
    pickSprites();
    new MutationObserver(pickSprites).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    if (darkQuery.addEventListener) darkQuery.addEventListener('change', pickSprites);
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
