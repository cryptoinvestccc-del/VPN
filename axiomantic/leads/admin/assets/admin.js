/* Axiomantic admin — leads panel. Vanilla JS, no build step.
 *
 * Security notes (see repo CLAUDE.md and spec section 4):
 *  - Every value from the API is attacker-controlled (visitors type it).
 *    The DOM is built only with createElement / textContent / setAttribute
 *    (the h() helper); no HTML string is ever parsed anywhere in this file.
 *  - Links built from contact data go through the whitelists in contactLinks().
 *  - Browser storage keeps only the theme and the sidebar state.
 */
'use strict';

(function () {
  /* ------------------------------------------------------------------ storage & theme (runs before first paint) */

  var THEME_KEY = 'axm-admin-theme';
  var SIDE_KEY = 'axm-admin-sidebar';

  function storeGet(key) {
    try { return window.localStorage.getItem(key); } catch (e) { return null; }
  }
  function storeSet(key, value) {
    try { window.localStorage.setItem(key, value); } catch (e) { /* private mode: ignore */ }
  }

  var saved = storeGet(THEME_KEY);
  if (saved === 'light' || saved === 'dark') document.documentElement.setAttribute('data-theme', saved);

  document.addEventListener('DOMContentLoaded', boot);

  /* ------------------------------------------------------------------ constants */

  var API = '/api/admin';
  var ICONS = 'assets/icons.svg';
  var SVGNS = 'http://www.w3.org/2000/svg';
  var PAGE_SIZE = 20;
  var DEFAULT_BRAND = { name: 'Аксиомантик', logo: 'А' };
  var HELP_MAIL = 'hello@axiomantic.example';

  var STATUS = { new: 'Новая', contacted: 'Связались', proposal: 'КП отправлено', won: 'Договор', lost: 'Отказ', spam: 'Спам' };
  var STATUS_KEYS = ['new', 'contacted', 'proposal', 'won', 'lost', 'spam'];
  var OPEN = { new: 1, contacted: 1, proposal: 1 };
  var WHEN = { hour: 'в течение часа', today: 'сегодня', tomorrow: 'завтра' };
  var CHANNEL = { telegram: 'Telegram', whatsapp: 'WhatsApp', email: 'Email' };
  var INTEREST = { landing: 'Лендинг', 'multi-base': 'Многостаночник · Базовый', 'multi-custom': 'Многостаночник · Под вас', unknown: 'пока не знает' };
  var BUDGET = { lt100: 'до 100 000 ₽', '100-300': '100–300 000 ₽', gt300: 'от 300 000 ₽', unknown: 'пока не знает' };
  var MODE = { call: 'Звонок', write: 'Сообщение', form: 'Форма' };
  var MODE_ICON = { call: 'phone', write: 'message', form: 'form' };
  var SOURCE = { modal: 'Окно заявки', contacts: 'Страница контактов' };
  var ROLE = { owner: 'Владелец', manager: 'Менеджер', viewer: 'Наблюдатель' };
  var KIND = { phone: 'Телефон', telegram: 'Telegram', whatsapp: 'WhatsApp', email: 'Email' };
  var KIND_ICON = { phone: 'phone', telegram: 'send', whatsapp: 'chat', email: 'mail' };
  var SORTS = { new: 'Сначала новые', old: 'Сначала старые', updated: 'По изменению' };

  function has(map, key) { return typeof key === 'string' && Object.prototype.hasOwnProperty.call(map, key); }
  function lab(map, key, dash) { return has(map, key) ? map[key] : (dash === undefined ? '—' : dash); }

  /* ------------------------------------------------------------------ state */

  var S = {
    user: null, ws: null, csrf: '', counts: { new: 0 },
    team: null, route: null, view: null, nav: 0,
    leads: null,        // { params, data }
    drawer: null,       // { id, el, overlay, data, opener }
    layers: [],         // open modal layers, topmost last
    cleanups: [],       // per-view teardown callbacks
    poll: 0
  };
  var E = {};           // shell element refs

  var reduceMotion = window.matchMedia ? window.matchMedia('(prefers-reduced-motion: reduce)') : { matches: false };
  var narrowMQ = window.matchMedia ? window.matchMedia('(max-width: 1023px)') : { matches: false, addEventListener: function () {} };
  var isMac = /Mac|iPhone|iPad/.test((navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '');

  /* ------------------------------------------------------------------ DOM helpers */

  function h(tag, attrs) {
    var el = document.createElement(tag);
    setAttrs(el, attrs);
    for (var i = 2; i < arguments.length; i++) add(el, arguments[i]);
    return el;
  }
  function svg(tag, attrs) {
    var el = document.createElementNS(SVGNS, tag);
    setAttrs(el, attrs);
    for (var i = 2; i < arguments.length; i++) add(el, arguments[i]);
    return el;
  }
  function setAttrs(el, attrs) {
    if (!attrs) return;
    Object.keys(attrs).forEach(function (k) {
      var v = attrs[k];
      if (v === null || v === undefined || v === false) return;
      if (k === 'text') el.textContent = String(v);
      else if (k === 'class') el.setAttribute('class', v);
      else if (k === 'vars') Object.keys(v).forEach(function (p) { el.style.setProperty(p, v[p]); });
      else if (k === 'data') Object.keys(v).forEach(function (p) { el.dataset[p] = v[p]; });
      else if (k.slice(0, 2) === 'on' && typeof v === 'function') el.addEventListener(k.slice(2), v);
      else if (v === true) el.setAttribute(k, '');
      else el.setAttribute(k, String(v));
    });
  }
  function add(el, kid) {
    if (kid === null || kid === undefined || kid === false) return;
    if (Array.isArray(kid)) { kid.forEach(function (k) { add(el, k); }); return; }
    el.appendChild(typeof kid === 'object' ? kid : document.createTextNode(String(kid)));
  }
  function clear(el) { while (el && el.firstChild) el.removeChild(el.firstChild); }
  function fill(el) { clear(el); for (var i = 1; i < arguments.length; i++) add(el, arguments[i]); return el; }
  function icon(name, cls) {
    var use = svg('use', { href: ICONS + '#' + name });
    return svg('svg', { class: 'ico' + (cls ? ' ' + cls : ''), 'aria-hidden': 'true', focusable: 'false' }, use);
  }
  function b(text) { return h('b', { text: text }); }
  function $(sel, root) { return (root || document).querySelector(sel); }
  function $$(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }
  var uid = 0;
  function nextId(prefix) { uid += 1; return prefix + '-' + uid; }
  function debounce(fn, ms) {
    var t = 0;
    var d = function () { var a = arguments, self = this; clearTimeout(t); t = setTimeout(function () { fn.apply(self, a); }, ms); };
    d.cancel = function () { clearTimeout(t); };
    return d;
  }
  function focusables(root) {
    return $$('a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])', root)
      .filter(function (el) { return el.offsetParent !== null || el === document.activeElement; });
  }
  function trapTab(e, root) {
    var f = focusables(root);
    if (!f.length) { e.preventDefault(); return; }
    var first = f[0], last = f[f.length - 1];
    if (e.shiftKey && (document.activeElement === first || !root.contains(document.activeElement))) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && (document.activeElement === last || !root.contains(document.activeElement))) { e.preventDefault(); first.focus(); }
  }

  /* ------------------------------------------------------------------ text helpers */

  var NB = '\u00A0';  // non-breaking space: numbers and their units, «№ 1», « — »
  var PR = new Intl.PluralRules('ru');
  function plural(n, forms) {
    var r = PR.select(n);
    return r === 'one' ? forms[0] : r === 'many' ? forms[2] : forms[1];
  }
  var NF = new Intl.NumberFormat('ru-RU');
  function num(n) { return NF.format(n || 0); }
  function pct1(x) { return new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 }).format(x); }
  function leadsWord(n) { return plural(n, ['заявка', 'заявки', 'заявок']); }

  var MALE_A = { 'Илья': 1, 'Никита': 1, 'Кузьма': 1, 'Фома': 1, 'Лука': 1, 'Савва': 1, 'Данила': 1, 'Гаврила': 1, 'Миша': 1, 'Дима': 1, 'Ваня': 1, 'Петя': 1, 'Коля': 1, 'Вова': 1, 'Лёша': 1, 'Паша': 1, 'Гоша': 1, 'Слава': 1, 'Серёжа': 1, 'Толя': 1, 'Федя': 1, 'Юра': 1 };
  var UNISEX = { 'Саша': 1, 'Женя': 1, 'Валя': 1, 'Слава': 1 };
  var FEMALE_SOFT = { 'Любовь': 1, 'Нинель': 1, 'Адель': 1 };
  // true = female, false = male, null = unknown
  function female(name) {
    var first = String(name || '').trim().split(/\s+/)[0] || '';
    if (!/^[А-ЯЁ][а-яё]+$/.test(first)) return null;
    if (has(UNISEX, first)) return null;
    if (has(MALE_A, first)) return false;
    if (has(FEMALE_SOFT, first)) return true;
    if (/[ая]$/.test(first)) return true;
    if (/[бвгджзйклмнпрстфхцчшщь]$/.test(first)) return false;
    return null;
  }
  function verb(name, m, f) {
    var g = female(name);
    return g === true ? f : g === false ? m : m + '(а)';
  }
  // Genitive of a single Russian first name; null when we would rather not guess.
  function genitive(name) {
    var t = String(name || '').trim();
    if (!/^[А-ЯЁ][а-яё]{1,30}$/.test(t)) return null;
    if (/ия$/.test(t)) return t.slice(0, -1) + 'и';
    if (/ья$/.test(t)) return t.slice(0, -1) + 'и';
    if (/[гкхжчшщ]а$/.test(t)) return t.slice(0, -1) + 'и';
    if (/а$/.test(t)) return t.slice(0, -1) + 'ы';
    if (/я$/.test(t)) return t.slice(0, -1) + 'и';
    if (/й$/.test(t)) return t.slice(0, -1) + 'я';
    if (/ь$/.test(t)) return null;
    if (/[бвгджзклмнпрстфхцчшщ]$/.test(t)) return t + 'а';
    return t;
  }
  function firstName(name) { return String(name || '').trim().split(/\s+/)[0] || ''; }
  function avatarTint(key) {
    var s = String(key || ''), x = 0;
    for (var i = 0; i < s.length; i++) x = (x * 31 + s.charCodeAt(i)) >>> 0;
    return 'av--' + (x % 6);
  }
  function initials(name) {
    var p = String(name || '').trim().split(/\s+/).filter(Boolean);
    var s = (p[0] ? Array.from(p[0])[0] : '') + (p[1] ? Array.from(p[1])[0] : '');
    return (s || '•').toUpperCase();
  }
  // Team members get distinct tints by their place in the team list (a hash
  // put three of four people on the same colour); anyone else gets a hash.
  var TINTS = ['av--0', 'av--1', 'av--2', 'av--3', 'av--4', 'av--5'];
  function tintFor(key) { return (key && S.tint && has(S.tint, key)) ? S.tint[key] : avatarTint(key); }
  function setTeamTints(items) {
    S.tint = {};
    (items || []).forEach(function (u, i) { if (u && u.id) S.tint[u.id] = TINTS[i % TINTS.length]; });
    $$('.av[data-pid]').forEach(function (el) {
      TINTS.forEach(function (t) { el.classList.remove(t); });
      el.classList.add(tintFor(el.dataset.pid));
    });
  }
  function avatar(person, size) {
    var name = person && person.name;
    var key = person && (person.id || name);
    var ini = (person && person.initials) || initials(name);
    return h('span', { class: 'av ' + (size ? 'av--' + size + ' ' : '') + tintFor(key), 'data-pid': key || null, 'aria-hidden': 'true', text: String(ini).slice(0, 3) });
  }

  /* ------------------------------------------------------------------ dates (workspace time zone) */

  var fmtCache = {};
  function tz() { return (S.ws && S.ws.tz) || 'Europe/Moscow'; }
  function fmt(opts, zone) {
    var z = zone || tz();
    var key = z + JSON.stringify(opts);
    if (!fmtCache[key]) fmtCache[key] = new Intl.DateTimeFormat('ru-RU', Object.assign({ timeZone: z }, opts));
    return fmtCache[key];
  }
  function validTz(z) {
    try { new Intl.DateTimeFormat('ru-RU', { timeZone: z }); return true; } catch (e) { return false; }
  }
  function toDate(iso) { var d = new Date(iso); return isNaN(d.getTime()) ? null : d; }
  function ymd(d) {
    var o = {};
    fmt({ year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(d).forEach(function (p) { o[p.type] = p.value; });
    return o.year + '-' + o.month + '-' + o.day;
  }
  function dayNumber(ymdStr) {
    var p = ymdStr.split('-');
    return Date.UTC(+p[0], +p[1] - 1, +p[2]) / 86400000;
  }
  function daysAgo(d) { return dayNumber(ymd(new Date())) - dayNumber(ymd(d)); }
  function hm(d) { return fmt({ hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(d); }
  function sameYear(d) { return ymd(d).slice(0, 4) === ymd(new Date()).slice(0, 4); }
  function dm(d, long) {
    var o = { day: 'numeric', month: long ? 'long' : 'short' };
    if (!sameYear(d)) o.year = 'numeric';
    return fmt(o).format(d).replace(/\.(?=\s|$)/g, '').replace(/\s?г$/, '');
  }
  function relTime(iso) {
    var d = toDate(iso);
    if (!d) return '';
    var diff = (Date.now() - d.getTime()) / 1000;
    if (diff < 60) return 'только что';
    if (diff < 3600) return Math.floor(diff / 60) + NB + 'мин назад';
    var ago = daysAgo(d);
    if (ago <= 0) return Math.floor(diff / 3600) + NB + 'ч назад';
    if (ago === 1) return 'вчера в' + NB + hm(d);
    return dm(d);
  }
  function listTime(iso) {
    var d = toDate(iso);
    if (!d) return '—';
    var ago = daysAgo(d);
    if (ago === 0) return 'сегодня, ' + hm(d);
    if (ago === 1) return 'вчера, ' + hm(d);
    return dm(d) + ', ' + hm(d);
  }
  function fullTime(iso) {
    var d = toDate(iso);
    return d ? dm(d, true) + ', ' + hm(d) : '—';
  }
  function plainDate(str, long) { // "2026-10-17" -> "17 окт"
    var p = String(str || '').split('-');
    if (p.length !== 3) return '';
    var d = new Date(Date.UTC(+p[0], +p[1] - 1, +p[2], 12));
    return fmt({ day: 'numeric', month: long ? 'long' : 'short' }, 'UTC').format(d).replace(/\.(?=\s|$)/g, '').replace(/\s/g, NB);
  }
  function shortDate(str) { var p = String(str || '').split('-'); return p.length === 3 ? p[2] + '.' + p[1] : ''; }
  function rangeLabel(from, to) { return plainDate(from) + NB + '– ' + plainDate(to); }
  function durationMin(m) {
    if (m === null || m === undefined || isNaN(m)) return null;
    m = Math.max(0, Math.round(m));
    if (m < 60) return { n: String(m), u: 'мин' };
    var hrs = Math.floor(m / 60), rest = m % 60;
    if (hrs >= 48) return { n: String(Math.round(m / 1440)), u: 'дн' };
    return { n: String(hrs), u: rest ? 'ч' + NB + rest + NB + 'мин' : 'ч' };
  }
  function durationText(m) { var d = durationMin(m); return d ? d.n + NB + d.u : '';
  }

  /* ------------------------------------------------------------------ URL whitelists */

  function safeHttpUrl(u) {
    try {
      var url = new URL(String(u || ''));
      return (url.protocol === 'https:' || url.protocol === 'http:') ? url.href : null;
    } catch (e) { return null; }
  }
  function phoneDigits(s) {
    var t = String(s || '').trim();
    if (!/^[+\d\s()-]+$/.test(t)) return null;
    var digits = t.replace(/\D/g, '');
    if (digits.length < 10 || digits.length > 15) return null;
    return { tel: (t.charAt(0) === '+' ? '+' : '') + digits, digits: digits };
  }
  function contactLinks(lead) {
    var out = [];
    var c = String(lead.contact || '').trim();
    var kind = lead.contactKind;
    var ph = phoneDigits(kind === 'phone' || lead.mode === 'call' ? (lead.phone || c) : c);
    if (kind === 'telegram') {
      var user = c.replace(/^@/, '');
      if (/^[A-Za-z0-9_]{5,32}$/.test(user)) out.push({ label: 'Telegram', icon: 'send', href: 'https://t.me/' + user, ext: true, primary: true });
    }
    if (kind === 'whatsapp' && ph) out.push({ label: 'WhatsApp', icon: 'chat', href: 'https://wa.me/' + ph.digits, ext: true, primary: true });
    if (ph) out.push({ label: 'Позвонить', icon: 'phone', href: 'tel:' + ph.tel, primary: kind === 'phone' });
    if (kind === 'email' && c) out.push({ label: 'Написать', icon: 'mail', href: 'mailto:' + encodeURIComponent(c), primary: true });
    return out;
  }

  /* ------------------------------------------------------------------ API client */

  function ApiError(code, status, field, retry) {
    this.code = code; this.status = status; this.field = field; this.retry = retry;
    this.message = code;
  }
  ApiError.prototype = Object.create(Error.prototype);

  var API_TIMEOUT = 15000;
  function api(path, opt) {
    opt = opt || {};
    var method = opt.method || 'GET';
    var headers = { Accept: 'application/json' };
    // A request that never answers must end in an error card with «Повторить»,
    // not in an endless skeleton: our own controller aborts it after 15 s and
    // also follows the caller's signal.
    var own = new AbortController(), timedOut = false;
    var timer = setTimeout(function () { timedOut = true; own.abort(); }, opt.timeout || API_TIMEOUT);
    if (opt.signal) {
      if (opt.signal.aborted) own.abort();
      else opt.signal.addEventListener('abort', function () { own.abort(); }, { once: true });
    }
    var init = { method: method, credentials: 'same-origin', headers: headers, cache: 'no-store', signal: own.signal };
    if (opt.body !== undefined) { headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(opt.body); }
    if (method !== 'GET' && method !== 'HEAD' && S.csrf) headers['X-CSRF-Token'] = S.csrf;
    return fetch(API + path, init).then(function (res) {
      var ctype = res.headers.get('Content-Type') || '';
      var parse = opt.raw && res.ok ? Promise.resolve(res) : (ctype.indexOf('json') >= 0 ? res.json().catch(function () { return null; }) : Promise.resolve(null));
      return parse.then(function (data) {
        clearTimeout(timer);
        if (timedOut) throw new ApiError('timeout', 0);
        if (res.ok) return data;
        var code = (data && typeof data.error === 'string') ? data.error : 'http';
        var err = new ApiError(code, res.status, data && data.field, res.headers.get('Retry-After'));
        if (res.status === 401 && path !== '/login') { onUnauthorized(); }
        if (code === 'csrf' && !opt.retried) {
          return loadSession(true).then(function () { return api(path, Object.assign({}, opt, { retried: true })); });
        }
        throw err;
      });
    }, function (e) {
      clearTimeout(timer);
      if (timedOut) throw new ApiError('timeout', 0);
      if (e && e.name === 'AbortError') throw new ApiError('abort', 0);
      throw new ApiError('network', 0);
    });
  }
  function errText(e) {
    switch (e && e.code) {
      case 'network': return 'Нет связи с сервером. Проверьте подключение и попробуйте ещё раз.';
      case 'timeout': return 'Сервер не отвечает. Попробуйте ещё раз.';
      case 'auth': return 'Сеанс истёк\u00A0— войдите снова.';
      case 'csrf': return 'Сеанс устарел. Обновите страницу.';
      case 'role': return 'Недостаточно прав для этого действия.';
      case 'not_found': return 'Заявка не найдена\u00A0— возможно, её уже нет.';
      case 'invalid': return 'Сервер не принял значение' + (e.field ? ' поля «' + e.field + '»' : '') + '.';
      case 'rate': return 'Слишком много запросов\u00A0— подождите немного.';
      default: return 'Что-то пошло не так. Попробуйте ещё раз.';
    }
  }
  function onUnauthorized() {
    if (S.user) { S.user = null; renderLogin('Сеанс истёк\u00A0— войдите снова.'); }
  }

  /* ------------------------------------------------------------------ session */

  function applySession(data) {
    S.user = data.user || null;
    S.ws = data.workspace || null;
    S.csrf = data.csrf || '';
    S.counts = data.counts || { new: 0 };
    if (S.ws && !validTz(S.ws.tz)) S.ws.tz = 'Europe/Moscow';
    var accent = S.ws && S.ws.brand && S.ws.brand.accent;
    if (typeof accent === 'string' && /^#[0-9a-fA-F]{6}$/.test(accent)) document.documentElement.style.setProperty('--brand', accent);
  }
  function loadSession(quiet) {
    return api('/session').then(function (data) {
      applySession(data);
      if (quiet) updateCounts();
      return data;
    });
  }
  function can(action) {
    var r = S.user && S.user.role;
    if (action === 'edit' || action === 'export') return r === 'owner' || r === 'manager';
    if (action === 'erase') return r === 'owner';
    return false;
  }
  function wsName() { return (S.ws && S.ws.name) || DEFAULT_BRAND.name; }
  function wsLogo() {
    var l = S.ws && S.ws.brand && S.ws.brand.logo;
    return l ? Array.from(String(l)).slice(0, 2).join('') : DEFAULT_BRAND.logo;
  }

  /* ------------------------------------------------------------------ boot */

  function boot() {
    E.app = $('#app');
    E.layers = $('#layers');
    E.toasts = $('#toasts');
    var skip = $('#skip');
    if (skip) skip.addEventListener('click', function (e) {
      e.preventDefault();
      var m = $('#main');
      if (m) m.focus();
    });
    document.addEventListener('keydown', onGlobalKey);
    window.addEventListener('hashchange', onRoute);
    narrowMQ.addEventListener && narrowMQ.addEventListener('change', syncSidebarInert);
    document.addEventListener('visibilitychange', function () { if (!document.hidden && S.user) loadSession(true).catch(function () {}); });
    start();
  }
  function start() {
    fill(E.app, h('div', { class: 'boot', role: 'status' }, h('span', { class: 'boot__tile', 'aria-hidden': 'true' }), h('span', { class: 'sr', text: 'Загрузка…' })));
    loadSession().then(function () {
      mountShell();
      onRoute();
      startPolling();
    }, function (e) {
      if (e.code === 'auth' || e.status === 401) renderLogin();
      else renderFatal(e);
    });
  }
  function startPolling() {
    clearInterval(S.poll);
    S.poll = setInterval(function () {
      if (document.hidden || !S.user) return;
      var before = S.counts.new || 0;
      loadSession(true).then(function () {
        var now = S.counts.new || 0;
        if (now > before) toast((now - before === 1 ? 'Новая заявка' : 'Новых заявок: ' + (now - before)), 'sparkle');
      }).catch(function () {});
    }, 60000);
  }
  function renderFatal(e) {
    E.app.removeAttribute('aria-busy');
    fill(E.app, h('div', { class: 'login__side' },
      h('div', { class: 'card err-card', role: 'alert' },
        h('span', { class: 'err-card__ico' }, icon('alert')),
        h('div', { class: 'err-card__text' },
          h('p', { class: 'err-card__title', text: 'Не удалось открыть админку' }),
          h('p', { class: 'muted small', text: errText(e) })),
        h('button', { class: 'btn btn--primary', type: 'button', onclick: start }, icon('refresh', 'ico--sm'), 'Повторить'))));
  }

  /* ------------------------------------------------------------------ login */

  function renderLogin(notice) {
    closeAllLayers();
    clearInterval(S.poll);
    runCleanups();
    S.view = null; S.leads = null; S.team = null;
    E.shell = null;
    document.body.classList.remove('is-locked');
    E.app.removeAttribute('aria-busy');
    document.title = 'Вход · ' + DEFAULT_BRAND.name;

    var errBox = h('p', { class: 'login__error', 'aria-live': 'assertive', id: 'login-err' });
    var login = h('input', { class: 'input', id: 'login', name: 'username', type: 'email', inputmode: 'email', autocomplete: 'username', autocapitalize: 'off', spellcheck: 'false', required: true, 'aria-describedby': 'login-err' });
    var pass = h('input', { class: 'input', id: 'password', name: 'password', type: 'password', autocomplete: 'current-password', required: true, 'aria-describedby': 'login-err' });
    var eye = h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm input-wrap__btn', type: 'button', 'aria-label': 'Показать пароль', 'aria-pressed': 'false', 'aria-controls': 'password' }, icon('eye', 'ico--sm'));
    eye.style.setProperty('top', '8px');
    eye.addEventListener('click', function () {
      var show = pass.type === 'password';
      pass.type = show ? 'text' : 'password';
      eye.setAttribute('aria-pressed', String(show));
      eye.setAttribute('aria-label', show ? 'Скрыть пароль' : 'Показать пароль');
      fill(eye, icon(show ? 'eye-off' : 'eye', 'ico--sm'));
    });
    var submit = h('button', { class: 'btn btn--primary btn--block', type: 'submit' }, 'Войти', icon('arrow-right', 'ico--sm'));
    var form = h('form', { class: 'login__form', novalidate: true },
      h('div', { class: 'field' }, h('label', { class: 'field__label', for: 'login', text: 'Логин' }), login),
      h('div', { class: 'field' }, h('label', { class: 'field__label', for: 'password', text: 'Пароль' }), h('div', { class: 'input-wrap' }, pass, eye)),
      errBox, submit);

    function setError(text) {
      clear(errBox);
      if (text) add(errBox, [icon('alert', 'ico--sm'), h('span', { text: text })]);
      login.setAttribute('aria-invalid', text ? 'true' : 'false');
      pass.setAttribute('aria-invalid', text ? 'true' : 'false');
    }
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (submit.disabled) return;
      var l = login.value.trim(), p = pass.value;
      if (!l || !p) { setError('Введите логин и пароль'); (l ? pass : login).focus(); return; }
      setError('');
      submit.disabled = true; submit.classList.add('is-busy'); submit.setAttribute('aria-busy', 'true');
      api('/login', { method: 'POST', body: { login: l, password: p } }).then(function () {
        return loadSession();
      }).then(function () {
        mountShell();
        onRoute();
        startPolling();
      }).catch(function (err) {
        submit.disabled = false; submit.classList.remove('is-busy'); submit.removeAttribute('aria-busy');
        if (err.code === 'credentials') { setError('Неверный логин или пароль'); pass.select(); pass.focus(); }
        else if (err.code === 'rate') {
          var m = Math.max(1, Math.ceil((parseInt(err.retry, 10) || 60) / 60));
          setError('Слишком много попыток\u00A0— подождите ' + m + NB + 'мин');
        } else setError(errText(err));
      });
    });

    var marqueeWords = ['Заявки', 'Команда', 'Аналитика', 'Экспорт'];
    function marqueeSet(hidden) {
      return h('div', { class: 'marquee__track-part', 'aria-hidden': hidden ? 'true' : null },
        [0, 1].map(function () {
          return marqueeWords.map(function (w) { return h('span', { class: 'marquee__item' }, w, h('i', { text: '·' })); });
        }));
    }
    var miniBars = [38, 52, 30, 64, 46, 72, 88].map(function (v, i) {
      return h('span', { class: 'mini__bar' + (i < 3 ? ' mini__bar--dim' : ''), vars: { '--h': v + '%' } });
    });

    fill(E.app, h('div', { class: 'login' },
      h('section', { class: 'login__art', 'aria-hidden': 'true' },
        h('p', { class: 'login__kicker', text: 'Панель заявок' }),
        h('div', { class: 'login__copy' },
          h('p', { class: 'login__headline' }, h('span', { text: 'Все заявки' }), h('span', { class: 'login__grad', text: 'в одном месте' })),
          h('p', { class: 'login__lead', text: 'Звонки, сообщения и формы с сайта\u00A0— в одной ленте. Статусы, ответственные и заметки команды, без таблиц и пересылок.' })),
        h('div', { class: 'mini' },
          h('div', { class: 'mini__row' }, h('span', { class: 'label', text: 'Заявки за неделю' }), h('span', { class: 'round-go round-go--dark' }, icon('arrow-up-right', 'ico--sm'))),
          h('div', { class: 'mini__row' }, h('span', { class: 'mini__num', text: '48' }), h('span', { class: 'delta' }, icon('arrow-up'), '24%')),
          h('div', { class: 'mini__bars' }, miniBars),
          h('div', { class: 'mini__foot' }, h('span', { class: 'pill st--new', text: 'Новые · 6' }), h('span', { class: 'pill st--won', text: 'Договор · 3' }))),
        h('div', { class: 'marquee' }, h('div', { class: 'marquee__track' }, marqueeSet(false), marqueeSet(true)))),
      h('main', { class: 'login__side', id: 'main', tabindex: '-1' },
        h('div', { class: 'login__card' },
          h('div', { class: 'brand' }, h('span', { class: 'brand__tile', 'aria-hidden': 'true', text: DEFAULT_BRAND.logo }), h('span', { class: 'brand__name', text: DEFAULT_BRAND.name })),
          h('div', null, h('h1', { class: 'login__title', text: 'Вход в админку' }), h('p', { class: 'login__sub', text: 'Заявки с сайта, статусы и команда.' })),
          form,
          h('p', { class: 'login__foot' }, icon('shield', 'ico--sm'), 'Доступ выдаёт владелец рабочего' + NB + 'пространства')))));
    setTimeout(function () { login.focus(); }, 0);
    // a live region only announces changes made after it is in the page
    if (notice) setTimeout(function () { if (errBox.isConnected && !errBox.firstChild) add(errBox, [icon('alert', 'ico--sm'), h('span', { text: notice })]); }, 60);
  }

  function logout() {
    api('/logout', { method: 'POST' }).catch(function () {}).then(function () {
      S.user = null; S.csrf = '';
      renderLogin();
    });
  }

  /* ------------------------------------------------------------------ shell */

  function navLink(href, ico, text, key) {
    var a = h('a', { class: 'nav__link', href: href, data: { nav: key } }, icon(ico), h('span', { class: 'nav__text', text: text }));
    return a;
  }
  function mountShell() {
    runCleanups();
    var collapsed = storeGet(SIDE_KEY) === 'collapsed';
    E.app.removeAttribute('aria-busy');

    E.badge = h('span', { class: 'badge num', 'aria-hidden': 'true' });
    E.navLeads = navLink('#/leads', 'inbox', 'Заявки', 'leads');
    add(E.navLeads, E.badge);
    E.navLeadsSr = h('span', { class: 'sr' });
    add(E.navLeads, E.navLeadsSr);
    var site = safeHttpUrl(S.ws && S.ws.siteURL);

    E.collapseBtn = h('button', { class: 'nav__link collapse-btn', type: 'button', 'aria-pressed': String(collapsed), onclick: toggleCollapse },
      icon('sidebar'), h('span', { class: 'nav__text', text: 'Свернуть меню' }));
    E.sideClose = h('button', { class: 'icon-btn icon-btn--flat side-close', type: 'button', 'aria-label': 'Закрыть меню', onclick: closeMobileNav }, icon('close'));

    E.sidebar = h('aside', { class: 'sidebar', id: 'sidebar', 'aria-label': 'Меню' },
      h('div', { class: 'brand' },
        h('span', { class: 'brand__tile', 'aria-hidden': 'true', text: wsLogo() }),
        h('span', { class: 'brand__name', text: wsName() }),
        E.sideClose),
      h('nav', { class: 'nav', 'aria-label': 'Разделы' },
        navLink('#/', 'dashboard', 'Дашборд', 'dash'),
        E.navLeads,
        navLink('#/team', 'users', 'Команда', 'team'),
        navLink('#/settings', 'settings', 'Настройки', 'settings')),
      h('div', { class: 'side-foot' },
        site ? h('a', { class: 'nav__link', href: site, target: '_blank', rel: 'noopener noreferrer' }, icon('external'), h('span', { class: 'nav__text', text: 'Открыть сайт' }), h('span', { class: 'sr', text: ' (в новой вкладке)' })) : null,
        h('a', { class: 'nav__link', href: 'mailto:' + HELP_MAIL }, icon('help'), h('span', { class: 'nav__text', text: 'Помощь' })),
        E.collapseBtn,
        h('div', { class: 'me' },
          avatar(S.user),
          h('div', { class: 'me__text' }, h('span', { class: 'me__name', text: S.user.name }), h('span', { class: 'me__role', text: lab(ROLE, S.user.role, '') })),
          h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm', type: 'button', 'aria-label': 'Выйти', title: 'Выйти', onclick: logout }, icon('logout', 'ico--sm')))));

    E.scrim = h('div', { class: 'scrim', 'aria-hidden': 'true', onclick: closeMobileNav });

    // top bar
    E.search = h('input', { class: 'search__input', type: 'search', placeholder: 'Поиск по заявкам', 'aria-label': 'Поиск по заявкам', autocomplete: 'off', spellcheck: 'false', enterkeyhint: 'search' });
    var kbd = h('span', { class: 'kbd', 'aria-hidden': 'true', text: isMac ? '⌘K' : 'Ctrl' + NB + 'K' });
    E.searchClear = h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm search__clear', type: 'button', 'aria-label': 'Очистить поиск', hidden: true }, icon('close', 'ico--sm'));
    E.searchBox = h('div', { class: 'search', role: 'search' }, icon('search'), E.search, E.searchClear, kbd);
    E.searchBox.addEventListener('mousedown', function (e) { if (e.target !== E.search && !E.searchClear.contains(e.target)) { e.preventDefault(); E.search.focus(); } });
    // Outside «Заявки» the field opens the search palette — on a click or on
    // typing, never on mere focus (tabbing past it must not open a dialog).
    E.searchBox.addEventListener('click', function (e) {
      if (E.searchClear.contains(e.target) || (S.route && S.route.name === 'leads')) return;
      openPalette(E.search.value, E.search);
    });
    wireTopSearch(kbd);

    E.themeBtn = h('button', { class: 'icon-btn', type: 'button', onclick: toggleTheme });
    syncThemeBtn();
    E.bellDot = h('span', { class: 'bell__dot', hidden: true });
    E.bell = h('a', { class: 'icon-btn bell', href: '#/leads?status=new' }, icon('bell'), E.bellDot);
    E.burger = h('button', { class: 'icon-btn burger', type: 'button', 'aria-label': 'Открыть меню', 'aria-controls': 'sidebar', 'aria-expanded': 'false', onclick: openMobileNav }, icon('menu'));

    E.topbar = h('header', { class: 'topbar' },
      E.burger,
      E.searchBox,
      h('button', { class: 'icon-btn search-icon-btn', type: 'button', 'aria-label': 'Поиск', onclick: function () { openPalette(''); } }, icon('search')),
      h('span', { class: 'topbar__spacer' }),
      E.themeBtn,
      E.bell,
      h('div', { class: 'who' }, avatar(S.user),
        h('div', { class: 'who__text' }, h('span', { class: 'who__name', text: S.user.name }), h('span', { class: 'who__role', text: lab(ROLE, S.user.role, '') }))));

    E.main = h('main', { class: 'view', id: 'main', tabindex: '-1' });
    E.mainCol = h('div', { class: 'main-col' }, h('div', { class: 'main-inner' }, E.topbar, E.main));
    E.shell = h('div', { class: 'shell' + (collapsed ? ' is-collapsed' : '') },
      h('div', { class: 'haze', 'aria-hidden': 'true' }), E.sidebar, E.scrim, E.mainCol);
    fill(E.app, E.shell);
    E.firstRender = true;
    document.title = 'Заявки · ' + wsName();
    syncSidebarInert();
    updateCounts();
    loadTeam().catch(function () {});   // team tints for every avatar on the page
  }

  function wireTopSearch(kbd) {
    var push = debounce(function () {
      if (!S.route || S.route.name !== 'leads') return;
      var p = Object.assign({}, S.route.params, { q: E.search.value.trim(), page: '' });
      setHash(leadsHash(p), true);
    }, 250);
    function onLeads() { return !!(S.route && S.route.name === 'leads'); }
    E.search.addEventListener('input', function () {
      if (!onLeads() && E.search.value) {   // paste, dictation, IME: hand the text to the palette
        var v = E.search.value;
        E.search.value = '';
        openPalette(v, E.search);
      }
      E.searchClear.hidden = !E.search.value;
      kbd.hidden = !!E.search.value;
      if (onLeads()) push();
    });
    E.search.addEventListener('keydown', function (e) {
      if (!onLeads() && !e.isComposing && !e.ctrlKey && !e.metaKey && !e.altKey) {
        if (e.key.length === 1 && e.key !== ' ') { e.preventDefault(); openPalette(E.search.value + e.key, E.search); return; }
        if (e.key === 'Enter' || e.key === 'ArrowDown') { e.preventDefault(); openPalette(E.search.value, E.search); return; }
      }
      if (e.key === 'Enter' && onLeads()) { push.cancel(); var p = Object.assign({}, S.route.params, { q: E.search.value.trim(), page: '' }); setHash(leadsHash(p), true); }
      if (e.key === 'Escape' && E.search.value) { e.stopPropagation(); E.search.value = ''; E.search.dispatchEvent(new Event('input')); }
    });
    E.searchClear.addEventListener('click', function () {
      E.search.value = '';
      E.search.dispatchEvent(new Event('input'));
      E.search.focus();
    });
  }
  function syncTopSearch() {
    if (!E.search) return;
    var leads = !!(S.route && S.route.name === 'leads');
    if (leads) E.search.removeAttribute('aria-haspopup'); else E.search.setAttribute('aria-haspopup', 'dialog');
    var q = leads ? (S.route.params.q || '') : '';
    if (document.activeElement !== E.search) E.search.value = q;
    E.searchClear.hidden = !E.search.value;
    var kbd = $('.kbd', E.searchBox);
    if (kbd) kbd.hidden = !!E.search.value;
  }

  function updateCounts() {
    if (!E.shell) return;
    var n = (S.counts && S.counts.new) || 0;
    E.badge.textContent = n > 99 ? '99+' : String(n);
    E.badge.hidden = !n;
    E.navLeadsSr.textContent = n ? ', новых: ' + n : '';
    if (n) E.navLeads.setAttribute('data-badge', ''); else E.navLeads.removeAttribute('data-badge');
    E.bellDot.hidden = !n;
    E.bell.setAttribute('aria-label', n ? 'Новые заявки: ' + n : 'Новых заявок нет');
    E.bell.setAttribute('title', n ? 'Новые заявки: ' + n : 'Новых заявок нет');
    $$('[data-new-count]').forEach(function (el) { el.textContent = String(n); el.hidden = !n; });
  }

  function toggleCollapse() {
    var on = !E.shell.classList.contains('is-collapsed');
    E.shell.classList.toggle('is-collapsed', on);
    E.collapseBtn.setAttribute('aria-pressed', String(on));
    $('.nav__text', E.collapseBtn).textContent = on ? 'Развернуть меню' : 'Свернуть меню';
    storeSet(SIDE_KEY, on ? 'collapsed' : 'open');
  }

  function currentTheme() {
    var t = document.documentElement.getAttribute('data-theme');
    if (t === 'light' || t === 'dark') return t;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  function toggleTheme() {
    var next = currentTheme() === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    storeSet(THEME_KEY, next);
    syncThemeBtn();
    if (S.redrawChart) S.redrawChart();
  }
  function syncThemeBtn() {
    if (!E.themeBtn) return;
    var dark = currentTheme() === 'dark';
    fill(E.themeBtn, icon(dark ? 'sun' : 'moon'));
    E.themeBtn.setAttribute('aria-label', dark ? 'Включить светлую тему' : 'Включить тёмную тему');
    E.themeBtn.setAttribute('title', dark ? 'Светлая тема' : 'Тёмная тема');
  }

  /* mobile navigation drawer */
  function openMobileNav() {
    if (!narrowMQ.matches) return;
    E.sidebar.classList.add('is-open');
    E.sidebar.inert = false;
    E.scrim.classList.add('is-on');
    E.mainCol.inert = true;
    E.burger.setAttribute('aria-expanded', 'true');
    document.body.classList.add('is-locked');
    E.navOpen = true;
    var cur = $('.nav__link[aria-current="page"]', E.sidebar) || $('.nav__link', E.sidebar);
    setTimeout(function () { (cur || E.sideClose).focus(); }, 30);
  }
  function closeMobileNav(noFocus) {
    if (!E.navOpen) return;
    E.navOpen = false;
    E.sidebar.classList.remove('is-open');
    E.scrim.classList.remove('is-on');
    E.mainCol.inert = !!S.layers.length;
    E.burger.setAttribute('aria-expanded', 'false');
    if (!S.layers.length) document.body.classList.remove('is-locked');
    syncSidebarInert();
    if (noFocus !== true) E.burger.focus();
  }
  function syncSidebarInert() {
    if (!E.sidebar) return;
    if (!narrowMQ.matches) {
      if (E.navOpen) { E.navOpen = false; E.sidebar.classList.remove('is-open'); E.scrim.classList.remove('is-on'); E.burger.setAttribute('aria-expanded', 'false'); if (!S.layers.length) document.body.classList.remove('is-locked'); }
      E.sidebar.inert = !!S.layers.length;
      E.mainCol.inert = !!S.layers.length;
    } else {
      E.sidebar.inert = !E.navOpen || !!S.layers.length;
    }
  }

  /* ------------------------------------------------------------------ router */

  function parseHash() {
    var raw = location.hash.slice(1) || '/';
    if (raw.charAt(0) !== '/') return null;
    var qi = raw.indexOf('?');
    var path = qi >= 0 ? raw.slice(0, qi) : raw;
    var qs = new URLSearchParams(qi >= 0 ? raw.slice(qi + 1) : '');
    var params = {};
    qs.forEach(function (v, k) { params[k] = v; });
    var parts = path.split('/').filter(Boolean);
    var r = { params: params, parts: parts };
    if (!parts.length) r.name = 'dash';
    else if (parts[0] === 'leads' && parts.length === 1) r.name = 'leads';
    else if (parts[0] === 'leads' && parts.length === 2) {
      r.name = 'leads';
      try { r.lead = decodeURIComponent(parts[1]); } catch (e) { r.lead = parts[1]; }
    }
    else if (parts[0] === 'team' && parts.length === 1) r.name = 'team';
    else if (parts[0] === 'settings' && parts.length === 1) r.name = 'settings';
    else r.name = 'missing';
    return r;
  }
  function setHash(hash, replace) {
    if (location.hash === hash || (hash === '#/' && !location.hash)) { if (replace) onRoute(); return; }
    if (replace) { history.replaceState(null, '', hash); onRoute(); }
    else location.hash = hash;
  }

  var VIEW_TITLE = { dash: 'Дашборд', leads: 'Заявки', team: 'Команда', settings: 'Настройки', missing: 'Не найдено' };

  function onRoute() {
    if (!S.user || !E.shell) return;
    var r = parseHash();
    if (!r) return;
    var prev = S.route;
    S.route = r;
    closeMobileNav(true);
    closePalette(true);
    syncTopSearch();

    $$('.nav__link[data-nav]', E.sidebar).forEach(function (a) {
      if (a.dataset.nav === r.name) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
    });
    document.title = (r.lead ? 'Заявка' : VIEW_TITLE[r.name]) + ' · ' + wsName();

    if (r.name !== 'leads' && S.drawer) closeDrawer(true);

    var same = prev && prev.name === r.name && S.view === r.name;
    if (r.name === 'dash') {
      if (same && S.dash) S.dash.setDays(daysParam(r.params));
      else render(viewDashboard, r);
    } else if (r.name === 'leads') {
      if (!(same && S.leads)) render(viewLeads, r);
      else S.leads.update(r.params);
      if (r.lead) openLead(r.lead);
      else if (S.drawer) closeDrawer(true);
    } else if (r.name === 'team') { if (!same) render(viewTeam, r); }
    else if (r.name === 'settings') { if (!same) render(viewSettings, r); }
    else render(viewMissing, r);
  }

  function runCleanups() {
    S.cleanups.forEach(function (fn) { try { fn(); } catch (e) { /* ignore */ } });
    S.cleanups = [];
    S.redrawChart = null;
    S.dash = null;
    S.leads = null;
  }
  function render(fn, r) {
    // Move focus to the new page's heading only on in-app navigation. On the
    // first render after boot or login focus stays at the document start, so
    // the first Tab reaches the skip link.
    var first = !!E.firstRender;
    E.firstRender = false;
    var fromNav = !first && document.activeElement && (E.sidebar.contains(document.activeElement) || document.activeElement === document.body);
    runCleanups();
    S.nav += 1;
    S.view = r.name;
    clear(E.main);
    window.scrollTo(0, 0);
    fn(E.main, r, S.nav);
    if (fromNav) {
      var h1 = $('h1', E.main);
      if (h1) { h1.setAttribute('tabindex', '-1'); h1.focus({ preventScroll: true }); }
    }
  }
  function alive(token) { return token === S.nav; }

  function pageHead(title, sub, actions) {
    return h('div', { class: 'page-head' },
      h('div', { class: 'page-head__text' }, h('h1', { class: 'page-title', text: title }), sub ? h('p', { class: 'page-sub', text: sub }) : null),
      actions ? h('div', { class: 'page-actions' }, actions) : null);
  }
  function rise(el, i) { if (el) { el.classList.add('rise'); el.style.setProperty('--i', String(i || 0)); } return el; }
  function errorCard(e, retry) {
    return h('div', { class: 'card err-card', role: 'alert' },
      h('span', { class: 'err-card__ico' }, icon('alert')),
      h('div', { class: 'err-card__text' },
        h('p', { class: 'err-card__title', text: 'Не удалось загрузить данные' }),
        h('p', { class: 'muted small', text: errText(e) })),
      retry ? h('button', { class: 'btn btn--primary', type: 'button', onclick: retry }, icon('refresh', 'ico--sm'), 'Повторить') : null);
  }

  /* ------------------------------------------------------------------ count-up */

  function countUp(el, to, format) {
    format = format || num;
    if (reduceMotion.matches || !(to > 0)) { el.textContent = format(to); return; }
    var start = performance.now(), dur = 700;
    function step(t) {
      var k = Math.min(1, (t - start) / dur);
      var e = 1 - Math.pow(1 - k, 3);
      // integers stay integers mid-animation: «57,91» reads as a fraction
      el.textContent = format(k < 1 ? (format === num ? Math.round(to * e) : to * e) : to);
      if (k < 1 && el.isConnected) requestAnimationFrame(step);
    }
    el.textContent = format(0);
    requestAnimationFrame(step);
  }

  /* ------------------------------------------------------------------ export */

  function exportCsv(params, btn) {
    if (!can('export')) return;
    var qs = new URLSearchParams();
    ['status', 'q', 'mode', 'interest', 'budget', 'assignee', 'waiting', 'stale', 'sort'].forEach(function (k) { if (params && params[k]) qs.set(k, params[k]); });
    if (btn) { btn.disabled = true; btn.setAttribute('aria-busy', 'true'); }
    var done = function () { if (btn) { btn.disabled = false; btn.removeAttribute('aria-busy'); } };
    fetch(API + '/export.csv' + (qs.toString() ? '?' + qs : ''), { credentials: 'same-origin', cache: 'no-store' }).then(function (res) {
      if (res.status === 401) { onUnauthorized(); throw new ApiError('auth', 401); }
      if (!res.ok) return res.json().catch(function () { return {}; }).then(function (d) { throw new ApiError((d && d.error) || 'http', res.status); });
      var cd = res.headers.get('Content-Disposition') || '';
      var m = /filename="?([A-Za-z0-9._-]{1,80})"?/.exec(cd);
      var name = m ? m[1] : 'leads-' + ymd(new Date()).replace(/-/g, '') + '.csv';
      return res.blob().then(function (blob) {
        var url = URL.createObjectURL(blob);
        var a = h('a', { href: url, download: name });
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(function () { URL.revokeObjectURL(url); }, 4000);
        toast('Файл ' + name + ' сохранён', 'download', 'ok');
      });
    }).catch(function (e) {
      if (e.code !== 'auth') toast(errText(e.code ? e : new ApiError('network')), 'alert', 'error');
    }).then(done);
  }

  /* ------------------------------------------------------------------ dashboard */

  function daysParam(p) { var d = parseInt(p.days, 10); return d === 7 || d === 90 ? d : 30; }

  function viewDashboard(root, r, token) {
    var days = daysParam(r.params);
    var seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Период' });
    [7, 30, 90].forEach(function (d) {
      add(seg, h('button', { class: 'seg__opt', type: 'button', 'aria-pressed': String(d === days), data: { days: String(d) }, text: d + ' дней',
        onclick: function () { setHash(d === 30 ? '#/' : '#/?days=' + d); } }));
    });
    var head = pageHead('С возвращением, ' + firstName(S.user.name) + '!', 'Вот что происходит с заявками', seg);

    var site = safeHttpUrl(S.ws && S.ws.siteURL);
    var newBadge = h('span', { class: 'badge num', 'data-new-count': '', text: String(S.counts.new || 0) });
    newBadge.hidden = !S.counts.new;
    var tiles = [
      h('a', { class: 'qa__tile qa__tile--dark', href: '#/leads?status=new' }, icon('sparkle'), h('span', { class: 'qa__label', text: 'Разобрать новые' }), newBadge),
      h('a', { class: 'qa__tile', href: '#/leads' }, icon('inbox'), h('span', { class: 'qa__label', text: 'Все заявки' })),
      can('export') ? h('button', { class: 'qa__tile', type: 'button', onclick: function (e) { exportCsv({}, e.currentTarget); } }, icon('download'), h('span', { class: 'qa__label', text: 'Экспорт CSV' })) : null,
      site ? h('a', { class: 'qa__tile', href: site, target: '_blank', rel: 'noopener noreferrer' }, icon('globe'), h('span', { class: 'qa__label', text: 'Открыть сайт' }), h('span', { class: 'sr', text: '(в новой вкладке)' })) : null,
      h('a', { class: 'qa__tile', href: '#/team' }, icon('users'), h('span', { class: 'qa__label', text: 'Команда' })),
      h('a', { class: 'qa__tile', href: '#/settings' }, icon('settings'), h('span', { class: 'qa__label', text: 'Настройки' }))
    ].filter(Boolean);
    tiles.forEach(function (t, i) { rise(t, i); });
    var qa = h('nav', { class: 'qa', 'aria-label': 'Быстрые действия', vars: { '--n': String(tiles.length) } }, tiles);
    qa.style.setProperty('grid-template-columns', 'repeat(' + tiles.length + ', minmax(0, 1fr))');

    var body = h('div', { class: 'dash' });
    fill(root, h('div', { class: 'dash' }, head, qa, body));

    var ctrl = null;
    var loadCleanups = [];   // teardown of the rows of the current period (chart observer)
    function dropRows() { loadCleanups.forEach(function (fn) { try { fn(); } catch (e) { /* ignore */ } }); loadCleanups = []; S.redrawChart = null; }
    function load(d) {
      days = d;
      $$('.seg__opt', seg).forEach(function (o) { o.setAttribute('aria-pressed', String(+o.dataset.days === d)); });
      if (ctrl) ctrl.abort();
      ctrl = new AbortController();
      var my = ctrl;
      dropRows();
      fill(body, dashSkeleton());
      body.setAttribute('aria-busy', 'true');
      Promise.all([
        api('/stats?days=' + d, { signal: my.signal }),
        api('/activity?limit=5', { signal: my.signal }),
        // current counts (the same numbers as the tabs of «Заявки») and the oldest new lead
        api('/leads?status=new&sort=old&limit=1', { signal: my.signal }).catch(function (e) { if (e.code === 'abort') throw e; return null; }),
        loadTeam().catch(function () { return null; })
      ]).then(function (res) {
        if (!alive(token) || my !== ctrl) return;
        body.removeAttribute('aria-busy');
        fill(body, dashRows(res[0], res[1].items || [], res[2], loadCleanups));
      }, function (e) {
        if (!alive(token) || my !== ctrl || e.code === 'abort' || e.code === 'auth') return;
        body.removeAttribute('aria-busy');
        fill(body, errorCard(e, function () { load(days); }));
      });
    }
    S.dash = { setDays: function (d) { if (d !== days) load(d); } };
    S.cleanups.push(function () { if (ctrl) ctrl.abort(); dropRows(); });
    load(days);
  }

  function dashSkeleton() {
    function sk(cls) { return h('div', { class: 'skel ' + (cls || '') }); }
    function card(extra, inner) { return h('div', { class: 'card ' + (extra || '') }, inner); }
    return [
      h('div', { class: 'row-a' },
        card('', [sk('skel--line'), h('div', { class: 'kpis', vars: { 'margin-top': '0' } }, [0, 1, 2].map(function () { return h('div', { class: 'tile kpi' }, sk('skel--line'), sk('skel--num')); })),
          h('div', { class: 'ranks' }, sk(), sk())]),
        card('', [sk('skel--line'), h('div', { class: 'feed' }, [0, 1, 2, 3].map(function () { return h('div', { class: 'feed__item' }, h('span', { class: 'av skel' }), h('div', { class: 'feed__body' }, sk('skel--line'))); }))])),
      h('div', { class: 'row-b' },
        card('chart-card', [sk('skel--line'), sk('skel--num'), h('div', { class: 'skel skel--chart' })]),
        h('div', { class: 'col-stack' }, card('', [sk('skel--line'), sk()]), card('', [sk('skel--line'), sk(), sk()])),
        card('speed', [h('span', { class: 'label', text: 'Скорость ответа' })]))
    ];
  }

  // Change against the previous period. A percentage on a tiny base
  // («+6850%» after 2 leads) means nothing, so below 10 leads or beyond ±300%
  // the absolute change is shown instead. null = no previous data.
  function deltaInfo(cur, prev, points) {
    var dir, sign;
    if (points) {
      var dp = (cur - prev) * 100;
      dir = Math.abs(dp) < 0.05 ? 0 : dp > 0 ? 1 : -1;
      return { dir: dir, text: pct1(Math.abs(dp)) + NB + 'п.п.', sr: dir > 0 ? 'рост на ' : dir < 0 ? 'снижение на ' : 'без изменений, ' };
    }
    if (!prev) return null;
    dir = cur === prev ? 0 : cur > prev ? 1 : -1;
    sign = dir > 0 ? '+' : dir < 0 ? '−' : '';
    var pc = Math.round((cur - prev) / prev * 100);
    if (prev < 10 || Math.abs(pc) > 300) {
      return { dir: dir, abs: true, n: num(Math.abs(cur - prev)), text: sign + num(Math.abs(cur - prev)), sr: dir > 0 ? 'больше на ' : dir < 0 ? 'меньше на ' : 'столько же, ' };
    }
    return { dir: dir, text: Math.abs(pc) + '%', signed: sign + Math.abs(pc) + '%', sr: dir > 0 ? 'рост на ' : dir < 0 ? 'снижение на ' : 'без изменений, ' };
  }
  function deltaChip(info) {
    if (!info) return null;
    return h('span', { class: 'delta' + (info.dir < 0 ? ' delta--down' : info.dir === 0 ? ' delta--flat' : '') },
      info.dir && !info.abs ? icon(info.dir > 0 ? 'arrow-up' : 'arrow-down') : null,
      h('span', { class: 'sr', text: info.sr }),
      info.abs ? h('span', { 'aria-hidden': 'true', text: info.text }) : info.text,
      info.abs ? h('span', { class: 'sr', text: info.n }) : null);
  }

  function dashRows(st, events, leadsNow, cleanups) {
    var t = st.totals || {};
    var range = rangeLabel(st.from, st.to);
    var i = 0;
    function card(cls, kids) { return rise(h('section', { class: 'card ' + (cls || '') }, kids), i++); }
    function head(label, right, id) {
      return h('div', { class: 'card__head' }, h('h2', { class: 'label', id: id, text: label }), right || null);
    }
    function rangeChip() { return h('span', { class: 'kbd' }, icon('calendar', 'ico--xs'), range); }

    // current numbers: the same ones the tabs of «Заявки» show
    var cnt = leadsNow && leadsNow.counts ? leadsNow.counts : null;
    var nowNew = cnt ? (cnt.new || 0) : (S.counts.new || 0);
    var nowProgress = cnt && typeof cnt.progress === 'number' ? cnt.progress : null;
    var nowClosed = cnt ? (cnt.closed || 0) : null;
    var oldestNew = leadsNow && leadsNow.items && leadsNow.items[0];

    /* period card */
    var waiting = (st.attention || []).filter(function (a) { return a.kind === 'waiting'; })[0];
    var waitN = waiting ? (waiting.count || 0) : 0;
    var nums = [];
    // label · number alone on its baseline · chip and caption on one line under it
    function kpi(label, ico, value, format, chip, caption, href) {
      var n = h('span', { class: 'kpi__num' });
      nums.push([n, value, format]);
      return h(href ? 'a' : 'div', { class: 'tile kpi' + (href ? ' kpi--link' : ''), href: href || null },
        h('div', { class: 'kpi__top' }, h('span', { class: 'kpi__label', text: label }), icon(ico, 'kpi__ico')),
        h('div', { class: 'kpi__row' }, n),
        h('p', { class: 'kpi__foot' }, chip, caption ? h('span', { class: 'kpi__cap', text: caption }) : null));
    }
    var convFmt = function (v) { return pct1(v * 100) + '%'; };
    var leadsDelta = deltaInfo(t.leads || 0, t.prevLeads || 0);
    var waitChip = null, waitCap;
    if (nowNew) {
      // the verb agrees with the number that waits, not with all the new ones
      var waitVerb = plural(waitN, ['ждёт', 'ждут', 'ждут']) + ' дольше часа';
      var waitAll = waitN >= nowNew;
      var waitSr = (waitAll ? (nowNew === 1 ? '' : 'все ') : 'из них ' + num(waitN) + ' ') + waitVerb;
      var waitTitle = waitAll ? (nowNew === 1 ? 'Ждёт' : 'Все ждут') + ' дольше часа'
        : num(waitN) + NB + 'из' + NB + num(nowNew) + ' ' + waitVerb;
      waitChip = waitN > 0
        ? h('span', { class: 'delta delta--warn', title: waitTitle }, icon('alert'),
            // one waiting lead: just «> 1 ч»; several: «все > 1 ч» or «2 > 1 ч»
            h('span', { 'aria-hidden': 'true', text: (nowNew === 1 ? '' : (waitN >= nowNew ? 'все' : num(waitN)) + NB) + '>' + NB + '1' + NB + 'ч' }),
            h('span', { class: 'sr', text: waitSr }))
        : h('span', { class: 'delta delta--flat' }, icon('check'), 'в срок');
      var oldT = oldestNew ? toDate(oldestNew.time) : null;
      waitCap = oldT ? 'старейшая ждёт ' + durationText((Date.now() - oldT.getTime()) / 60000) : 'новые, без ответа';
    } else waitCap = 'новых заявок нет';
    var kpis = h('div', { class: 'kpis' },
      kpi('Всего заявок', 'inbox', t.leads || 0, num, deltaChip(leadsDelta), leadsDelta ? 'к прошлому периоду' : 'за прошлый период данных нет'),
      kpi('Ждут ответа', 'clock', nowNew, num, waitChip, waitCap, '#/leads?status=new'),
      kpi('Конверсия в договор', 'target', t.conversion || 0, convFmt,
        t.prevLeads ? deltaChip(deltaInfo(t.conversion || 0, t.prevConversion || 0, true)) : null,
        t.prevLeads ? 'к прошлому периоду' : 'за прошлый период данных нет'));

    function rankCard(cls, title, items, link, keyName, ofAll) {
      var total = items.reduce(function (s, x) { return s + (x.count || 0); }, 0);
      var list = h('ol', { class: 'rank__list' });
      items.slice(0, 3).forEach(function (x, k) {
        var q = {}; q[keyName] = x.key;
        var share = total ? (x.count || 0) / total : 0;
        add(list, h('li', null, h('a', { class: 'rank__row', href: leadsHash(q) },
          h('span', { class: 'rank__n', text: '0' + (k + 1) }),
          h('span', { class: 'rank__body' },
            h('span', { class: 'rank__name', text: x.label || lab(keyName === 'mode' ? MODE : INTEREST, x.key) }),
            h('span', { class: 'rank__bar', 'aria-hidden': 'true' }, h('span', { class: 'rank__fill', vars: { '--w': (share * 100).toFixed(1) + '%' } }))),
          h('span', { class: 'rank__count', title: num(x.count) + NB + leadsWord(x.count) }, num(x.count), h('span', { class: 'sr', text: ' ' + leadsWord(x.count) })))));
      });
      if (!items.length) add(list, h('li', { class: 'rank__empty', text: 'Пока нет данных за период' }));
      return h('div', { class: 'rank ' + cls },
        h('div', { class: 'rank__head' },
          h('h3', { class: 'label', text: title }),
          h('a', { class: 'round-go ' + (cls === 'rank--accent' ? 'round-go--glass' : 'icon-btn--tile'), href: link, 'aria-label': title + ': все заявки' }, icon('arrow-up-right', 'ico--sm'))),
        list,
        // the rows stay together at the top; the total closes the card at the bottom
        // byMode adds up to the period total; byInterest only counts leads that named an interest
        !total ? null : keyName === 'interest'
          ? h('p', { class: 'rank__total' }, 'Указали интерес: ', h('b', { text: ofAll > total ? num(total) + NB + 'из' + NB + num(ofAll) : (total > 1 ? 'все' + NB : '') + num(total) }))
          : h('p', { class: 'rank__total' }, 'Всего за период: ', h('b', { text: num(total) + NB + leadsWord(total) })));
    }
    var perf = card('perf', [
      head('Заявки за период', rangeChip(), 'h-perf'),
      kpis,
      h('div', { class: 'ranks' },
        rankCard('rank--accent', 'Что интересует', st.byInterest || [], '#/leads', 'interest', t.leads || 0),
        rankCard('rank--plain', 'Как связываются', st.byMode || [], '#/leads', 'mode'))
    ]);
    perf.setAttribute('aria-labelledby', 'h-perf');

    /* activity card */
    // The chips show what their links open: the current tabs of «Заявки»
    // (not the period), so «В работе 23» opens exactly those 23 rows.
    var bs = st.byStatus || {};
    var inWork = (bs.contacted || 0) + (bs.proposal || 0);
    function chipStat(cls, label, value, href) {
      return h('a', { class: 'chip-stat ' + cls, href: href }, h('span', { class: 'chip-stat__label', text: label }), h('span', { class: 'chip-stat__num', text: value === null ? '—' : num(value) }));
    }
    var chips = h('div', { class: 'chips', role: 'group', 'aria-label': 'Заявки сейчас' },
      chipStat('chip-stat--dark', 'Новые', nowNew, '#/leads?status=new'),
      chipStat('chip-stat--accent', 'В работе', nowProgress, '#/leads?status=progress'),
      chipStat('chip-stat--light', 'Закрыты', nowClosed, '#/leads?status=closed'));
    var feed = h('ul', { class: 'feed' });
    events.slice(0, 5).forEach(function (ev) {
      var lead = ev.lead || {};
      add(feed, h('li', null, h('a', { class: 'feed__item', href: '#/leads/' + encodeURIComponent(lead.id || '') },
        ev.kind === 'created' || !ev.by ? h('span', { class: 'av av--visitor', 'aria-hidden': 'true' }, icon('user', 'ico--sm')) : avatar(ev.by),
        h('div', { class: 'feed__body' },
          h('p', { class: 'feed__text' }, eventSentence(ev, lead)),
          h('span', { class: 'feed__time', text: relTime(ev.time) })),
        h('span', { class: 'round-go', 'aria-hidden': 'true' }, icon('arrow-up-right', 'ico--sm')))));
    });
    if (!events.length) add(feed, h('li', { class: 'feed__empty', text: 'Событий пока нет. Как только придёт первая заявка, она появится здесь.' }));
    var activity = card('', [
      head('Активность', h('a', { class: 'btn btn--sm btn--soft', href: '#/leads?sort=updated' }, 'Смотреть все', h('span', { class: 'round-go round-go--dark', 'aria-hidden': 'true' }, icon('arrow-up-right', 'ico--xs'))), 'h-act'),
      chips, feed]);
    activity.querySelector('.btn--soft').style.setProperty('padding-right', '4px');
    activity.setAttribute('aria-labelledby', 'h-act');

    /* chart card */
    var chartBox = h('div', { class: 'chart-box' });
    var totalWon = (st.series || []).reduce(function (s, p) { return s + (p.won || 0); }, 0);
    var summaryNum = h('span', { class: 'chart-summary__num', text: num(t.leads || 0) });
    var deltaTxt = '';
    if (leadsDelta) {
      if (leadsDelta.abs) deltaTxt = leadsDelta.dir ? ', на' + NB + leadsDelta.n + ' ' + (leadsDelta.dir > 0 ? 'больше' : 'меньше') + ', чем в прошлом периоде' : ', столько же, сколько в прошлом периоде';
      else deltaTxt = ', ' + leadsDelta.signed + ' к прошлому периоду';
    }
    var foot = h('p', { class: 'chart-foot' });
    var chart = card('chart-card', [
      head('Заявки по дням', rangeChip(), 'h-chart'),
      h('div', { class: 'chart-summary' }, summaryNum, h('span', { class: 'chart-summary__text', text: leadsWord(t.leads || 0) + deltaTxt })),
      h('div', { class: 'chart-legend', 'aria-hidden': 'true' },
        h('span', null, h('i', { class: 'lg-bar' }), 'Заявки'),
        h('span', null, h('i', { class: 'lg-line' }), 'Договоры')),
      chartBox,
      foot]);
    chart.setAttribute('aria-labelledby', 'h-chart');

    /* status card */
    var pOpen = (bs.new || 0) + inWork, pWon = bs.won || 0, pLost = (bs.lost || 0) + (bs.spam || 0);
    var pTotal = pOpen + pWon + pLost;
    function pc(v) { return pTotal ? Math.round(v / pTotal * 100) : 0; }
    var bar = h('div', { class: 'sbar', role: 'img', 'aria-label': pTotal ? 'Открыты ' + pc(pOpen) + '%, договор ' + pc(pWon) + '%, отказ и спам ' + pc(pLost) + '%' : 'Нет заявок за период' });
    if (!pTotal) add(bar, h('span', { class: 'sbar__seg sbar__seg--empty' }));
    [['open', pOpen], ['won', pWon], ['lost', pLost]].forEach(function (x) {
      if (!x[1]) return;
      var share = x[1] / pTotal;
      add(bar, h('span', { class: 'sbar__seg sbar__seg--' + x[0], vars: { '--w': String(share) }, text: share >= 0.14 ? pc(x[1]) + '%' : '' }));
    });
    // period numbers: not links, since the lists of «Заявки» are not limited
    // to the period and would show other numbers under the same words
    function legendItem(cls, name, value) {
      return h('div', { class: 'legend__item legend__item--' + cls },
        h('span', { class: 'legend__name', text: name }),
        h('span', { class: 'legend__num', text: num(value) }),
        h('span', { class: 'legend__pct', text: pc(value) + '%' }));
    }
    var status = card('status-card', [
      h('div', { class: 'card__head' }, h('div', null, h('h2', { class: 'label', id: 'h-status', text: 'Статус заявок' }), h('p', { class: 'card__sub', text: 'За выбранный период' }))),
      bar,
      h('div', { class: 'legend' },
        legendItem('open', 'Открыты', pOpen),
        legendItem('won', 'Договор', pWon),
        legendItem('lost', 'Отказ, спам', pLost))]);
    status.setAttribute('aria-labelledby', 'h-status');

    /* attention */
    var att = (st.attention || []).filter(function (a) { return a && a.count > 0; });
    var attList = h('ul', { class: 'attn' });
    att.forEach(function (a) {
      add(attList, h('li', null, h('a', { class: 'attn__item', href: hashFromQuery(a.query) },
        h('span', { class: 'attn__count', text: num(a.count) }),
        h('span', { class: 'attn__body' }, h('span', { class: 'attn__title', text: a.title || '' }), h('span', { class: 'attn__hint', text: a.hint || '' })),
        h('span', { class: 'round-go', 'aria-hidden': 'true' }, icon('arrow-up-right', 'ico--sm')))));
    });
    var attention = card('attn-card', [
      head('Требует внимания', null, 'h-attn'),
      att.length ? attList : h('p', { class: 'attn__done' }, icon('check-circle'), 'Всё разобрано ✓')]);
    attention.setAttribute('aria-labelledby', 'h-attn');

    /* speed */
    var med = durationMin(t.responseMedianMin);
    var target = t.responseTargetMin || 60;
    var p = t.responseMedianMin === null || t.responseMedianMin === undefined ? 0 : Math.min(100, Math.round(t.responseMedianMin / target * 100));
    var speedNum = h('div', { class: 'speed__num' }, med ? [med.n, h('small', { text: NB + med.u })] : '—');
    var share = Math.round((t.respondedShare || 0) * 100);
    var over = med && t.responseMedianMin > target;
    var speed = card('speed', [
      h('div', { class: 'card__head' }, h('h2', { class: 'label', id: 'h-speed', text: 'Скорость ответа' }), h('span', { class: 'round-go round-go--glass', 'aria-hidden': 'true' }, icon('zap', 'ico--sm'))),
      h('div', null, speedNum, h('p', { class: 'speed__meta', text: med ? 'медиана за период' : 'нет ответов за период' })),
      h('div', null,
        h('div', { class: 'prog', role: 'img', 'aria-label': med ? 'Медиана ' + med.n + NB + med.u + ' при цели ' + target + NB + 'мин' : 'Нет данных' },
          h('span', { class: 'prog__fill', vars: { '--p': p + '%' } })),
        h('div', { class: 'prog-labels' }, h('span', { text: '0' }), h('span', { text: over ? 'выше цели' : 'цель' + NB + target + NB + 'мин' }))),
      // after a percentage the noun is always genitive plural: «на 97% заявок»
      h('p', { class: 'speed__share', text: 'Ответили на ' + share + '%' + NB + 'заявок' }),
      h('div', { class: 'speed__tip' },
        h('span', { class: 'label' }, icon('bulb', 'ico--xs'), 'Совет'),
        h('p', { text: 'Чем быстрее первый ответ, тем выше шанс договора. Уведомления в Telegram помогают не пропускать новые заявки.' }))]);
    speed.setAttribute('aria-labelledby', 'h-speed');

    var rows = [
      h('div', { class: 'row-a' }, perf, activity),
      h('div', { class: 'row-b' }, chart, h('div', { class: 'col-stack' }, status, attention), speed)
    ];

    // after insertion: numbers + chart. The observer belongs to these rows
    // only: load() disconnects it before the next period replaces them.
    requestAnimationFrame(function () {
      if (!chartBox.isConnected) return;
      nums.forEach(function (x) { countUp(x[0], x[1], x[2]); });
      var lastW = -1, lastH = -1;
      var draw = function () {
        if (!chartBox.isConnected) return;
        var size = drawChart(chartBox, st, foot, totalWon);
        lastW = size.w; lastH = size.h;   // the size the chart was drawn for
      };
      draw();
      var ro = new ResizeObserver(function () {
        if (!chartBox.isConnected) { ro.disconnect(); return; }
        if (Math.abs(chartBox.clientWidth - lastW) < 2 && Math.abs(chartBox.clientHeight - lastH) < 2) return;
        draw();
      });
      ro.observe(chartBox);
      S.redrawChart = draw;
      (cleanups || S.cleanups).push(function () { ro.disconnect(); });
    });
    return rows;
  }

  function hashFromQuery(q) {
    var src = new URLSearchParams(String(q || ''));
    var p = {};
    ['status', 'mode', 'interest', 'budget', 'assignee', 'waiting', 'stale', 'sort', 'q'].forEach(function (k) { if (src.get(k)) p[k] = src.get(k); });
    return leadsHash(p);
  }

  function eventSentence(ev, lead) {
    var who = ev.by ? ev.by.name : '';
    var name = lead ? lead.name : '';
    var gen = genitive(name);
    var ref = gen ? b(gen) : name ? [ '«', b(name), '»' ] : null;
    switch (ev.kind) {
      case 'created':
        var how = lead && { call: 'звонок', write: 'сообщение', form: 'форма' }[lead.mode];
        return name ? ['Новая заявка: ', b(name), how ? ', ' + how : ''] : ['Новая заявка' + (how ? ': ' + how : '')];
      case 'status':
        return [b(who), ' ' + verb(who, 'перевёл', 'перевела') + ' заявку ', ref ? [ref, ' '] : '', 'в «' + lab(STATUS, ev.to) + '»'];
      case 'note':
        return [b(who), ' ' + verb(who, 'оставил', 'оставила') + ' заметку к заявке', ref ? [' ', ref] : ''];
      case 'assign':
        if (ev.assignee && ev.by && ev.assignee.id === ev.by.id) return [b(who), ' ' + verb(who, 'взял', 'взяла') + ' заявку', ref ? [' ', ref] : '', ' в работу'];
        return ev.assignee
          ? [b(who), ': ответственный за заявку', ref ? [' ', ref] : '', NB + '— ', b(ev.assignee.name)]
          : [b(who), ' ' + verb(who, 'снял', 'сняла') + ' ответственного с заявки', ref ? [' ', ref] : ''];
      case 'erase':
        return [b(who), ' ' + verb(who, 'удалил', 'удалила') + ' персональные данные из заявки'];
      default:
        return [b(who || 'Система'), ' изменил(а) заявку'];
    }
  }

  /* chart */
  function niceStep(max) {
    var step = Math.max(1, Math.ceil(max / 4));
    if (step > 50) step = Math.ceil(step / 50) * 50;
    else if (step > 10) step = Math.ceil(step / 10) * 10;
    else if (step > 5) step = Math.ceil(step / 5) * 5;
    return step;
  }
  function barPath(x, y, w, hgt, r) {
    r = Math.min(r, w / 2, hgt);
    return 'M' + x + ',' + (y + hgt) + 'V' + (y + r) + 'Q' + x + ',' + y + ' ' + (x + r) + ',' + y +
      'H' + (x + w - r) + 'Q' + (x + w) + ',' + y + ' ' + (x + w) + ',' + (y + r) + 'V' + (y + hgt) + 'Z';
  }
  // Returns the size it was drawn for; the caller's ResizeObserver redraws
  // when the box size differs from it.
  function drawChart(box, st, foot, totalWon) {
    clear(box);
    var series = (st.series || []).map(function (p) { return { from: p.date, to: p.date, days: 1, leads: p.leads || 0, won: p.won || 0 }; });
    var boxW = box.clientWidth;
    var W = Math.max(240, Math.floor(boxW || 600));
    var padL = 30, padR = 6, padT = 32, padB = 30;
    var pw = W - padL - padR;
    var weekly = false;
    if (series.length > 7 && (pw / series.length) * 0.56 < 10) {
      weekly = true;
      var out = [];
      for (var e = series.length; e > 0; e -= 7) {
        var chunk = series.slice(Math.max(0, e - 7), e);
        out.unshift({ from: chunk[0].from, to: chunk[chunk.length - 1].to, days: chunk.length, partial: chunk.length < 7,
          leads: chunk.reduce(function (s, x) { return s + x.leads; }, 0),
          won: chunk.reduce(function (s, x) { return s + x.won; }, 0) });
      }
      series = out;
    }
    var partialFirst = weekly && series.length && series[0].partial;
    // The caption goes in first: it takes a line under the chart, so the
    // height is measured only after it is there (it used to overlap the axis).
    if (foot) foot.textContent = (weekly ? 'По неделям' : 'По дням') + NB + '· ' + rangeLabel(st.from, st.to) + NB + '· ' + num(totalWon) + NB + plural(totalWon, ['договор', 'договора', 'договоров']) +
      (partialFirst ? NB + '· первая неделя неполная (' + series[0].days + NB + 'дн.)' : '');
    var boxH = box.clientHeight;
    var H = Math.max(220, Math.min(460, Math.floor(boxH || 260)));
    var ph = H - padT - padB;
    var n = series.length || 1;
    var slot = pw / n;
    var bw = Math.max(4, Math.min(40, slot * 0.58));
    var maxV = Math.max(1, Math.max.apply(null, series.map(function (p) { return Math.max(p.leads, p.won); })));
    var step = niceStep(maxV);
    var top = step * 4;
    function y(v) { return padT + ph - (v / top) * ph; }
    function cx(i) { return padL + slot * (i + 0.5); }

    var maxDay = series.reduce(function (m, p) { return p.leads > m.leads ? p : m; }, { leads: -1 });
    var label = 'Заявки ' + (weekly ? 'по неделям' : 'по дням') + ' за ' + rangeLabel(st.from, st.to) + ': всего ' + num(st.totals && st.totals.leads) +
      (maxDay.leads > 0 ? ', больше всего ' + num(maxDay.leads) + ' (' + (weekly ? plainDate(maxDay.from) + ' – ' + plainDate(maxDay.to) : plainDate(maxDay.from)) + ')' : '') +
      ', договоров ' + num(totalWon) + '. Подробности в таблице ниже.';

    var root = svg('svg', { class: 'chart-svg', viewBox: '0 0 ' + W + ' ' + H, width: W, height: H, role: 'img', 'aria-label': label, preserveAspectRatio: 'xMidYMid meet' });
    root.style.setProperty('height', H + 'px');
    add(root, svg('defs', null,
      svg('pattern', { id: 'hatch', patternUnits: 'userSpaceOnUse', width: '7', height: '7', patternTransform: 'rotate(45)' },
        svg('rect', { class: 'hatch-bg', width: '7', height: '7' }),
        svg('line', { class: 'hatch-line', x1: '1', y1: '0', x2: '1', y2: '7' }))));
    var g = svg('g');
    for (var k = 0; k <= 4; k++) {
      var gy = y(step * k);
      add(g, svg('line', { class: 'grid', x1: padL, x2: W - padR, y1: gy, y2: gy, 'stroke-dasharray': k ? '3 4' : null }));
      add(g, svg('text', { class: 'axis-y', x: padL - 8, y: gy + 4, 'text-anchor': 'end', text: num(step * k) }));
    }
    add(root, g);
    var bars = svg('g');
    series.forEach(function (p, i) {
      var x = cx(i) - bw / 2;
      if (p.leads > 0) {
        var bh = Math.max(3, (p.leads / top) * ph);
        add(bars, svg('path', { class: 'bar' + (p.partial ? ' bar--partial' : ''), d: barPath(x, padT + ph - bh, bw, bh, Math.min(8, bw / 3)) },
          svg('title', { text: (weekly ? plainDate(p.from) + ' – ' + plainDate(p.to) : plainDate(p.from)) + (p.partial ? ' (неполная неделя, ' + p.days + NB + 'дн.)' : '') + ': ' + p.leads + NB + leadsWord(p.leads) + ', договоров: ' + p.won })));
      } else {
        add(bars, svg('rect', { class: 'bar--zero', x: x, y: padT + ph - 2, width: bw, height: 2, rx: 1 }));
      }
    });
    add(root, bars);
    // won line
    if (series.length > 1) {
      var pts = series.map(function (p, i) { return cx(i).toFixed(1) + ',' + y(p.won).toFixed(1); }).join(' ');
      add(root, svg('polyline', { class: 'won-line', points: pts }));
    }
    var dots = svg('g');
    series.forEach(function (p, i) {
      if (series.length <= 31 || i === series.length - 1) add(dots, svg('circle', { class: 'won-dot', cx: cx(i), cy: y(p.won), r: series.length > 16 ? 3 : 4 }));
    });
    add(root, dots);
    // value bubbles above the last bars
    var labelsCount = Math.min(series.length, slot > 30 ? 4 : 3);
    var vals = svg('g', { 'aria-hidden': 'true' });
    for (var j = series.length - labelsCount; j < series.length; j++) {
      var pv = series[j];
      if (!pv || pv.leads <= 0) continue;
      var txt = num(pv.leads);
      var bwid = 10 + txt.length * 7;
      var by = Math.max(2, y(pv.leads) - 26);
      if (slot < bwid + 2 && j !== series.length - 1 && (series.length - 1 - j) % 2) continue;
      add(vals, svg('rect', { class: 'val-bg', x: cx(j) - bwid / 2, y: by, width: bwid, height: 20, rx: 10 }));
      add(vals, svg('text', { class: 'val', x: cx(j), y: by + 14, 'text-anchor': 'middle', text: txt }));
    }
    add(root, vals);
    // x labels, anchored at the last slot
    var xl = svg('g', { 'aria-hidden': 'true' });
    var lw = weekly ? 84 : 46;
    var every = Math.max(1, Math.ceil(n / Math.max(2, Math.floor(pw / lw))));
    series.forEach(function (p, i) {
      if ((n - 1 - i) % every) return;
      var t = weekly ? shortDate(p.from) + '–' + shortDate(p.to) : shortDate(p.from);
      var xx = Math.min(W - padR - (weekly ? 34 : 16), Math.max(padL + (weekly ? 30 : 14), cx(i)));
      add(xl, svg('text', { class: 'axis-x', x: xx, y: H - 8, 'text-anchor': 'middle', text: t }));
    });
    add(root, xl);
    add(box, root);

    // text alternative
    var table = h('table', null,
      h('caption', { text: 'Заявки и договоры ' + (weekly ? 'по неделям' : 'по дням') }),
      h('thead', null, h('tr', null, h('th', { scope: 'col', text: weekly ? 'Неделя' : 'Дата' }), h('th', { scope: 'col', text: 'Заявки' }), h('th', { scope: 'col', text: 'Договоры' }))),
      h('tbody', null, series.map(function (p) {
        return h('tr', null, h('th', { scope: 'row', text: weekly ? plainDate(p.from) + ' – ' + plainDate(p.to) + (p.partial ? ' (неполная неделя)' : '') : plainDate(p.from, true) }), h('td', { text: String(p.leads) }), h('td', { text: String(p.won) }));
      })));
    add(box, h('div', { class: 'sr' }, table));   // a wrapper: a table ignores width:1px and widened the page
    return { w: boxW, h: boxH };
  }

  /* ------------------------------------------------------------------ leads list */

  var LEAD_KEYS = ['status', 'q', 'mode', 'interest', 'budget', 'assignee', 'waiting', 'stale', 'sort', 'page', 'view'];
  function cleanParams(p) {
    var o = {};
    LEAD_KEYS.forEach(function (k) { if (p && p[k] !== undefined && p[k] !== null && String(p[k]) !== '') o[k] = String(p[k]); });
    if (o.status === 'all') delete o.status;
    if (o.status && o.status !== 'open' && o.status !== 'closed' && o.status !== 'progress' && !has(STATUS, o.status)) delete o.status;
    if (o.sort === 'new' || (o.sort && !has(SORTS, o.sort))) delete o.sort;
    if (o.page && !(parseInt(o.page, 10) > 1)) delete o.page;
    if (o.view !== 'grid') delete o.view;
    if (o.mode && !has(MODE, o.mode)) delete o.mode;
    if (o.interest && !has(INTEREST, o.interest)) delete o.interest;
    if (o.budget && !has(BUDGET, o.budget)) delete o.budget;
    if (o.waiting !== '1') delete o.waiting;
    if (o.stale !== '1') delete o.stale;
    return o;
  }
  function qsOf(p) {
    var c = cleanParams(p), qs = new URLSearchParams();
    LEAD_KEYS.forEach(function (k) { if (c[k]) qs.set(k, c[k]); });
    return qs.toString();
  }
  function leadsHash(p) { var s = qsOf(p); return '#/leads' + (s ? '?' + s : ''); }
  function leadHash(id, p) { var s = qsOf(p); return '#/leads/' + encodeURIComponent(id) + (s ? '?' + s : ''); }
  function dataKey(p) { var c = cleanParams(p); delete c.view; return JSON.stringify(c); }

  var teamReq = null;
  function loadTeam() {
    if (S.team) return Promise.resolve(S.team);
    if (!teamReq) {
      teamReq = api('/team').then(function (d) { S.team = d.items || []; setTeamTints(S.team); teamReq = null; return S.team; },
        function (e) { teamReq = null; throw e; });
    }
    return teamReq;
  }

  function viewLeads(root, r, token) {
    var params = cleanParams(r.params);
    var data = null, ctrl = null, popOpen = false, popBuilt = false, pagerFocus = false;

    var exportBtn = can('export') ? h('button', { class: 'btn btn--primary', type: 'button', onclick: function (e) { exportCsv(params, e.currentTarget); } }, icon('download', 'ico--sm'), 'Экспорт CSV') : null;
    var head = pageHead('Заявки', 'Все обращения с сайта: звонки, сообщения и формы', exportBtn);

    // narrow-screen search (the top bar has only an icon there)
    var ls = h('input', { class: 'search__input', type: 'search', placeholder: 'Имя, телефон, почта, № заявки', 'aria-label': 'Поиск по заявкам', autocomplete: 'off', spellcheck: 'false', enterkeyhint: 'search' });
    ls.value = params.q || '';
    var lsPush = debounce(function () { setHash(leadsHash(Object.assign({}, params, { q: ls.value.trim(), page: '' })), true); }, 250);
    ls.addEventListener('input', lsPush);
    var leadsSearch = h('div', { class: 'leads-search' }, h('div', { class: 'search', role: 'search' }, icon('search'), ls));

    var tabs = h('nav', { class: 'tabs', 'aria-label': 'Статус заявок' });
    var filterBadge = h('span', { class: 'badge', hidden: true });
    var filterBtn = h('button', { class: 'btn btn--ghost filter-btn', type: 'button', 'aria-expanded': 'false', 'aria-controls': 'filters-pop' }, icon('filter', 'ico--sm'), 'Фильтры', filterBadge);
    var pop = h('div', { class: 'pop', id: 'filters-pop', role: 'group', 'aria-label': 'Фильтры', hidden: true });
    var popWrap = h('div', { class: 'pop-wrap' }, filterBtn, pop);
    var sortSel = h('select', { class: 'select select--sm', 'aria-label': 'Сортировка' },
      Object.keys(SORTS).map(function (k) { return h('option', { value: k, text: SORTS[k] }); }));
    var vList = h('button', { type: 'button', 'aria-label': 'Списком', title: 'Списком' }, icon('list', 'ico--sm'));
    var vGrid = h('button', { type: 'button', 'aria-label': 'Плиткой', title: 'Плиткой' }, icon('grid', 'ico--sm'));
    var viewTog = h('div', { class: 'viewtog', role: 'group', 'aria-label': 'Вид' }, vList, vGrid);
    var tools = h('div', { class: 'tools' }, popWrap, sortSel, viewTog);
    var chipsBox = h('div', { class: 'active-filters', hidden: true });
    // no aria-live here: srStatus below announces the count, not 20 rows
    var results = h('div', { class: 'results', 'aria-busy': 'true', tabindex: '-1', 'aria-label': 'Список заявок' });
    var srStatus = h('p', { class: 'sr', role: 'status' });

    fill(root, head, leadsSearch, h('div', { class: 'tabs-row' }, tabs, tools), chipsBox, srStatus, results);

    sortSel.addEventListener('change', function () { go({ sort: sortSel.value, page: '' }); });
    vList.addEventListener('click', function () { go({ view: '' }, true); });
    vGrid.addEventListener('click', function () { go({ view: 'grid' }, true); });
    filterBtn.addEventListener('click', function () { popOpen ? closePop(true) : openPop(); });

    function go(patch, replace) { setHash(leadsHash(Object.assign({}, params, patch)), !!replace); }

    // tabs scroll sideways on phones: fade the right edge while more is hidden
    function syncTabsFade() { tabs.classList.toggle('tabs--more', tabs.scrollWidth - tabs.clientWidth - tabs.scrollLeft > 2); }
    tabs.addEventListener('scroll', syncTabsFade, { passive: true });
    window.addEventListener('resize', syncTabsFade);

    function onDocDown(e) { if (popOpen && !popWrap.contains(e.target)) closePop(false); }
    function onDocKey(e) { if (popOpen && e.key === 'Escape') { e.stopPropagation(); closePop(true); } }
    document.addEventListener('mousedown', onDocDown);
    document.addEventListener('keydown', onDocKey, true);
    S.cleanups.push(function () {
      document.removeEventListener('mousedown', onDocDown);
      document.removeEventListener('keydown', onDocKey, true);
      window.removeEventListener('resize', syncTabsFade);
      if (ctrl) ctrl.abort();
    });

    function openPop() {
      popOpen = true;
      pop.hidden = false;
      filterBtn.setAttribute('aria-expanded', 'true');
      if (popBuilt) syncPop(); else buildPop();
      var f = $('select', pop);
      if (f) f.focus();
    }
    function closePop(focusBtn) {
      popOpen = false;
      pop.hidden = true;
      filterBtn.setAttribute('aria-expanded', 'false');
      if (focusBtn) filterBtn.focus();
    }
    // Stable ids and a popover built once: changing a filter updates the
    // values in place, so the focused select is never replaced.
    function selectField(label, key, options) {
      var id = 'flt-' + key;
      var sel = h('select', { class: 'select', id: id, data: { key: key } },
        h('option', { value: '', text: 'Любой' }),
        options.map(function (o) { return h('option', { value: o[0], text: o[1] }); }));
      sel.value = params[key] || '';
      sel.addEventListener('change', function () { var p = {}; p[key] = sel.value; p.page = ''; go(p); });
      return h('div', { class: 'field' }, h('label', { class: 'field__label', for: id, text: label }), sel);
    }
    function assigneeOptions() {
      return [['none', 'Не назначен']].concat((S.team || []).filter(function (u) { return u.role !== 'viewer' && u.active !== false; }).map(function (u) { return [u.id, u.name]; }));
    }
    function syncPop() {
      $$('select', pop).forEach(function (sel) { var v = params[sel.dataset.key] || ''; if (sel.value !== v) sel.value = v; });
    }
    function fillAssignees() {
      var sel = $('#flt-assignee', pop);
      if (!sel) return;
      fill(sel, h('option', { value: '', text: 'Любой' }), assigneeOptions().map(function (o) { return h('option', { value: o[0], text: o[1] }); }));
      sel.value = params.assignee || '';
    }
    function buildPop() {
      popBuilt = true;
      fill(pop,
        selectField('Тип обращения', 'mode', Object.keys(MODE).map(function (k) { return [k, MODE[k]]; })),
        selectField('Интерес', 'interest', Object.keys(INTEREST).map(function (k) { return [k, INTEREST[k]]; })),
        selectField('Бюджет', 'budget', Object.keys(BUDGET).map(function (k) { return [k, BUDGET[k]]; })),
        selectField('Ответственный', 'assignee', assigneeOptions()),
        h('div', { class: 'pop__foot' },
          h('button', { class: 'btn btn--soft btn--sm', type: 'button', onclick: function () { go({ mode: '', interest: '', budget: '', assignee: '', waiting: '', stale: '', page: '' }); } }, 'Сбросить'),
          h('button', { class: 'btn btn--primary btn--sm', type: 'button', onclick: function () { closePop(true); } }, 'Готово')));
      if (!S.team) loadTeam().then(fillAssignees).catch(function () {});
    }

    function renderControls() {
      var c = data ? data.counts : null;
      var st = params.status || 'all';
      // «В работе» = contacted + proposal (status=progress), never «Новая»:
      // the tabs do not overlap and match the dashboard chip
      var defs = [['all', 'Все', c && c.all], ['new', 'Новые', c && c.new], ['progress', 'В работе', c && c.progress], ['closed', 'Закрытые', c && c.closed]];
      fill(tabs, defs.map(function (d) {
        var p = Object.assign({}, params, { status: d[0], page: '' });
        return h('a', { class: 'tab', href: leadsHash(p), 'aria-current': st === d[0] ? 'true' : null },
          d[1], d[2] !== undefined && d[2] !== null ? h('span', { class: 'tab__count', text: num(d[2]) }) : h('span', { class: 'tab__count skel-num', 'aria-hidden': 'true' }));
      }));
      syncTabsFade();
      sortSel.value = params.sort || 'new';
      vList.setAttribute('aria-pressed', String(params.view !== 'grid'));
      vGrid.setAttribute('aria-pressed', String(params.view === 'grid'));
      var nf = ['mode', 'interest', 'budget', 'assignee', 'waiting', 'stale'].filter(function (k) { return params[k]; }).length;
      filterBadge.textContent = String(nf);
      filterBadge.hidden = !nf;
      if (popBuilt) syncPop();
      if (document.activeElement !== ls) ls.value = params.q || '';
      // active filter chips
      var chips = [];
      function chip(text, patch) {
        chips.push(h('span', { class: 'fchip' }, h('span', { class: 'fchip__text', text: text }),
          h('button', { type: 'button', 'aria-label': 'Убрать фильтр: ' + text, onclick: function () { go(Object.assign({ page: '' }, patch)); } }, icon('close', 'ico--xs'))));
      }
      if (params.status && has(STATUS, params.status)) chip('Статус: ' + STATUS[params.status], { status: '' });
      if (params.status === 'open') chip('Открытые: новые и в работе', { status: '' });   // from «Требует внимания», not a tab
      if (params.q) chip('Поиск: ' + params.q, { q: '' });
      if (params.mode) chip(MODE[params.mode], { mode: '' });
      if (params.interest) chip('Интерес: ' + INTEREST[params.interest], { interest: '' });
      if (params.budget) chip('Бюджет: ' + BUDGET[params.budget], { budget: '' });
      if (params.assignee) {
        var u = params.assignee === 'none' ? null : (S.team || []).filter(function (x) { return x.id === params.assignee; })[0];
        chip(params.assignee === 'none' ? 'Без ответственного' : 'Ответственный: ' + (u ? u.name : '…'), { assignee: '' });
      }
      if (params.waiting) chip('Без ответа больше часа', { waiting: '' });
      if (params.stale) chip('Без движения больше 3' + NB + 'дней', { stale: '' });
      fill(chipsBox, chips);
      chipsBox.hidden = !chips.length;
      if (params.assignee && params.assignee !== 'none' && !S.team) loadTeam().then(renderControls).catch(function () {});
    }

    function skeleton() {
      var grid = params.view === 'grid';
      if (grid) return h('div', { class: 'cards' }, [0, 1, 2, 3, 4, 5].map(function () {
        return h('div', { class: 'lcard' }, h('div', { class: 'skel skel--line' }), h('div', { class: 'skel', vars: { height: '64px' } }), h('div', { class: 'skel skel--line' }));
      }));
      return h('div', { class: 'list' }, [0, 1, 2, 3, 4, 5, 6].map(function (i) {
        var row = h('div', { class: 'skel-row' }, h('div', { class: 'skel skel--line' }), h('div', { class: 'skel skel--line' }), h('div', { class: 'skel skel--line' }));
        row.firstChild.style.setProperty('width', (24 + (i % 3) * 6) + '%');
        row.children[1].style.setProperty('width', '12%');
        row.children[2].style.setProperty('width', '18%');
        return row;
      }));
    }

    function load(quiet) {
      if (ctrl) ctrl.abort();
      ctrl = new AbortController();
      var my = ctrl;
      var page = parseInt(params.page, 10) || 1;
      var qs = new URLSearchParams();
      ['status', 'q', 'mode', 'interest', 'budget', 'assignee', 'waiting', 'stale', 'sort'].forEach(function (k) { if (params[k]) qs.set(k, params[k]); });
      qs.set('limit', String(PAGE_SIZE));
      qs.set('offset', String((page - 1) * PAGE_SIZE));
      if (!quiet) { fill(results, skeleton()); results.setAttribute('aria-busy', 'true'); }
      return Promise.all([
        api('/leads?' + qs, { signal: my.signal }),
        loadTeam().catch(function () { return null; })   // avatar tints
      ]).then(function (res) {
        var d = res[0];
        if (!alive(token) || my !== ctrl) return;
        data = d;
        results.removeAttribute('aria-busy');
        if (d.total && d.offset >= d.total) { go({ page: String(Math.ceil(d.total / PAGE_SIZE)) }, true); return; }
        renderControls();
        renderResults(quiet);
        srStatus.textContent = d.total ? 'Найдено: ' + num(d.total) + ' ' + leadsWord(d.total) : 'Ничего не найдено';
        if (pagerFocus) {
          // the pager was re-rendered: keep keyboard users on it and bring the list into view
          pagerFocus = false;
          var curPg = $('.pg[aria-current="page"]', results);
          results.scrollIntoView({ block: 'start', behavior: reduceMotion.matches ? 'auto' : 'smooth' });
          if (curPg) curPg.focus({ preventScroll: true }); else results.focus({ preventScroll: true });
        }
      }, function (e) {
        if (!alive(token) || my !== ctrl || e.code === 'abort' || e.code === 'auth') return;
        results.removeAttribute('aria-busy');
        pagerFocus = false;
        if (quiet) { toast(errText(e), 'alert', 'error'); return; }
        fill(results, errorCard(e, function () { load(); }));
      });
    }

    function renderResults(quiet) {
      var d = data;
      if (!d.items || !d.items.length) {
        var anyFilter = ['status', 'q', 'mode', 'interest', 'budget', 'assignee', 'waiting', 'stale'].some(function (k) { return params[k]; });
        if (!anyFilter && !d.counts.all) {
          fill(results, h('div', { class: 'empty fade' },
            h('span', { class: 'empty__art' }, icon('inbox')),
            h('h2', { class: 'empty__title', text: 'Заявок пока нет' }),
            h('p', { class: 'empty__text', text: 'Как только кто-то оставит заявку на сайте, она появится здесь.' })));
        } else {
          var onlyQ = !!params.q && !['status', 'mode', 'interest', 'budget', 'assignee', 'waiting', 'stale'].some(function (k) { return params[k]; });
          fill(results, h('div', { class: 'empty fade' },
            h('span', { class: 'empty__art' }, icon('search')),
            h('h2', { class: 'empty__title', text: 'Ничего не нашлось' }),
            h('p', { class: 'empty__text', text: onlyQ ? 'По запросу «' + params.q + '» заявок нет. Проверьте написание или ищите по телефону, почте, номеру заявки.' : 'Попробуйте изменить запрос или снять часть фильтров.' }),
            h('a', { class: 'btn btn--primary', href: onlyQ ? leadsHash(Object.assign({}, params, { q: '', page: '' })) : '#/leads' + (params.view === 'grid' ? '?view=grid' : '') }, onlyQ ? 'Очистить поиск' : 'Сбросить фильтры')));
        }
        return;
      }
      var grid = params.view === 'grid';
      var cur = S.drawer && S.drawer.id;
      var box = h('ul', { class: grid ? 'cards' : 'list' });
      d.items.forEach(function (it, i) {
        var el = grid ? leadCard(it, params) : leadRow(it, params);
        if (it.id === cur) el.classList.add('is-current');
        if (!quiet) rise(el, Math.min(i, 12));
        add(box, h('li', { class: 'li' }, el));
      });
      var page = parseInt(params.page, 10) || 1;
      var pages = Math.max(1, Math.ceil(d.total / PAGE_SIZE));
      var from = d.offset + 1, to = d.offset + d.items.length;
      fill(results, box, h('nav', { class: 'pager', 'aria-label': 'Страницы' },
        h('p', { class: 'pager__info', text: 'Показано ' + num(from) + '–' + num(to) + ' из ' + num(d.total) }),
        pages > 1 ? pager(page, pages) : null));
    }

    function pager(page, pages) {
      var nums = h('div', { class: 'pager__nums' });
      function btn(p, label, ico) {
        var el = h('button', { class: 'pg', type: 'button', 'aria-label': label || 'Страница ' + p, disabled: p < 1 || p > pages ? true : null },
          ico ? icon(ico, 'ico--sm') : String(p));
        if (!ico && p === page) el.setAttribute('aria-current', 'page');
        el.addEventListener('click', function () { if (p !== page) { pagerFocus = true; go({ page: String(p) }); } });
        return el;
      }
      add(nums, btn(page - 1, 'Предыдущая страница', 'chevron-left'));
      var set = {};
      [1, pages, page - 1, page, page + 1].forEach(function (p) { if (p >= 1 && p <= pages) set[p] = 1; });
      if (page <= 3) [2, 3, 4].forEach(function (p) { if (p <= pages) set[p] = 1; });
      if (page >= pages - 2) [pages - 1, pages - 2, pages - 3].forEach(function (p) { if (p >= 1) set[p] = 1; });
      var list = Object.keys(set).map(Number).sort(function (a, c) { return a - c; });
      list.forEach(function (p, i) {
        if (i && p - list[i - 1] > 1) add(nums, h('span', { class: 'pg pg--gap', 'aria-hidden': 'true', text: '…' }));
        add(nums, btn(p));
      });
      add(nums, btn(page + 1, 'Следующая страница', 'chevron-right'));
      return nums;
    }

    var lastKey = null;
    function update(p) {
      var next = cleanParams(p);
      var key = dataKey(next);
      var viewChanged = (next.view || '') !== (params.view || '');
      params = next;
      if (key !== lastKey) { lastKey = key; renderControls(); load(); }
      else if (viewChanged && data) { renderControls(); renderResults(); }
      else if (data) markCurrent();
    }
    function markCurrent() {
      var cur = S.drawer && S.drawer.id;
      $$('[data-id]', results).forEach(function (el) { el.classList.toggle('is-current', el.dataset.id === cur); });
    }

    S.leads = { update: update, refresh: function () { return load(true); }, params: function () { return params; }, markCurrent: markCurrent };
    renderControls();
    lastKey = dataKey(params);
    load();
  }

  function leadName(it) { return it.erased ? 'Данные удалены' : (it.name || 'Без имени'); }
  function leadMeta(it) {
    var parts = [lab(MODE, it.mode), lab(SOURCE, it.source, '')].filter(Boolean);
    if (it.notes) parts.push(it.notes + ' ' + plural(it.notes, ['заметка', 'заметки', 'заметок']));
    return h('span', { class: 'row__meta' },
      h('span', { class: 'mdot mdot--' + (has(MODE, it.mode) ? it.mode : 'call'), 'aria-hidden': 'true' }),
      h('span', { class: 'row__meta-text', text: parts.join(' · ') }));
  }
  function statusPill(st) { return h('span', { class: 'pill st--' + (has(STATUS, st) ? st : 'lost'), text: lab(STATUS, st, st || '—') }); }
  function cell(cls, k, v, muted) {
    return h('div', { class: 'cell ' + cls }, h('span', { class: 'cell__k', text: k }), h('span', { class: 'cell__v' + (muted ? ' cell__v--muted' : ''), text: v }));
  }
  function whoCell(cls, a) {
    return h('div', { class: 'cell ' + cls }, h('span', { class: 'cell__k', text: 'Ответственный' }),
      a ? h('span', { class: 'who-cell' }, avatar(a, 'sm'), h('span', { class: 'cell__v', text: a.name, title: a.name })) : h('span', { class: 'cell__v cell__v--muted', text: '—' }));
  }
  function leadRow(it, params) {
    return h('a', { class: 'row', href: leadHash(it.id, params), data: { id: it.id } },
      h('div', { class: 'row__main' }, h('span', { class: 'row__name' + (it.erased ? ' row__name--erased' : ''), text: leadName(it) }), leadMeta(it)),
      h('div', { class: 'row__status' }, statusPill(it.status)),
      cell('cell--contact', lab(KIND, it.contactKind, 'Контакт'), it.erased ? '—' : (it.contact || '—'), it.erased || !it.contact),
      cell('cell--interest', 'Интересует', lab(INTEREST, it.interest), !it.interest),
      cell('cell--budget', 'Бюджет', lab(BUDGET, it.budget), !it.budget),
      whoCell('cell--who', it.assignee),
      cell('cell--time', 'Получена', listTime(it.time)),
      h('span', { class: 'row__go', 'aria-hidden': 'true' }, icon('chevron-right', 'ico--sm')));
  }
  function leadCard(it, params) {
    // fixed slots, so labels line up from card to card: contact | interest, assignee | budget
    return h('a', { class: 'lcard', href: leadHash(it.id, params), data: { id: it.id } },
      h('div', { class: 'lcard__head' },
        h('div', { class: 'lcard__title' }, h('span', { class: 'row__name' + (it.erased ? ' row__name--erased' : ''), text: leadName(it) }), leadMeta(it)),
        statusPill(it.status)),
      h('div', { class: 'lcard__pairs' },
        cell('', lab(KIND, it.contactKind, 'Контакт'), it.erased ? '—' : (it.contact || '—'), it.erased || !it.contact),
        cell('', 'Интересует', lab(INTEREST, it.interest), !it.interest),
        whoCell('', it.assignee),
        cell('', 'Бюджет', lab(BUDGET, it.budget), !it.budget)),
      it.preview && !it.erased ? h('p', { class: 'lcard__preview', text: it.preview }) : null,
      h('div', { class: 'lcard__foot' }, h('span', { text: 'Получена ' + listTime(it.time) }), icon('arrow-up-right', 'ico--sm')));
  }

  /* ------------------------------------------------------------------ layers (modal stack) */

  function pushLayer(layer) {
    S.layers.forEach(function (l) { l.el.inert = true; if (l.overlay) l.overlay.inert = true; });
    S.layers.push(layer);
    if (E.shell) { E.sidebar.inert = true; E.mainCol.inert = true; }
    document.body.classList.add('is-locked');
  }
  function popLayer(layer) {
    var i = S.layers.indexOf(layer);
    if (i >= 0) S.layers.splice(i, 1);
    var top = S.layers[S.layers.length - 1];
    if (top) { top.el.inert = false; if (top.overlay) top.overlay.inert = false; }
    if (!S.layers.length) {
      if (E.shell) { E.mainCol.inert = !!E.navOpen; syncSidebarInert(); }
      if (!E.navOpen) document.body.classList.remove('is-locked');
    }
  }
  function closeAllLayers() {
    S.layers.slice().reverse().forEach(function (l) { if (l.close) l.close(true); });
    S.layers = [];
    clear(E.layers);
    S.drawer = null;
    document.body.classList.remove('has-drawer');
  }
  function leave(el, cb) {
    if (!el) { if (cb) cb(); return; }
    if (reduceMotion.matches) { el.remove(); if (cb) cb(); return; }
    el.classList.add('is-leaving');
    var done = false;
    var fin = function () { if (done) return; done = true; el.remove(); if (cb) cb(); };
    el.addEventListener('animationend', fin, { once: true });
    setTimeout(fin, 320);
  }

  function onGlobalKey(e) {
    if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.key === 'k' || e.key === 'K' || e.key === 'л' || e.key === 'Л')) {
      if (!S.user || !E.shell) return;
      e.preventDefault();
      if (S.palette) closePalette(); else openPalette('');
      return;
    }
    var top = S.layers[S.layers.length - 1];
    if (e.key === 'Escape') {
      if (top) { e.preventDefault(); top.onEsc(); return; }
      if (E.navOpen) { e.preventDefault(); closeMobileNav(); return; }
    }
    if (e.key === 'Tab') {
      if (top) trapTab(e, top.el);
      else if (E.navOpen) trapTab(e, E.sidebar);
    }
  }

  /* ------------------------------------------------------------------ toasts */

  function toast(text, ico, kind) {
    var el = h('div', { class: 'toast' + (kind ? ' toast--' + kind : '') }, icon(ico || (kind === 'error' ? 'alert' : 'check-circle')), h('span', { text: text }));
    E.toasts.appendChild(el);
    while (E.toasts.children.length > 3) E.toasts.removeChild(E.toasts.firstChild);
    setTimeout(function () { leave(el); }, kind === 'error' ? 6000 : 3500);
  }

  /* ------------------------------------------------------------------ confirm dialog */

  function confirmDialog(opt) {
    return new Promise(function (resolve) {
      var tid = nextId('dlg'), did = nextId('dlgd');
      var prevFocus = document.activeElement;
      // above the drawer: its own scrim dims the drawer too
      var overlay = h('div', { class: 'overlay overlay--top' });
      var cancel = h('button', { class: 'btn btn--soft', type: 'button', text: opt.cancel || 'Отмена' });
      var ok = h('button', { class: 'btn ' + (opt.danger ? 'btn--danger-solid' : 'btn--primary'), type: 'button', text: opt.ok || 'Подтвердить' });
      var el = h('div', { class: 'dialog', role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': tid, 'aria-describedby': did },
        opt.danger ? h('span', { class: 'dialog__ico' }, icon('trash')) : null,
        h('h2', { class: 'dialog__title', id: tid, text: opt.title }),
        h('p', { class: 'dialog__text', id: did, text: opt.text }),
        h('div', { class: 'dialog__foot' }, cancel, ok));
      var layer = { el: el, overlay: overlay };
      function close(result) {
        popLayer(layer);
        leave(overlay); el.remove();
        if (prevFocus && prevFocus.isConnected) prevFocus.focus();
        resolve(!!result);
      }
      layer.onEsc = function () { close(false); };
      layer.close = function () { close(false); };
      cancel.addEventListener('click', function () { close(false); });
      ok.addEventListener('click', function () { close(true); });
      overlay.addEventListener('click', function () { close(false); });
      pushLayer(layer);
      E.layers.appendChild(overlay);
      E.layers.appendChild(el);
      cancel.focus();
    });
  }

  /* ------------------------------------------------------------------ lead drawer */

  function openLead(id) {
    if (S.drawer && S.drawer.id === id) return;
    var opener = document.activeElement;
    var reuse = S.drawer;
    if (!reuse) {
      var overlay = h('div', { class: 'overlay' });
      var el = h('div', { class: 'drawer', role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': 'dr-title' });
      var layer = { el: el, overlay: overlay };
      layer.onEsc = function () { requestCloseDrawer(); };
      layer.close = function () { closeDrawer(true); };
      overlay.addEventListener('click', requestCloseDrawer);
      S.drawer = { id: id, el: el, overlay: overlay, layer: layer, opener: opener && opener !== document.body ? opener : null, token: 0 };
      document.body.classList.add('has-drawer');
      pushLayer(layer);
      E.layers.appendChild(overlay);
      E.layers.appendChild(el);
    } else {
      S.drawer.id = id;
    }
    if (S.leads) S.leads.markCurrent();
    loadLead(true);
  }
  function requestCloseDrawer() {
    var p = S.leads ? S.leads.params() : cleanParams(S.route ? S.route.params : {});
    setHash(leadsHash(p));
  }
  function closeDrawer(silent) {
    var d = S.drawer;
    if (!d) return;
    S.drawer = null;
    document.body.classList.remove('has-drawer');
    popLayer(d.layer);
    leave(d.overlay);
    leave(d.el);
    if (S.leads) S.leads.markCurrent();
    // return focus to the row that opened the drawer (or its replacement after a refresh)
    var target = null;
    $$('[data-id]', E.main).forEach(function (el) { if (el.dataset.id === d.id) target = el; });
    if (!target && d.opener && d.opener.isConnected) target = d.opener;
    if (target && !silent) target.focus({ preventScroll: false });
    else if (target) setTimeout(function () { if (!S.layers.length && target.isConnected) target.focus({ preventScroll: true }); }, 0);
  }

  function drawerSkeleton() {
    return [
      h('div', { class: 'drawer__head' },
        h('div', { class: 'drawer__top' }, h('h2', { class: 'drawer__title', id: 'dr-title' }, h('span', { class: 'sr', text: 'Загрузка заявки' }), h('span', { class: 'skel skel--num', vars: { width: '220px' } })), closeBtn()),
        h('div', { class: 'skel skel--line', vars: { width: '60%' } })),
      h('div', { class: 'drawer__body' }, [0, 1, 2].map(function () {
        return h('div', { class: 'dsec' }, h('div', { class: 'skel skel--line', vars: { width: '30%' } }), h('div', { class: 'skel', vars: { height: '56px' } }));
      }))
    ];
  }
  function closeBtn() {
    return h('button', { class: 'icon-btn icon-btn--tile', type: 'button', 'aria-label': 'Закрыть', 'data-close': '', onclick: requestCloseDrawer }, icon('close'));
  }

  function loadLead(fresh) {
    var d = S.drawer;
    if (!d) return;
    d.token += 1;
    var my = d.token, id = d.id;
    if (fresh) { fill(d.el, drawerSkeleton()); $('[data-close]', d.el).focus(); }
    return api('/leads/' + encodeURIComponent(id)).then(function (res) {
      if (!S.drawer || S.drawer.token !== my) return;
      S.drawer.data = res;
      renderLead(res, fresh);
    }, function (e) {
      if (!S.drawer || S.drawer.token !== my || e.code === 'auth') return;
      fill(d.el,
        h('div', { class: 'drawer__head' }, h('div', { class: 'drawer__top' }, h('h2', { class: 'drawer__title', id: 'dr-title', text: e.code === 'not_found' ? 'Заявка не найдена' : 'Не удалось открыть заявку' }), closeBtn())),
        h('div', { class: 'drawer__body' }, h('div', { class: 'dsec' },
          h('p', { class: 'muted', text: e.code === 'not_found' ? 'Такой заявки нет в этом рабочем пространстве. Возможно, ссылка устарела.' : errText(e) }),
          e.code !== 'not_found' ? h('div', null, h('button', { class: 'btn btn--primary btn--sm', type: 'button', onclick: function () { loadLead(true); } }, icon('refresh', 'ico--sm'), 'Повторить')) : null)));
      $('[data-close]', d.el).focus();
    });
  }

  function section(title, kids, extra, cls) {
    var id = nextId('sec');
    return h('section', { class: 'dsec ' + (cls || ''), 'aria-labelledby': id },
      h('div', { class: 'dsec__head' }, h('h3', { class: 'label', id: id, text: title }), extra || null), kids);
  }

  function renderLead(res, fresh) {
    var d = S.drawer;
    var L = res.lead || {};
    var hist = res.history || [];
    var editable = can('edit');
    var keepFocus = document.activeElement && d.el.contains(document.activeElement) ? (document.activeElement.getAttribute('data-focus-key') || null) : null;
    var scroll = $('.drawer__body', d.el) ? $('.drawer__body', d.el).scrollTop : 0;

    var title = h('h2', { class: 'drawer__title' + (L.erased ? ' drawer__title--erased' : ''), id: 'dr-title', text: leadName(L) });
    var head = h('div', { class: 'drawer__head' },
      h('div', { class: 'drawer__top' }, title, closeBtn()),
      h('div', { class: 'drawer__meta' },
        statusPill(L.status),
        h('span', { class: 'drawer__id', text: '№' + NB + (L.id || '') + ' · получена ' + fullTime(L.time) })));

    var body = h('div', { class: 'drawer__body' });

    // contact
    if (L.erased) {
      add(body, h('div', { class: 'erased-banner' }, icon('shield'),
        h('div', null, h('p', null, b('Персональные данные удалены.')), h('p', { text: 'Имя, контакты и сообщение стёрты по запросу. История статусов сохранена.' }))));
    } else {
      var links = contactLinks(L);
      var contactValue = L.contact || L.phone || '';
      var actions = h('div', { class: 'actions' });
      links.forEach(function (l, i) {
        add(actions, h('a', { class: 'btn btn--sm ' + (i === 0 ? 'btn--accent' : 'btn--soft'), href: l.href, target: l.ext ? '_blank' : null, rel: l.ext ? 'noopener noreferrer' : null }, icon(l.icon, 'ico--sm'), l.label));
      });
      if (contactValue) add(actions, h('button', { class: 'btn btn--sm btn--soft', type: 'button', onclick: function () { copyText(contactValue); } }, icon('copy', 'ico--sm'), 'Скопировать'));
      var kindLabel = lab(KIND, L.contactKind, 'Контакт');
      if (L.mode === 'call' && L.when) kindLabel += ' · перезвонить ' + lab(WHEN, L.when, '');
      if (L.mode === 'write' && L.channel) kindLabel = 'Ответить в ' + lab(CHANNEL, L.channel, L.channel);
      add(body, section('Контакт', [
        h('div', { class: 'contact-line' },
          h('span', { class: 'contact-line__ico' }, icon(KIND_ICON[L.contactKind] || 'user')),
          h('div', { class: 'contact-line__text' },
            h('span', { class: 'contact-line__value', text: contactValue || '—' }),
            h('span', { class: 'contact-line__kind', text: kindLabel }))),
        actions.children.length ? actions : null]));
    }

    // details
    // only the fields this lead has; the empty ones are named once at the end
    function item(k, v, cls) { return h('div', { class: 'details__item' + (cls ? ' ' + cls : '') }, h('dt', { class: 'details__k', text: k }), h('dd', { class: 'details__v', text: v })); }
    var missing = [];
    // a value is shown whenever it is there; a gap is named only for a field this form asks
    // (the server drops budget for call/write and interest for form leads)
    function opt(k, miss, asked, map, v) { if (has(map, v)) return item(k, map[v]); if (asked) missing.push(miss); return null; }
    var detailItems = [
      item('Тип обращения', lab(MODE, L.mode)),
      item('Источник', lab(SOURCE, L.source)),
      opt('Когда позвонить', 'время звонка', L.mode === 'call', WHEN, L.when),
      opt('Интересует', 'интерес', L.mode !== 'form', INTEREST, L.interest),
      opt('Бюджет', 'бюджет', L.mode === 'form', BUDGET, L.budget),
      item('Согласие на обработку данных', L.consent ? 'дано' : 'не дано')
    ].filter(Boolean);
    if (missing.length) detailItems.push(item('Не указано', missing.join(', '), 'details__item--muted'));
    add(body, section('Детали', h('dl', { class: 'details' }, detailItems)));

    // message
    if (!L.erased && L.message) add(body, section('Сообщение', h('p', { class: 'msg', text: L.message })));

    // work: status, assignee, note
    var work = [];
    var fs = h('fieldset', { class: 'statuses', role: 'radiogroup', 'aria-label': 'Статус' }, h('legend', { class: 'sr', text: 'Статус' }));
    var pointerAt = 0, stTimer = 0;
    fs.addEventListener('pointerdown', function () { pointerAt = Date.now(); });
    STATUS_KEYS.forEach(function (k) {
      var inp = h('input', { type: 'radio', name: 'lead-status', value: k, 'data-focus-key': 'st-' + k, disabled: !editable ? true : null });
      inp.checked = L.status === k;
      inp.addEventListener('change', function () {
        if (!inp.checked) return;
        clearTimeout(stTimer);
        var fire = function () { if (S.drawer && S.drawer.data && S.drawer.data.lead.status !== k) patchLead({ status: k }, 'st-' + k, 'Статус: ' + STATUS[k]); };
        // arrow keys walk through the group: wait until the choice settles
        if (Date.now() - pointerAt < 800) fire(); else stTimer = setTimeout(fire, 700);
      });
      add(fs, h('label', { class: 'stopt stopt--' + k }, inp, h('span', { text: STATUS[k] })));
    });
    work.push(h('div', { class: 'field' }, h('span', { class: 'field__label', 'aria-hidden': 'true', text: 'Статус' }), fs));

    var asSel = h('select', { class: 'select', id: 'lead-assignee', 'data-focus-key': 'assignee', disabled: !editable ? true : null },
      h('option', { value: '', text: 'Не назначен' }));
    if (L.assignee) add(asSel, h('option', { value: L.assignee.id, text: L.assignee.name }));
    asSel.value = L.assignee ? L.assignee.id : '';
    loadTeam().then(function (team) {
      var cur = asSel.value;
      fill(asSel, h('option', { value: '', text: 'Не назначен' }));
      var seen = {};
      team.filter(function (u) { return u.active !== false && u.role !== 'viewer'; }).forEach(function (u) { seen[u.id] = 1; add(asSel, h('option', { value: u.id, text: u.name })); });
      if (L.assignee && !seen[L.assignee.id]) add(asSel, h('option', { value: L.assignee.id, text: L.assignee.name }));
      asSel.value = cur;
    }).catch(function () {});
    asSel.addEventListener('change', function () {
      var opt = asSel.options[asSel.selectedIndex];
      patchLead({ assignee: asSel.value }, 'assignee', asSel.value ? 'Ответственный: ' + (opt ? opt.textContent : '') : 'Ответственный снят');
    });
    work.push(h('div', { class: 'field' }, h('label', { class: 'field__label', for: 'lead-assignee', text: 'Ответственный' }), asSel));

    if (editable) {
      var ta = h('textarea', { class: 'textarea', id: 'lead-note', rows: '3', maxlength: '2000', placeholder: 'Например: перезвонить в пятницу после обеда', 'data-focus-key': 'note' });
      var addBtn = h('button', { class: 'btn btn--primary btn--sm', type: 'button', disabled: true }, icon('plus', 'ico--sm'), 'Добавить');
      var sync = function () { addBtn.disabled = !ta.value.trim() || addBtn.classList.contains('is-busy'); };
      ta.addEventListener('input', sync);
      var submit = function () {
        var text = ta.value.trim();
        if (!text || addBtn.disabled) return;
        addBtn.disabled = true; addBtn.classList.add('is-busy'); ta.readOnly = true;
        patchLead({ note: text }, 'note', 'Заметка добавлена').then(function (ok) {
          addBtn.classList.remove('is-busy'); ta.readOnly = false;
          if (!ok) sync();
        });
      };
      ta.addEventListener('keydown', function (e) { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); submit(); } });
      addBtn.addEventListener('click', submit);
      work.push(h('div', { class: 'field composer' },
        h('label', { class: 'field__label', for: 'lead-note', text: 'Заметка для команды' }), ta,
        h('div', { class: 'composer__foot' }, h('span', { class: 'composer__hint', text: (isMac ? '⌘' : 'Ctrl') + ' + Enter\u00A0— добавить' }), addBtn)));
    }
    add(body, section('Работа с заявкой', work, editable ? null : h('span', { class: 'ro-hint' }, icon('eye', 'ico--xs'), 'Только просмотр')));

    // history
    var tl = h('ol', { class: 'timeline' });
    hist.forEach(function (ev) { add(tl, timelineItem(ev)); });
    if (!hist.length) add(tl, h('li', { class: 'muted small', text: 'Событий пока нет' }));
    add(body, section('История', tl, h('span', { class: 'muted small', text: hist.length + ' ' + plural(hist.length, ['событие', 'события', 'событий']) })));

    // danger zone
    if (can('erase') && !L.erased) {
      add(body, section('Персональные данные', [
        h('p', { text: 'По запросу человека (152\u2011ФЗ) можно стереть его имя, телефон, контакты, сообщение и тексты заметок. Статусы и история останутся.' }),
        h('div', null, h('button', { class: 'btn btn--danger btn--sm', type: 'button', 'data-focus-key': 'erase', onclick: eraseLead }, icon('trash', 'ico--sm'), 'Удалить персональные данные'))
      ], null, 'danger'));
    }

    fill(d.el, head, body);
    body.scrollTop = fresh ? 0 : scroll;
    if (fresh) {
      var cb = $('[data-close]', d.el);
      if (cb) cb.focus();
    } else if (keepFocus) {
      var again = $$('[data-focus-key]', d.el).filter(function (x) { return x.getAttribute('data-focus-key') === keepFocus; })[0];
      if (again && !again.disabled) again.focus({ preventScroll: true });
      else { var c2 = $('[data-close]', d.el); if (c2) c2.focus({ preventScroll: true }); }
    }
  }

  function timelineItem(ev) {
    var kind = ev.kind;
    var dotIcon = { created: 'sparkle', status: 'arrow-right', note: 'note', assign: 'user', erase: 'shield' }[kind] || 'dot';
    var text, extra = null;
    switch (kind) {
      case 'created': text = ['Заявка получена с сайта']; break;
      case 'status': text = ['Статус: ', lab(STATUS, ev.from, ev.from || '—'), ' → ', b(lab(STATUS, ev.to, ev.to || '—'))]; break;
      case 'note':
        text = ['Заметка'];
        extra = ev.text ? h('p', { class: 'tl__note', text: ev.text }) : h('p', { class: 'tl__note tl__note--erased', text: 'Текст заметки удалён' });
        break;
      case 'assign': text = ev.assignee ? ['Ответственный: ', b(ev.assignee.name)] : ['Ответственный снят']; break;
      case 'erase': text = ['Персональные данные удалены']; break;
      default: text = ['Изменение'];
    }
    var who = ev.by ? ev.by.name : (kind === 'created' ? 'Посетитель сайта' : '');
    return h('li', { class: 'tl' },
      h('span', { class: 'tl__dot tl__dot--' + kind, 'aria-hidden': 'true' }, icon(dotIcon)),
      h('div', { class: 'tl__body' },
        h('p', { class: 'tl__text' }, text),
        extra,
        h('p', { class: 'tl__meta' }, who ? who + ' · ' : '', h('time', { datetime: ev.time || '', text: listTime(ev.time), title: fullTime(ev.time) }))));
  }

  function patchLead(body, focusKey, okText) {
    var d = S.drawer;
    if (!d) return Promise.resolve(false);
    var id = d.id;
    var controls = $$('input[name="lead-status"], #lead-assignee', d.el);
    controls.forEach(function (c) { c.disabled = true; });
    return api('/leads/' + encodeURIComponent(id), { method: 'PATCH', body: body }).then(function (res) {
      if (S.drawer && S.drawer.id === id) {
        S.drawer.data = res;
        var active = document.activeElement;
        renderLead(res, false);
        var target = $$('[data-focus-key]', S.drawer.el).filter(function (x) { return x.getAttribute('data-focus-key') === focusKey; })[0];
        if (target && (!active || !S.drawer.el.contains(active) || active === document.body || !active.isConnected)) target.focus({ preventScroll: true });
        else if (target) target.focus({ preventScroll: true });
      }
      toast(okText, body.note ? 'note' : 'check-circle', 'ok');
      if (S.leads) S.leads.refresh();
      loadSession(true).catch(function () {});
      return true;
    }, function (e) {
      if (e.code === 'auth') return false;
      toast(errText(e), 'alert', 'error');
      if (S.drawer && S.drawer.id === id && S.drawer.data) renderLead(S.drawer.data, false);
      return false;
    });
  }

  function eraseLead() {
    var d = S.drawer;
    if (!d) return;
    var id = d.id;
    confirmDialog({
      title: 'Удалить персональные данные?',
      text: 'Имя, телефон, контакты, сообщение и тексты заметок будут стёрты без возможности восстановления. История статусов останется.',
      ok: 'Удалить данные', danger: true
    }).then(function (yes) {
      if (!yes || !S.drawer || S.drawer.id !== id) return;
      return api('/leads/' + encodeURIComponent(id), { method: 'DELETE' }).then(function () {
        toast('Персональные данные удалены', 'shield', 'ok');
        loadLead(false);
        if (S.leads) S.leads.refresh();
      }, function (e) { if (e.code !== 'auth') toast(errText(e), 'alert', 'error'); });
    });
  }

  function copyText(text) {
    var ok = function () { toast('Скопировано', 'copy', 'ok'); };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(ok, function () { fallbackCopy(text) ? ok() : toast('Не удалось скопировать', 'alert', 'error'); });
    } else if (fallbackCopy(text)) ok();
    else toast('Не удалось скопировать', 'alert', 'error');
  }
  function fallbackCopy(text) {
    var ta = h('textarea', { class: 'sr', readonly: true, 'aria-hidden': 'true' });
    ta.value = text;
    document.body.appendChild(ta);
    var prev = document.activeElement;
    ta.select();
    var ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    ta.remove();
    if (prev && prev.focus) prev.focus({ preventScroll: true });
    return ok;
  }

  /* ------------------------------------------------------------------ search palette */

  function openPalette(initial, returnTo) {
    if (S.palette || !E.shell) return;
    var prevFocus = returnTo || document.activeElement;
    var overlay = h('div', { class: 'overlay' });
    var listId = 'pal-list';
    var input = h('input', { class: 'palette__input', type: 'search', placeholder: 'Имя, телефон, почта или № заявки', autocomplete: 'off', spellcheck: 'false',
      role: 'combobox', 'aria-expanded': 'true', 'aria-controls': listId, 'aria-autocomplete': 'list', 'aria-label': 'Поиск по заявкам', enterkeyhint: 'go' });
    input.value = initial || '';
    var body = h('div', { class: 'palette__body', id: listId, role: 'listbox', 'aria-label': 'Результаты' });
    var foot = h('div', { class: 'palette__foot', 'aria-hidden': 'true' },
      h('span', null, h('span', { class: 'kbd', text: '↑' }), h('span', { class: 'kbd', text: '↓' }), 'выбор'),
      h('span', null, h('span', { class: 'kbd', text: 'Enter' }), 'открыть'),
      h('span', null, h('span', { class: 'kbd', text: 'Esc' }), 'закрыть'));
    var el = h('div', { class: 'palette', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Поиск' },
      h('div', { class: 'palette__bar' }, icon('search'), input,
        h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm', type: 'button', 'aria-label': 'Закрыть поиск', onclick: function () { closePalette(); } }, icon('close', 'ico--sm'))),
      body, foot);
    var layer = { el: el, overlay: overlay };
    var options = [], active = -1, ctrl = null, seq = 0;

    function setActive(i, scroll) {
      foot.hidden = !options.length;   // no ↑↓ / Enter hints when there is nothing to pick
      if (!options.length) { active = -1; input.removeAttribute('aria-activedescendant'); return; }
      active = (i + options.length) % options.length;
      options.forEach(function (o, k) {
        o.el.classList.toggle('is-active', k === active);
        o.el.setAttribute('aria-selected', String(k === active));
      });
      input.setAttribute('aria-activedescendant', options[active].el.id);
      if (scroll !== false) options[active].el.scrollIntoView({ block: 'nearest' });
    }
    function option(el, go) {
      el.id = nextId('pal-opt');
      el.setAttribute('role', 'option');
      el.setAttribute('aria-selected', 'false');
      var idx = options.length;
      el.addEventListener('mousemove', function () { if (active !== idx) setActive(idx, false); });
      el.addEventListener('click', function () { go(); });
      options.push({ el: el, go: go });
      return el;
    }
    function nav(hash) { return function () { closePalette(true); setHash(hash); }; }

    function quick() {
      options = [];
      var n = S.counts.new || 0;
      var tiles = [
        ['sparkle', 'Новые заявки', n ? n + ' ' + plural(n, ['ждёт', 'ждут', 'ждут']) + ' ответа' : 'Новых нет', '#/leads?status=new'],
        ['inbox', 'Все заявки', 'Список с фильтрами', '#/leads'],
        ['users', 'Команда', 'Участники и роли', '#/team'],
        ['settings', 'Настройки', 'Пространство и данные', '#/settings']
      ];
      fill(body,
        h('div', { class: 'palette__group', role: 'presentation' }, h('span', { text: 'Быстрый переход' })),
        h('div', { class: 'palette__tiles', role: 'presentation' }, tiles.map(function (t) {
          return option(h('div', { class: 'ptile' },
            h('span', { class: 'ptile__ico' }, icon(t[0], 'ico--sm')),
            h('span', { class: 'ptile__text' }, h('span', { class: 'ptile__title', text: t[1] }), h('span', { class: 'ptile__hint', text: t[2] })),
            icon('arrow-right', 'ico--sm')), nav(t[3]));
        })));
      setActive(0, false);
    }
    var search = debounce(function () {
      var q = input.value.trim();
      if (!q) { quick(); return; }
      if (ctrl) ctrl.abort();
      ctrl = new AbortController();
      var my = ++seq;
      api('/leads?q=' + encodeURIComponent(q) + '&limit=8', { signal: ctrl.signal }).then(function (d) {
        if (my !== seq || !S.palette) return;
        options = [];
        var items = d.items || [];
        var allHash = leadsHash({ q: q });
        if (!items.length) {
          fill(body, h('div', { class: 'palette__empty', role: 'presentation' },
            h('span', { class: 'palette__empty-ico' }, icon('search')),
            h('p', { class: 'palette__empty-title' }, 'Ничего не нашлось по запросу «', q, '»'),
            h('p', { class: 'palette__empty-hint', text: 'Ищите по имени, телефону, почте или номеру заявки' })));
          setActive(-1);
          return;
        }
        var ql = q.toLowerCase();
        fill(body,
          h('div', { class: 'palette__group', role: 'presentation' }, h('span', { text: 'Заявки' }),
            // a mouse shortcut; the same action is the last option of the list
            h('a', { class: 'palette__group-all', href: allHash, tabindex: '-1', 'aria-hidden': 'true' }, 'Все ' + num(d.total) + ' ', icon('arrow-right', 'ico--xs'))),
          items.map(function (it) {
            var sub = [it.erased ? '' : it.contact, lab(MODE, it.mode, ''), listTime(it.time)].filter(Boolean).join(' · ');
            // found by the message text: show that bit, so the row explains itself
            var inName = String(it.name || '').toLowerCase().indexOf(ql) >= 0 || String(it.contact || '').toLowerCase().indexOf(ql) >= 0 || String(it.id || '').toLowerCase().indexOf(ql) >= 0;
            var pv = String(it.preview || ''), at = pv.toLowerCase().indexOf(ql);
            if (!it.erased && !inName && at >= 0) {
              var from = Math.max(0, at - 24);
              if (from) { var sp = pv.lastIndexOf(' ', from); from = sp >= 0 && at - sp < 40 ? sp + 1 : from; }   // start on a word
              sub = '«' + (from ? '…' : '') + pv.slice(from, at + ql.length + 40).trim() + (at + ql.length + 40 < pv.length ? '…' : '') + '»';
            }
            return option(h('div', { class: 'presult' },
              it.erased ? h('span', { class: 'av av--visitor', 'aria-hidden': 'true' }, icon('shield', 'ico--sm')) : avatar({ id: it.id, name: it.name }),
              h('span', { class: 'presult__text' }, h('span', { class: 'presult__name', text: leadName(it) }), h('span', { class: 'presult__sub', text: sub })),
              statusPill(it.status)), nav(leadHash(it.id, { q: q })));
          }),
          option(h('div', { class: 'palette__all' }, h('span', { text: 'Показать все результаты (' + num(d.total) + ')' }), icon('arrow-right', 'ico--sm')), nav(allHash)));
        setActive(0, false);
      }, function (e) {
        if (my !== seq || !S.palette || e.code === 'abort' || e.code === 'auth') return;
        options = [];
        fill(body, h('p', { class: 'palette__empty', text: errText(e) }));
        setActive(-1);
      });
    }, 150);

    input.addEventListener('input', function () {
      if (input.value.trim()) {
        options = [];
        fill(body, h('div', { class: 'palette__group', role: 'presentation' }, h('span', { text: 'Ищем…' })),
          [0, 1, 2].map(function () { return h('div', { class: 'presult', role: 'presentation' }, h('span', { class: 'av skel' }), h('span', { class: 'presult__text' }, h('span', { class: 'skel skel--line' }))); }));
      }
      search();
    });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setActive(active + 1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setActive(active - 1); }
      else if (e.key === 'Enter') {
        e.preventDefault();
        if (options[active]) options[active].go();
        else if (input.value.trim()) nav(leadsHash({ q: input.value.trim() }))();
      }
    });
    overlay.addEventListener('click', function () { closePalette(); });

    layer.onEsc = function () { closePalette(); };
    layer.close = function () { closePalette(true); };
    S.palette = { layer: layer, el: el, overlay: overlay, prevFocus: prevFocus, abort: function () { if (ctrl) ctrl.abort(); search.cancel(); } };
    pushLayer(layer);
    E.layers.appendChild(overlay);
    E.layers.appendChild(el);
    if (input.value.trim()) { input.dispatchEvent(new Event('input')); } else quick();
    input.focus();
  }
  function closePalette(noFocus) {
    var p = S.palette;
    if (!p) return;
    S.palette = null;
    p.abort();
    popLayer(p.layer);
    leave(p.overlay);
    p.el.remove();
    if (!noFocus && p.prevFocus && p.prevFocus.isConnected) p.prevFocus.focus({ preventScroll: true });
  }

  /* ------------------------------------------------------------------ team */

  function seenText(u) {
    if (!u.lastSeen) return { on: false, text: verb(u.name, 'ещё не заходил', 'ещё не заходила') };
    var d = toDate(u.lastSeen);
    if (!d) return { on: false, text: '' };
    if (Date.now() - d.getTime() < 5 * 60000) return { on: true, text: 'в сети' };
    return { on: false, text: verb(u.name, 'был', 'была') + ' в сети ' + relTime(u.lastSeen) };
  }

  function viewTeam(root, r, token) {
    var list = h('div', { class: 'team', 'aria-busy': 'true' }, [0, 1, 2, 3].map(function () {
      return h('div', { class: 'card member' }, h('div', { class: 'member__head' }, h('span', { class: 'av av--xl skel' }), h('div', { class: 'member__text' }, h('div', { class: 'skel skel--line' }), h('div', { class: 'skel skel--line', vars: { width: '60%' } })))) ;
    }));
    // the real syntax of admincli.go; the password is generated and printed once
    var wsId = (S.ws && S.ws.id) || 'ID';
    var cmd = 'axiomantic-leads admin user add -config admin.json -workspace ' + wsId + ' -login ЛОГИН -name "Имя Фамилия" -role manager';
    var cli = h('div', { class: 'cli' },
      h('span', { class: 'cli__ico', 'aria-hidden': 'true' }, icon('terminal')),
      h('div', { class: 'cli__text' },
        h('p', { text: 'Новых участников добавляет владелец через консоль сервера. Пароль генерируется и показывается один раз; роль\u00A0— owner, manager или viewer.' }),
        h('div', { class: 'code' }, h('code', { class: 'code__text', text: cmd }),
          h('button', { class: 'btn btn--sm btn--ghost', type: 'button', onclick: function () { copyText(cmd); } }, icon('copy', 'ico--sm'), 'Скопировать'))));
    fill(root, pageHead('Команда', 'Кто работает с заявками и с какими правами'), list, cli);

    var load = function () {
      api('/team').then(function (d) {
        if (!alive(token)) return;
        S.team = d.items || [];
        setTeamTints(S.team);
        list.removeAttribute('aria-busy');
        if (!S.team.length) { fill(list, h('p', { class: 'muted', text: 'В команде пока никого нет.' })); return; }
        fill(list, S.team.map(function (u, i) {
          var seen = seenText(u);
          var me = S.user && u.id === S.user.id;
          return rise(h('article', { class: 'card member' + (me ? ' member--me' : '') },
            h('div', { class: 'member__head' }, avatar(u, 'xl'),
              h('div', { class: 'member__text' },
                h('h2', { class: 'member__name', text: u.name, title: u.name }),
                h('p', { class: 'member__login', text: u.login || '', title: u.login || null }))),
            // the role sits in the foot, so the login line gets the full width
            h('div', { class: 'member__foot' },
              h('span', { class: 'online' + (seen.on ? ' online--on' : ''), text: u.active === false ? 'Доступ отключён' : seen.text }),
              h('span', { class: 'member__pills' },
                me ? h('span', { class: 'pill pill--plain st--new', text: 'Это вы' }) : null,
                h('span', { class: 'pill pill--plain role--' + (has(ROLE, u.role) ? u.role : 'viewer'), text: lab(ROLE, u.role, u.role) })))), i);
        }));
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        list.removeAttribute('aria-busy');
        fill(list, errorCard(e, load));
      });
    };
    load();
  }

  /* ------------------------------------------------------------------ settings */

  function viewSettings(root, r, token) {
    var grid = h('div', { class: 'settings', 'aria-busy': 'true' }, [0, 1, 2, 3].map(function () {
      return h('div', { class: 'card' }, h('div', { class: 'skel skel--line', vars: { width: '40%' } }), h('div', { class: 'skel', vars: { height: '96px', 'margin-top': '20px' } }));
    }));
    var version = h('p', { class: 'version' });
    fill(root, pageHead('Настройки', 'Рабочее пространство, уведомления и данные'), grid, version);

    function setCard(ico, title, kids, i) {
      return rise(h('section', { class: 'card' },
        h('div', { class: 'set-head' }, h('span', { class: 'set-head__ico', 'aria-hidden': 'true' }, icon(ico)), h('h2', { class: 'set-head__title', text: title })),
        kids), i);
    }
    function kv(k, v) { return h('div', { class: 'kv__row' }, h('dt', { class: 'kv__k', text: k }), h('dd', { class: 'kv__v' }, v)); }

    var load = function () {
      api('/settings').then(function (d) {
        if (!alive(token)) return;
        grid.removeAttribute('aria-busy');
        var ws = d.workspace || S.ws || {};
        var accent = ws.brand && /^#[0-9a-fA-F]{6}$/.test(ws.brand.accent || '') ? ws.brand.accent : null;
        var origins = Array.isArray(d.origins) ? d.origins : [];
        var tg = !!(d.notifications && d.notifications.telegram);
        var zone = validTz(ws.tz) ? ws.tz : tz();
        var nowLocal = fmt({ hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }, zone).format(new Date());
        // two columns that stack independently: no gaps between unequal cards
        var colA = h('div', { class: 'settings__col' }), colB = h('div', { class: 'settings__col' });
        fill(grid, colA, colB);
        add(colA,
          setCard('globe', 'Рабочее пространство', h('dl', { class: 'kv' },
            kv('Название', ws.name || '—'),
            kv('Фирменный цвет', accent ? [h('span', { class: 'swatch', 'aria-hidden': 'true' }), h('span', { class: 'num', text: accent.toUpperCase() })] : '—'),
            kv('Часовой пояс', [h('span', { text: zone }), h('span', { class: 'muted', text: 'сейчас ' + nowLocal })]),
            kv('Сайты', origins.length ? origins.map(function (o) { return h('span', { class: 'origin', text: o }); }) : '—')), 0));
        add(colB,
          setCard('bell', 'Уведомления', [
            h('dl', { class: 'kv' }, kv('Telegram', h('span', { class: 'state ' + (tg ? 'state--on' : 'state--off'), text: tg ? 'подключены' : 'не настроены' }))),
            h('div', { class: 'set-text' }, h('p', { text: tg
              ? 'Каждая новая заявка приходит в рабочий чат. Ключ бота хранится только на сервере и в браузер не передаётся.'
              : 'Чтобы получать заявки в Telegram, владелец указывает чат в конфигурации сервера. Ключ бота в браузер не передаётся.' }))], 1));
        add(colA,
          setCard('database', 'Данные', [
            h('div', { class: 'set-text' },
              h('p', { text: 'Заявки хранятся в файле на сервере, который может читать только сервис админки. В браузере ничего не сохраняется.' }),
              h('p', { text: 'По запросу человека (152\u2011ФЗ) его персональные данные удаляются из карточки заявки: откройте заявку → «Удалить персональные данные». Удаление необратимо, история статусов остаётся.' })),
            can('export') ? h('div', { class: 'set-actions' }, h('button', { class: 'btn btn--primary', type: 'button', onclick: function (e) { exportCsv({}, e.currentTarget); } }, icon('download', 'ico--sm'), 'Экспорт CSV')) : null], 2));
        add(colB,
          setCard('key', 'Сеанс', [
            h('dl', { class: 'kv' },
              kv('Пользователь', S.user.name),
              kv('Логин', S.user.login || '—'),
              kv('Роль', h('span', { class: 'pill pill--plain role--' + (has(ROLE, S.user.role) ? S.user.role : 'viewer'), text: lab(ROLE, S.user.role, '') }))),
            h('div', { class: 'set-actions' }, h('button', { class: 'btn btn--ghost', type: 'button', onclick: logout }, icon('logout', 'ico--sm'), 'Выйти'))], 3));
        version.textContent = 'Версия ' + (d.version || '—');
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        grid.removeAttribute('aria-busy');
        fill(grid, errorCard(e, load));
      });
    };
    load();
  }

  /* ------------------------------------------------------------------ not found */

  function viewMissing(root) {
    fill(root, h('div', { class: 'empty' },
      h('span', { class: 'empty__art' }, icon('alert')),
      h('h1', { class: 'empty__title', text: 'Такой страницы нет' }),
      h('p', { class: 'empty__text', text: 'Проверьте адрес или вернитесь на дашборд.' }),
      h('a', { class: 'btn btn--primary', href: '#/' }, 'На дашборд')));
  }
})();
