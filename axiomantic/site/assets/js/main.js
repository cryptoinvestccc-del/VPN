/* Аксиомантик: everything the pages do on every device — header, menu,
   scroll reveals, the hero video, the approach steps, counters, the order
   dialog, lead forms, filters and the click-to-load map. No dependencies,
   no trackers. Desktop-only scroll choreography lives in motion.js.

   Every order button is a link to the contacts page, so the site still
   takes orders with this script blocked; the script only upgrades that
   link into the dialog. */
(() => {
  'use strict';

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  const root = document.documentElement;
  const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const hasIO = 'IntersectionObserver' in window;

  /* ---------------------------------------------------------- header */

  // Transparent over the dark hero, a white bar once the page moves; it
  // slides away while reading down and comes back on the way up.
  const hdr = $('[data-hdr]');
  let lastY = window.scrollY;
  let ticking = false;
  const onScroll = () => {
    ticking = false;
    const y = window.scrollY;
    hdr.classList.toggle('is-solid', y > 24);
    if (!document.body.classList.contains('menu-open')) {
      if (y > 640 && y > lastY + 6) hdr.classList.add('is-hidden');
      else if (y < lastY - 6 || y <= 640) hdr.classList.remove('is-hidden');
    }
    lastY = y;
  };
  if (hdr) {
    onScroll();
    window.addEventListener('scroll', () => {
      if (!ticking) { ticking = true; window.requestAnimationFrame(onScroll); }
    }, { passive: true });
    hdr.addEventListener('focusin', () => hdr.classList.remove('is-hidden'));
  }

  const burger = $('[data-burger]');
  const menu = $('#menu');
  const setMenu = (open) => {
    if (!burger || !menu) return;
    burger.setAttribute('aria-expanded', String(open));
    menu.hidden = !open;
    document.body.classList.toggle('menu-open', open);
  };
  if (burger && menu) {
    burger.addEventListener('click', () => setMenu(menu.hidden));
    menu.addEventListener('click', (e) => { if (e.target.closest('a')) setMenu(false); });
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !menu.hidden) { setMenu(false); burger.focus(); }
    });
    window.matchMedia('(min-width: 1024px)').addEventListener('change', (e) => { if (e.matches) setMenu(false); });
  }

  /* --------------------------------------------------------- reveals */

  // Things fade up once as they enter. They are hidden only from here on
  // (.motion-ready), so without this script everything is simply visible.
  if (!reduced && hasIO) {
    $$('[data-split]').forEach((h) => {
      $$('.w > span', h).forEach((w, i) => { w.style.transitionDelay = `${Math.min(i * 0.045, 0.4)}s`; });
    });
    $$('[data-reveal]').forEach((el) => {
      const sibs = el.parentElement ? $$(':scope > [data-reveal]', el.parentElement) : [];
      const i = sibs.indexOf(el);
      if (i > 0) el.style.transitionDelay = `${Math.min(i * 0.08, 0.32)}s`;
    });
    root.classList.add('motion-ready');
    const io = new IntersectionObserver((entries) => {
      entries.forEach((en) => {
        if (en.isIntersecting) { en.target.classList.add('is-in'); io.unobserve(en.target); }
      });
    }, { rootMargin: '0px 0px -12% 0px' });
    $$('[data-reveal], [data-split]').forEach((el) => io.observe(el));
    // returning from the back/forward cache: show everything as it was left
    window.addEventListener('pageshow', (e) => {
      if (e.persisted) $$('[data-reveal], [data-split]').forEach((el) => el.classList.add('is-in'));
    });
  }

  /* ------------------------------------------------------ hero video */

  // The poster is the picture; the loop is a bonus. It is fetched after the
  // page has loaded, never for reduced motion or Save-Data, plays only
  // while on screen and can be paused (WCAG 2.2.2).
  const media = $('[data-hero-media]');
  const video = media && $('video', media);
  const conn = navigator.connection || {};
  if (video && !reduced && !conn.saveData && !/2g/.test(conn.effectiveType || '') && hasIO) {
    const toggle = $('[data-video-toggle]');
    const small = window.innerWidth < 1024;
    let inView = true;
    let userPaused = false;
    const play = () => { if (!userPaused && inView && !document.hidden) video.play().catch(() => {}); };
    video.addEventListener('playing', () => {
      media.classList.add('is-playing');
      if (toggle) toggle.hidden = false;
    }, { once: true });
    const start = () => {
      [['webm', 'video/webm'], ['mp4', 'video/mp4']].forEach(([key, type]) => {
        const src = video.dataset[small ? `${key}Sm` : key];
        if (!src) return;
        const s = document.createElement('source');
        s.src = src;
        s.type = type;
        video.appendChild(s);
      });
      video.preload = 'auto';
      video.load();
      new IntersectionObserver(([en]) => {
        inView = en.isIntersecting;
        if (inView) play(); else video.pause();
      }).observe(media);
      play();
    };
    document.addEventListener('visibilitychange', () => { if (document.hidden) video.pause(); else play(); });
    if (toggle) {
      toggle.addEventListener('click', () => {
        userPaused = !userPaused;
        toggle.setAttribute('aria-pressed', String(userPaused));
        toggle.setAttribute('aria-label', userPaused ? 'Запустить анимацию' : 'Остановить анимацию');
        if (userPaused) video.pause(); else play();
      });
    }
    const later = () => window.setTimeout(start, 400);
    if (document.readyState === 'complete') later(); else window.addEventListener('load', later, { once: true });
  }

  /* ------------------------------------------------- approach steps */

  const steps = $$('[data-step]');
  const stepNow = $('[data-step-now]');
  if (steps.length && hasIO) {
    steps[0].parentElement.classList.add('js-steps');
    steps[0].classList.add('is-active');
    const io = new IntersectionObserver((entries) => {
      entries.forEach((en) => {
        if (!en.isIntersecting) return;
        steps.forEach((s) => s.classList.toggle('is-active', s === en.target));
        if (stepNow) stepNow.textContent = String(steps.indexOf(en.target) + 1).padStart(2, '0');
      });
    }, { rootMargin: '-45% 0px -45% 0px' });
    steps.forEach((s) => io.observe(s));
  }

  /* -------------------------------------------------------- counters */

  // "120+", "−40%", "×2,4", "0,9 с": the number counts up, the rest stays.
  if (!reduced && hasIO) {
    const io = new IntersectionObserver((entries) => {
      entries.forEach((en) => {
        if (!en.isIntersecting) return;
        io.unobserve(en.target);
        const el = en.target;
        const text = el.textContent;
        const m = text.match(/^(\D*?)(\d+(?:[.,]\d+)?)(.*)$/);
        if (!m) return;
        const target = parseFloat(m[2].replace(',', '.'));
        const decimals = (m[2].split(/[.,]/)[1] || '').length;
        const comma = m[2].includes(',');
        const t0 = performance.now();
        const dur = 1200;
        const tick = (now) => {
          const k = Math.min((now - t0) / dur, 1);
          const eased = 1 - Math.pow(1 - k, 3);
          let v = (target * eased).toFixed(decimals);
          if (comma) v = v.replace('.', ',');
          el.textContent = k < 1 ? m[1] + v + m[3] : text;
          if (k < 1) window.requestAnimationFrame(tick);
        };
        window.requestAnimationFrame(tick);
      });
    }, { rootMargin: '0px 0px -15% 0px' });
    $$('[data-count]').forEach((el) => io.observe(el));
  }

  /* ------------------------------------------------------ lead forms */

  // The same rules are enforced again by the server; these only save a
  // round trip and explain what is wrong next to the field.
  const RE = {
    email: /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/,
    tg: /^@?[A-Za-z0-9_]{5,32}$/,
  };
  const digits = (v) => v.replace(/\D/g, '');
  const isPhone = (v) => /^[+\d\s()-]+$/.test(v) && digits(v).length >= 10 && digits(v).length <= 15;

  const WHEN = { hour: 'в течение часа', today: 'сегодня', tomorrow: 'завтра' };
  const WHERE = { telegram: 'в Telegram', whatsapp: 'в WhatsApp', email: 'на почту' };

  const formatPhone = (value) => {
    let d = digits(value);
    if (!d) return '';
    if (d[0] === '8') d = '7' + d.slice(1);
    if (d[0] !== '7') return value; // not a Russian number: leave as typed
    d = d.slice(0, 11);
    let out = '+7';
    if (d.length > 1) out += ' (' + d.slice(1, 4);
    if (d.length >= 4) out += ')';
    if (d.length > 4) out += ' ' + d.slice(4, 7);
    if (d.length > 7) out += '-' + d.slice(7, 9);
    if (d.length > 9) out += '-' + d.slice(9, 11);
    return out;
  };

  const fieldOf = (input) => input.closest('.field') || input.closest('.consent');
  const errOf = (input) => {
    const id = input.getAttribute('aria-describedby');
    return id ? document.getElementById(id) : null;
  };
  const setError = (input, msg) => {
    const box = fieldOf(input);
    const err = errOf(input);
    if (box) box.classList.toggle('is-invalid', Boolean(msg));
    input.setAttribute('aria-invalid', msg ? 'true' : 'false');
    if (err) err.textContent = msg || '';
  };
  const clearErrors = (form) => {
    $$('input,textarea', form).forEach((i) => { if (i.type !== 'radio') setError(i, ''); });
    const status = $('.lead__status', form);
    if (status) status.textContent = '';
  };
  const checked = (form, name) => {
    const el = $(`input[name="${name}"]:checked`, form);
    return el && !el.disabled ? el.value : '';
  };
  const setRadio = (form, name, value) => {
    const el = $(`input[name="${name}"][value="${value}"]`, form);
    if (el) el.checked = true;
  };
  const mode = (form) => checked(form, 'mode') || ($('input[name="mode"]', form) || {}).value;

  const syncMode = (form) => {
    const m = mode(form);
    $$('[data-mode-fields]', form).forEach((group) => {
      const on = group.dataset.modeFields === m;
      group.hidden = !on;
      group.disabled = !on;
    });
    const label = $('[data-submit-label]', form);
    if (label && form.dataset.source === 'modal') {
      label.textContent = m === 'write' ? 'Отправить сообщение' : 'Жду звонка';
    }
    clearErrors(form);
  };

  const syncChannel = (form) => {
    const ch = $('input[name="channel"]:checked', form);
    const input = $('[data-contact]', form);
    if (!ch || !input) return;
    const label = $(`label[for="${input.id}"]`, form);
    if (label) label.textContent = ch.dataset.label;
    input.placeholder = ch.dataset.placeholder;
    input.type = ch.dataset.type;
    input.autocomplete = ch.dataset.autocomplete;
    input.inputMode = { tel: 'tel', email: 'email' }[ch.dataset.type] || 'text';
    input.toggleAttribute('data-phone', ch.dataset.type === 'tel');
    setError(input, '');
  };

  const validate = (form) => {
    const m = mode(form);
    const problems = [];
    const need = (name, test, msg) => {
      const input = $(`[name="${name}"]`, form);
      if (!input || input.disabled || input.closest('[disabled]')) return;
      const ok = test(input.value.trim());
      setError(input, ok ? '' : msg);
      if (!ok) problems.push(input);
    };
    need('name', (v) => v.length >= 2 && v.length <= 80, 'Напишите, как к вам обращаться');
    if (m === 'call') need('phone', isPhone, 'Укажите телефон полностью, например +7 900 000-00-00');
    if (m === 'write') {
      const ch = checked(form, 'channel');
      const rules = {
        telegram: [(v) => RE.tg.test(v) || isPhone(v), 'Укажите ник, например @username, или номер телефона'],
        whatsapp: [isPhone, 'Укажите номер WhatsApp полностью'],
        email: [(v) => RE.email.test(v), 'Укажите email, например name@company.ru'],
      }[ch];
      if (rules) need('contact', rules[0], rules[1]);
    }
    if (m === 'form') {
      need('contact', (v) => RE.email.test(v) || isPhone(v), 'Укажите email или телефон');
      need('message', (v) => v.length >= 10, 'Опишите задачу хотя бы в паре предложений');
    }
    const consent = $('input[name="consent"]', form);
    if (consent) {
      setError(consent, consent.checked ? '' : 'Без согласия мы не сможем ответить на заявку');
      if (!consent.checked) problems.push(consent);
    }
    return problems[0] || null;
  };

  const doneText = (payload, demo) => {
    const name = payload.name;
    let text;
    if (payload.mode === 'call') text = `${name}, перезвоним ${WHEN[payload.when] || 'в ближайшее время'} — в рабочее время студии.`;
    else if (payload.mode === 'write') text = `${name}, ответим ${WHERE[payload.channel] || ''} в течение рабочего дня.`;
    else text = `${name}, маркетолог изучит задачу и свяжется с вами в течение рабочего дня.`;
    return demo ? `${text} Демо-режим: заявка никуда не отправлена.` : text;
  };

  const contactEmail = () => {
    const a = $('a[href^="mailto:"]');
    return a ? a.textContent.trim() : '';
  };

  const showDone = (form, text) => {
    $('[data-done-text]', form).textContent = text;
    form.classList.add('is-done');
    const done = $('.lead__done', form);
    done.hidden = false;
    done.focus();
  };

  const resetForm = (form) => {
    if (!form.classList.contains('is-done')) return;
    form.classList.remove('is-done');
    $('.lead__done', form).hidden = true;
    const keepMode = mode(form);
    form.reset();
    if (form.dataset.source === 'modal') setRadio(form, 'mode', keepMode);
    syncMode(form);
    syncChannel(form);
  };

  const submit = async (form) => {
    const bad = validate(form);
    if (bad) { bad.focus(); return; }
    const payload = { source: form.dataset.source };
    new FormData(form).forEach((v, k) => { payload[k] = String(v).trim(); });
    payload.consent = Boolean($('input[name="consent"]', form).checked);

    const btn = $('[data-submit]', form);
    const status = $('.lead__status', form);
    btn.setAttribute('aria-busy', 'true');
    status.textContent = '';
    const endpoint = document.body.dataset.endpoint || '';
    try {
      if (!endpoint) {
        await new Promise((r) => setTimeout(r, 600));
        showDone(form, doneText(payload, true));
        return;
      }
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
        credentials: 'same-origin',
      });
      if (res.ok) { showDone(form, doneText(payload, false)); return; }
      let body = {};
      try { body = await res.json(); } catch (_) { /* not JSON */ }
      if (res.status === 429) {
        status.textContent = 'Слишком много заявок подряд. Попробуйте через несколько минут или позвоните нам.';
      } else if (res.status === 400 && body.field) {
        const input = $(`[name="${body.field}"]`, form);
        if (input) { setError(input, 'Проверьте это поле'); input.focus(); }
        status.textContent = 'Проверьте выделенное поле.';
      } else {
        throw new Error(String(res.status));
      }
    } catch (_) {
      const mail = contactEmail();
      status.textContent = 'Не получилось отправить заявку. Попробуйте ещё раз' + (mail ? ` или напишите на ${mail}.` : '.');
    } finally {
      btn.removeAttribute('aria-busy');
    }
  };

  $$('[data-lead]').forEach((form) => {
    form.addEventListener('submit', (e) => { e.preventDefault(); submit(form); });
    form.addEventListener('change', (e) => {
      if (e.target.name === 'mode') syncMode(form);
      if (e.target.name === 'channel') syncChannel(form);
      if (e.target.name === 'consent' && e.target.checked) setError(e.target, '');
    });
    form.addEventListener('input', (e) => {
      const t = e.target;
      if (t.hasAttribute('data-phone') && e.inputType !== 'deleteContentBackward') t.value = formatPhone(t.value);
      if (t.getAttribute('aria-invalid') === 'true') setError(t, '');
    });
    form.addEventListener('click', (e) => {
      if (e.target.closest('[data-again]')) resetForm(form);
    });
    syncMode(form);
    syncChannel(form);
  });

  /* ---------------------------------------------------- order dialog */

  const dialog = $('#order');
  let opener = null;
  let downOnBackdrop = false;

  const openOrder = (trigger) => {
    if (!dialog || typeof dialog.showModal !== 'function') return false;
    setMenu(false);
    const form = $('form', dialog);
    resetForm(form);
    if (trigger.dataset.mode) setRadio(form, 'mode', trigger.dataset.mode);
    if (trigger.dataset.interest) setRadio(form, 'interest', trigger.dataset.interest);
    syncMode(form);
    dialog.showModal();
    document.body.classList.add('modal-open');
    // Keyboard and mouse users land in the first field; on touch screens
    // that would throw the keyboard over the choice they have to make first.
    if (window.matchMedia('(pointer: fine)').matches) $('input[name="name"]', form).focus();
    opener = trigger;
    return true;
  };

  document.addEventListener('click', (e) => {
    const t = e.target.closest('[data-order]');
    if (t && openOrder(t)) e.preventDefault();
  });

  if (dialog) {
    dialog.addEventListener('pointerdown', (e) => { downOnBackdrop = e.target === dialog; });
    dialog.addEventListener('click', (e) => {
      if (e.target === dialog && downOnBackdrop) dialog.close();
      if (e.target.closest('[data-close]')) dialog.close();
    });
    dialog.addEventListener('close', () => {
      document.body.classList.remove('modal-open');
      if (opener) opener.focus();
      opener = null;
    });
  }

  /* --------------------------------------------------------- filters */

  $$('[data-filters]').forEach((bar) => {
    const scope = bar.parentElement;
    const grid = $('[data-filter-grid]', scope);
    const empty = $('[data-empty]', scope);
    const apply = (key, remember) => {
      let shown = 0;
      $$('[data-filter]', bar).forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.filter === key)));
      $$('[data-cat]', grid).forEach((card) => {
        const on = key === 'all' || card.dataset.cat === key;
        card.hidden = !on;
        if (on) shown += 1;
      });
      if (empty) empty.hidden = shown > 0;
      if (remember) {
        const url = new URL(window.location.href);
        if (key === 'all') url.searchParams.delete('type'); else url.searchParams.set('type', key);
        window.history.replaceState(null, '', url);
      }
    };
    bar.addEventListener('click', (e) => {
      const b = e.target.closest('[data-filter]');
      if (b) apply(b.dataset.filter, true);
    });
    const initial = new URL(window.location.href).searchParams.get('type');
    if (initial && $$('[data-filter]', bar).some((b) => b.dataset.filter === initial)) apply(initial, false);
  });

  /* ------------------------------------------------------- demo note */

  const demo = $('[data-demo]');
  if (demo) {
    try { if (sessionStorage.getItem('demo-hidden')) demo.hidden = true; } catch (_) { /* storage off */ }
    $('[data-demo-close]', demo).addEventListener('click', () => {
      demo.hidden = true;
      try { sessionStorage.setItem('demo-hidden', '1'); } catch (_) { /* storage off */ }
    });
  }

  /* ------------------------------------------------------------- map */

  // The map is Yandex's page in a frame; it is fetched only on request so
  // that opening the contacts page sends nothing to a third party.
  $$('[data-map]').forEach((box) => {
    const btn = $('[data-map-load]', box);
    if (!btn) return;
    btn.addEventListener('click', () => {
      const frame = document.createElement('iframe');
      frame.src = box.dataset.map;
      frame.title = 'Карта: как нас найти';
      frame.referrerPolicy = 'strict-origin-when-cross-origin';
      frame.allowFullscreen = true;
      box.prepend(frame);
      box.classList.add('is-loaded');
    });
  });
})();
