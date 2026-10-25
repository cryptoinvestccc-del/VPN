'use strict';
/* Axiomantic: menu, the project dialog, lead forms, the cookie notice and
   the counter. Endpoints and every message come from the page (data-* on
   <body>, written by build.py from the panel's content). */
document.addEventListener('DOMContentLoaded', () => {
  const body = document.body;
  let MSG = {};
  try { MSG = JSON.parse(body.dataset.msg || '{}'); } catch { MSG = {}; }

  /* -------------------------------------------------------------- menu */
  const menuButton = document.getElementById('menu-toggle');
  const mobileMenu = document.getElementById('mobile-menu');
  const setMenu = open => {
    mobileMenu.hidden = !open;
    menuButton.setAttribute('aria-expanded', String(open));
    menuButton.setAttribute('aria-label', open ? menuButton.dataset.labelClose : menuButton.dataset.labelOpen);
    menuButton.querySelector('i').className = open ? 'fa-solid fa-xmark' : 'fa-solid fa-bars';
  };
  if (menuButton && mobileMenu) {
    menuButton.addEventListener('click', () => setMenu(mobileMenu.hidden));
    mobileMenu.addEventListener('click', event => { if (event.target.closest('a')) setMenu(false); });
    document.addEventListener('keydown', event => {
      if (event.key === 'Escape' && !mobileMenu.hidden) { setMenu(false); menuButton.focus(); }
    });
    matchMedia('(min-width: 1024px)').addEventListener('change', event => { if (event.matches) setMenu(false); });
  }

  const year = document.getElementById('year');
  if (year) year.textContent = String(new Date().getFullYear());

  /* --------------------------------------------- cookie notice, counter */
  // With a Yandex Metrika counter set in the panel the page asks first; the
  // counter's script is fetched only after «Принять».
  const metrika = body.dataset.metrika;
  const banner = document.getElementById('cookie-banner');
  const loadMetrika = () => {
    window.ym = window.ym || function () { (window.ym.a = window.ym.a || []).push(arguments); };
    window.ym.l = Date.now();
    const s = document.createElement('script');
    s.async = true;
    s.src = 'https://mc.yandex.ru/metrika/tag.js';
    document.head.appendChild(s);
    window.ym(Number(metrika), 'init', { clickmap: true, trackLinks: true, accurateTrackBounce: true, webvisor: 'webvisor' in body.dataset });
  };
  if (metrika && /^\d{4,12}$/.test(metrika) && banner) {
    let choice = '';
    try { choice = localStorage.getItem('axiomantic_cookies') || ''; } catch { /* storage off */ }
    if (choice === 'yes') loadMetrika();
    else if (choice !== 'no') {
      setTimeout(() => {
        banner.hidden = false;
        requestAnimationFrame(() => requestAnimationFrame(() => {
          banner.classList.remove('translate-y-24', 'opacity-0');
          banner.classList.add('translate-y-0', 'opacity-100');
        }));
      }, 1000);
    }
    const decide = yes => {
      try { localStorage.setItem('axiomantic_cookies', yes ? 'yes' : 'no'); } catch { /* storage off */ }
      banner.hidden = true;
      if (yes) loadMetrika();
    };
    document.getElementById('accept-cookies')?.addEventListener('click', () => decide(true));
    document.getElementById('decline-cookies')?.addEventListener('click', () => decide(false));
  }

  /* ------------------------------------------------------------ dialog */
  const modal = document.getElementById('project-modal');
  let modalTrigger;
  let previousOverflow;
  if (modal && typeof modal.showModal === 'function') {
    document.querySelectorAll('[data-project-modal]').forEach(trigger => {
      trigger.addEventListener('click', () => {
        modalTrigger = trigger;
        if (mobileMenu && !mobileMenu.hidden) setMenu(false);
        // a tariff's button says which tariff the lead is about
        const interest = modal.querySelector('[name="interest"]');
        if (interest) interest.value = trigger.dataset.interest || '';
        previousOverflow = document.documentElement.style.overflow;
        modal.showModal();
        document.documentElement.style.overflow = 'hidden';
        modal.querySelector('[name="name"]').focus({ preventScroll: true });
      });
    });
    modal.querySelector('[data-close-modal]').addEventListener('click', () => modal.close());
    modal.addEventListener('click', event => {
      const rect = modal.getBoundingClientRect();
      if (event.target === modal && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom)) modal.close();
    });
    modal.addEventListener('close', () => {
      document.documentElement.style.overflow = previousOverflow;
      const target = modalTrigger?.closest('#mobile-menu') ? menuButton : modalTrigger;
      target?.focus({ preventScroll: true });
    });
  }

  /* ------------------------------------------------------------- forms */
  // The same rules as the lead server; the server has the last word.
  const digits = v => v.replace(/\D/g, '');
  const isPhone = v => {
    if (v.length > 24 || !/^[+\d ()-]+$/.test(v)) return false;
    const n = digits(v).length;
    if (/^\s*(\+\s*7|[78])/.test(v)) return n === 11;
    return n >= 10 && n <= 15;
  };
  const isTelegram = v => /^@?[A-Za-z0-9_]{5,32}$/.test(v);
  const isEmail = v => /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(v);
  const endpoint = body.dataset.endpoint;

  document.querySelectorAll('#contact-form, #project-modal-form').forEach(form => {
    const status = form.querySelector('[role="status"]');
    const button = form.querySelector('[type="submit"]');
    button.disabled = false;
    const say = (text, ok) => {
      status.textContent = text || '';
      status.classList.toggle('text-emerald-600', !!ok);
    };
    form.addEventListener('input', () => say(''));
    form.addEventListener('submit', async event => {
      event.preventDefault();
      const data = Object.fromEntries(new FormData(form));
      const name = String(data.name || '').trim();
      const contact = String(data.contact || '').trim();
      if (name.length < 2) { say(MSG.name); form.querySelector('[name="name"]').focus(); return; }
      if (!(isPhone(contact) || isTelegram(contact) || isEmail(contact))) { say(MSG.contact); form.querySelector('[name="contact"]').focus(); return; }
      if (!form.querySelector('[name="consent"]').checked) { say(MSG.consent); form.querySelector('[name="consent"]').focus(); return; }
      if (endpoint === undefined) { say(MSG.off); return; }
      if (endpoint === '') { say(MSG.demo, true); return; }   // a preview: nothing is sent
      button.disabled = true;
      say(MSG.sending);
      try {
        const response = await fetch(endpoint, {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin',
          body: JSON.stringify({
            source: form.dataset.source, mode: 'brief', name, contact,
            message: String(data.message || '').trim(), interest: String(data.interest || ''),
            consent: true, website: String(data.website || '')
          }),
          signal: AbortSignal.timeout(15000)
        });
        if (response.status === 429) { say(MSG.too_many); return; }
        if (!response.ok) throw new Error(String(response.status));
        say(MSG.done, true);
        form.reset();
      } catch {
        say(MSG.failed);
      } finally { button.disabled = false; }
    });
  });
});
