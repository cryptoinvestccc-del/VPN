'use strict';
// Анимация «шариков» в блоке «Как мы решаем задачи» (главная страница).
// Отключается при prefers-reduced-motion.
(() => {
  const board = document.querySelector('[data-flow-board]');
  const overlay = document.querySelector('[data-flow-overlay]');

  if (!board || !overlay ||
      matchMedia('(prefers-reduced-motion: reduce)').matches) return;

  const hub = board.querySelector('[data-flow-hub]');
  const targets = [...board.querySelectorAll('[data-flow-target]')];

  if (!hub || !targets.length) return;

  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');

  svg.setAttribute('aria-hidden', 'true');
  svg.style.cssText = 'display:block;width:100%;height:100%;overflow:visible';
  overlay.replaceChildren(svg);

  const nodes = targets.map(() => {
    const path = document.createElementNS(ns, 'path');
    path.setAttribute('fill', 'none');
    path.setAttribute('stroke', 'none');

    const ball = document.createElementNS(ns, 'circle');
    ball.setAttribute('r', '4.5');
    ball.setAttribute('fill', 'currentColor'); // цвет берётся от .text-brand у оверлея
    ball.setAttribute('opacity', '0');

    svg.append(path, ball);

    return { path, ball, length: 0 };
  });

  const centerOf = (element, base) => {
    const r = element.getBoundingClientRect();
    return {
      x: r.left - base.left + r.width / 2,
      y: r.top - base.top + r.height / 2
    };
  };

  function updatePaths() {
    const r = board.getBoundingClientRect();

    svg.setAttribute(
      'viewBox',
      `0 0 ${Math.max(1, r.width)} ${Math.max(1, r.height)}`
    );

    const from = centerOf(hub, r);

    targets.forEach((target, i) => {
      const to = centerOf(target, r);
      const dx = to.x - from.x;

      const bend = Math.abs(dx) > 32
        ? Math.sign(dx) * Math.min(28, Math.abs(dx) * 0.12)
        : 0;

      const cx = (from.x + to.x) / 2 + bend;
      const cy = (from.y + to.y) / 2;

      const d = `M ${from.x} ${from.y} Q ${cx} ${cy} ${to.x} ${to.y}`;

      nodes[i].path.setAttribute('d', d);
      nodes[i].length = nodes[i].path.getTotalLength();
    });
  }

  updatePaths();

  new ResizeObserver(updatePaths).observe(board);
  window.addEventListener('resize', updatePaths, { passive: true });

  const cycle = 3300;
  const travel = 1750;

  let frameId;
  let active = true;
  let origin;

  const observer = new IntersectionObserver(([entry]) => {
    active = entry.isIntersecting;

    if (active) {
      origin = undefined;
      frameId = requestAnimationFrame(animate);
    } else {
      cancelAnimationFrame(frameId);
    }
  }, { threshold: 0 });

  observer.observe(board);

  function animate(now) {
    if (!active) return;

    if (origin === undefined) origin = now;

    nodes.forEach(({ path, ball, length }, i) => {
      const elapsed =
        ((now - origin - i * 320) % cycle + cycle) % cycle;

      if (elapsed > travel || !length) {
        ball.setAttribute('opacity', '0');
        return;
      }

      const progress = elapsed / travel;
      const p = path.getPointAtLength(progress * length);

      ball.setAttribute('cx', p.x.toFixed(2));
      ball.setAttribute('cy', p.y.toFixed(2));

      ball.setAttribute(
        'opacity',
        String(Math.min(1, progress * 14, (1 - progress) * 14))
      );
    });

    frameId = requestAnimationFrame(animate);
  }
})();
