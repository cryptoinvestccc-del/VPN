/* Аксиомантик: scroll choreography for large screens.

   Phones never download GSAP: this file loads it (from assets/js/vendor)
   only on a wide screen with a mouse and no reduced-motion preference.
   Everything here is scrubbed with the scroll position and animates only
   transform and opacity; leaving the breakpoint reverts it all. */
(() => {
  'use strict';

  const query = '(min-width: 1024px) and (hover: hover) and (prefers-reduced-motion: no-preference)';
  if (!window.matchMedia(query).matches) return;

  const self = document.currentScript || document.querySelector('script[src*="motion.js"]');
  const base = self ? self.src.replace(/motion\.js.*$/, 'vendor/') : 'assets/js/vendor/';
  const load = (name) => new Promise((resolve, reject) => {
    const s = document.createElement('script');
    s.src = base + name;
    s.onload = resolve;
    s.onerror = reject;
    document.head.appendChild(s);
  });

  const run = () => {
    const { gsap, ScrollTrigger } = window;
    if (!gsap || !ScrollTrigger) return;
    gsap.registerPlugin(ScrollTrigger);
    const $ = (sel) => document.querySelector(sel);
    const $$ = (sel) => Array.from(document.querySelectorAll(sel));
    const mm = gsap.matchMedia();

    mm.add(query, () => {
      // The hero sinks under the white sheet as the page scrolls.
      const hero = $('.hero');
      if (hero) {
        const tl = gsap.timeline({
          scrollTrigger: { trigger: hero, start: 'top top', end: 'bottom top', scrub: 0.4 },
        });
        tl.to('.hero__text', { y: 110, opacity: 0.25, ease: 'none' }, 0)
          .to('.hero__art', { y: 70, scale: 0.94, ease: 'none' }, 0)
          .to('.hero__feats', { opacity: 0, ease: 'none' }, 0);
      }

      // The letter leans a little towards the pointer: depth on a flat video.
      const media = $('.hero__media');
      let unbindPointer = null;
      if (hero && media) {
        const toX = gsap.quickTo(media, 'x', { duration: 1.2, ease: 'power3.out' });
        const toY = gsap.quickTo(media, 'y', { duration: 1.2, ease: 'power3.out' });
        const onMove = (e) => {
          const r = hero.getBoundingClientRect();
          toX(((e.clientX - r.left) / r.width - 0.5) * 24);
          toY(((e.clientY - r.top) / r.height - 0.5) * 16);
        };
        const onLeave = () => { toX(0); toY(0); };
        hero.addEventListener('pointermove', onMove, { passive: true });
        hero.addEventListener('pointerleave', onLeave);
        // matchMedia reverts the tweens on leaving the breakpoint; the listeners are ours
        unbindPointer = () => {
          hero.removeEventListener('pointermove', onMove);
          hero.removeEventListener('pointerleave', onLeave);
        };
      }

      // The second half of the statement lights up word by word.
      const statement = $('[data-statement-section]');
      if (statement) {
        statement.classList.add('is-scrub');
        gsap.to(statement.querySelectorAll('.statement__b .sw > span'), {
          opacity: 1,
          ease: 'none',
          stagger: 0.08,
          scrollTrigger: { trigger: statement.querySelector('.statement__text'), start: 'top 78%', end: 'bottom 45%', scrub: 0.5 },
        });
      }

      // Renders drift slightly against the scroll: depth without 3D.
      $$('[data-parallax]').forEach((el) => {
        gsap.fromTo(el, { yPercent: 6 }, {
          yPercent: -6,
          ease: 'none',
          scrollTrigger: { trigger: el, start: 'top bottom', end: 'bottom top', scrub: true },
        });
      });
      $$('.svc__media img, .bc__media img').forEach((img) => {
        gsap.fromTo(img, { yPercent: -3 }, {
          yPercent: 3,
          ease: 'none',
          scrollTrigger: { trigger: img.parentElement, start: 'top bottom', end: 'bottom top', scrub: true },
        });
      });

      // The footer wordmark rises out of the bottom edge.
      const word = $('.ftr__word');
      if (word) {
        gsap.fromTo(word, { yPercent: 35, opacity: 0.2 }, {
          yPercent: 0,
          opacity: 1,
          ease: 'none',
          scrollTrigger: { trigger: word, start: 'top bottom', end: 'bottom bottom', scrub: true },
        });
      }

      return () => {
        if (statement) statement.classList.remove('is-scrub');
        if (unbindPointer) unbindPointer();
      };
    });

    // Pictures that load late change heights: let triggers re-measure.
    window.addEventListener('load', () => ScrollTrigger.refresh(), { once: true });
  };

  load('gsap.min.js').then(() => load('ScrollTrigger.min.js')).then(run).catch(() => { /* motion is optional */ });
})();
