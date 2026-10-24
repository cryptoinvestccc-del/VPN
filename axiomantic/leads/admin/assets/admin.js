/* Axiomantic admin — site editor and leads panel. Vanilla JS, no build step.
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
    if (arguments.length > 2) { for (var i = 1; i < arguments.length; i++) add(el, arguments[i]); return; }
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
    if (opt.form) init.body = opt.form;   // multipart: the browser sets the boundary
    else if (opt.body !== undefined) { headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(opt.body); }
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
        err.data = data;
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
      case 'invalid': return (e.data && typeof e.data.message === 'string' && e.data.message) ? 'Сервер не принял значение: ' + e.data.message + '.' : 'Сервер не принял значение' + (e.field ? ' поля «' + e.field + '»' : '') + '.';
      case 'self': return 'Себя нельзя понизить, отключить или удалить.';
      case 'duplicate': return 'Такой логин уже есть.';
      case 'busy': return 'Сервер занят\u00A0— попробуйте через минуту.';
      case 'no_site': return 'У этого рабочего пространства нет сайта.';
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
    var f = data.features || {};
    S.features = { site: f.site === true, leads: f.leads !== false };
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
    if (action === 'edit' || action === 'export' || action === 'site') return r === 'owner' || r === 'manager';
    if (action === 'erase' || action === 'restore' || action === 'team') return r === 'owner';
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
    // closing the tab with unsaved edits in a section asks the browser's own question
    window.addEventListener('beforeunload', function (e) { if (S.editor && S.user && S.editor.dirty()) { e.preventDefault(); e.returnValue = ''; } });
    start();
  }
  function start() {
    fill(E.app, h('div', { class: 'boot', role: 'status' }, h('span', { class: 'boot__tile', 'aria-hidden': 'true' }), h('span', { class: 'sr', text: 'Загрузка…' })));
    loadSession().then(enterShell, function (e) {
      if (e.code === 'auth' || e.status === 401) renderLogin();
      else renderFatal(e);
    });
  }
  // The site (schema + draft) is loaded before the shell: the sidebar lists
  // the schema's sections. A failed load still opens the panel; the pages
  // that need the site show an error card with «Повторить».
  function enterShell() {
    var site = S.features.site ? loadSite().catch(function (e) { if (e.code !== 'auth') S.siteErr = e; }) : Promise.resolve();
    return site.then(function () {
      if (!S.user) return;
      mountShell();
      onRoute();
      startPolling();
    });
  }
  function startPolling() {
    clearInterval(S.poll);
    S.poll = setInterval(function () {
      if (document.hidden || !S.user) return;
      var before = S.counts.new || 0;
      loadSession(true).then(function () {
        var now = S.counts.new || 0;
        if (now > before && S.features.leads) toast((now - before === 1 ? 'Новая заявка' : 'Новых заявок: ' + (now - before)), 'sparkle');
      }).catch(function () {});
      // someone else may have saved or published: refresh the publish bar
      // (an open editor keeps its own version and merges on save)
      if (S.features.site && !S.publishing) loadSite().catch(function () {});
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
    S.view = null; S.leads = null; S.team = null; S.site = null; S.siteErr = null; S.editor = null;
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
      }).then(enterShell).catch(function (err) {
        submit.disabled = false; submit.classList.remove('is-busy'); submit.removeAttribute('aria-busy');
        if (err.code === 'credentials') { setError('Неверный логин или пароль'); pass.select(); pass.focus(); }
        else if (err.code === 'rate') {
          var m = Math.max(1, Math.ceil((parseInt(err.retry, 10) || 60) / 60));
          setError('Слишком много попыток\u00A0— подождите ' + m + NB + 'мин');
        } else setError(errText(err));
      });
    });

    var marqueeWords = ['Сайт', 'Заявки', 'Публикации', 'Команда'];
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
        h('p', { class: 'login__kicker', text: 'Панель управления сайтом' }),
        h('div', { class: 'login__copy' },
          h('p', { class: 'login__headline' }, h('span', { text: 'Весь сайт' }), h('span', { class: 'login__grad', text: 'в одном месте' })),
          h('p', { class: 'login__lead', text: 'Тексты, цены, кейсы и статьи\u00A0— правки в черновик, предпросмотр и публикация в один клик. Заявки с сайта и команда\u00A0— там же.' })),
        h('div', { class: 'mini' },
          h('div', { class: 'mini__row' }, h('span', { class: 'label', text: 'Заявки за неделю' }), h('span', { class: 'round-go round-go--dark' }, icon('arrow-up-right', 'ico--sm'))),
          h('div', { class: 'mini__row' }, h('span', { class: 'mini__num', text: '48' }), h('span', { class: 'delta' }, icon('arrow-up'), '24%')),
          h('div', { class: 'mini__bars' }, miniBars),
          h('div', { class: 'mini__foot' }, h('span', { class: 'pill st--new', text: 'Новые · 6' }), h('span', { class: 'pill st--won', text: 'Договор · 3' }))),
        h('div', { class: 'marquee' }, h('div', { class: 'marquee__track' }, marqueeSet(false), marqueeSet(true)))),
      h('main', { class: 'login__side', id: 'main', tabindex: '-1' },
        h('div', { class: 'login__card' },
          h('div', { class: 'brand' }, h('span', { class: 'brand__tile', 'aria-hidden': 'true', text: DEFAULT_BRAND.logo }), h('span', { class: 'brand__name', text: DEFAULT_BRAND.name })),
          h('div', null, h('h1', { class: 'login__title', text: 'Вход в админку' }), h('p', { class: 'login__sub', text: 'Сайт, заявки и команда.' })),
          form,
          h('p', { class: 'login__foot' }, icon('shield', 'ico--sm'), 'Доступ выдаёт владелец рабочего' + NB + 'пространства')))));
    setTimeout(function () { login.focus(); }, 0);
    // a live region only announces changes made after it is in the page
    if (notice) setTimeout(function () { if (errBox.isConnected && !errBox.firstChild) add(errBox, [icon('alert', 'ico--sm'), h('span', { text: notice })]); }, 60);
  }

  function logout() {
    if (S.editor && S.editor.dirty()) {
      confirmDialog({ title: 'Выйти без сохранения?', text: 'В разделе «' + S.editor.title + '» есть несохранённые правки. Если выйти, они пропадут.', ok: 'Выйти', cancel: 'Остаться', danger: true, icon: 'logout' })
        .then(function (yes) { if (yes) { S.editor.discard(); logout(); } });
      return;
    }
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

    // the leads link and its badge always exist (updateCounts writes to them);
    // they are put in the menu only when the workspace has leads
    E.badge = h('span', { class: 'badge num', 'aria-hidden': 'true' });
    E.navLeads = navLink('#/leads', 'inbox', 'Заявки', 'leads');
    add(E.navLeads, E.badge);
    E.navLeadsSr = h('span', { class: 'sr' });
    add(E.navLeads, E.navLeadsSr);
    var site = safeHttpUrl(S.ws && S.ws.siteURL);

    E.collapseBtn = h('button', { class: 'nav__link collapse-btn', type: 'button', 'aria-pressed': String(collapsed), title: collapsed ? 'Развернуть меню' : 'Свернуть меню', onclick: toggleCollapse },
      icon('sidebar'), h('span', { class: 'nav__text', text: collapsed ? 'Развернуть меню' : 'Свернуть меню' }));
    E.sideClose = h('button', { class: 'icon-btn icon-btn--flat side-close', type: 'button', 'aria-label': 'Закрыть меню', onclick: closeMobileNav }, icon('close'));

    // «Найти настройку…»: filters the menu by section title, place, hint and field labels
    E.sideSearch = h('input', { class: 'side-search__input', type: 'search', placeholder: 'Найти настройку…', 'aria-label': 'Найти настройку', autocomplete: 'off', spellcheck: 'false', enterkeyhint: 'go', 'aria-controls': 'side-nav' });
    E.sideSearch.addEventListener('input', function () { filterSideNav(E.sideSearch.value); });
    E.sideSearch.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && E.sideSearch.value) { e.stopPropagation(); e.preventDefault(); E.sideSearch.value = ''; filterSideNav(''); }
      if (e.key === 'Enter') {
        var first = $$('.nav__link', E.navBox).filter(function (a) { return !a.closest('[hidden]'); })[0];
        if (first) { e.preventDefault(); first.click(); }
      }
      if (e.key === 'ArrowDown') {
        var f = $$('.nav__link', E.navBox).filter(function (a) { return !a.closest('[hidden]'); })[0];
        if (f) { e.preventDefault(); f.focus(); }
      }
    });
    E.navBox = h('nav', { class: 'nav', id: 'side-nav', 'aria-label': 'Разделы' });
    var syncMore = function () { E.navBox.classList.toggle('nav--more', E.navBox.scrollHeight - E.navBox.clientHeight - E.navBox.scrollTop > 4); };
    E.navBox.addEventListener('scroll', syncMore, { passive: true });
    window.addEventListener('resize', syncMore);
    E.syncNavMore = syncMore;

    E.sidebar = h('aside', { class: 'sidebar', id: 'sidebar', 'aria-label': 'Меню' },
      h('div', { class: 'brand' },
        h('span', { class: 'brand__tile', 'aria-hidden': 'true', text: wsLogo() }),
        h('span', { class: 'brand__name', text: wsName() }),
        E.sideClose),
      h('div', { class: 'side-search', role: 'search' }, icon('search', 'ico--sm'), E.sideSearch),
      E.navBox,
      h('div', { class: 'side-foot' },
        h('div', { class: 'side-row' },
          site ? h('a', { class: 'nav__link', href: site, target: '_blank', rel: 'noopener noreferrer' }, icon('external'), h('span', { class: 'nav__text', text: 'Открыть сайт ↗' }), h('span', { class: 'sr', text: ' (в новой вкладке)' })) : null,
          E.collapseBtn),
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

    // the publish bar: always there when the workspace has a site, hidden for viewers
    E.pub = null;
    if (S.features.site && can('site')) {
      E.pubState = h('a', { class: 'pub__state', href: '#/' }, h('span', { class: 'pub__dot', 'aria-hidden': 'true' }), h('span', { class: 'pub__text' }));
      E.pubPreview = h('button', { class: 'btn btn--ghost pub__btn', type: 'button', onclick: function () { previewSite(E.pubPreview); } }, icon('eye', 'ico--sm'), h('span', { class: 'pub__label', text: 'Предпросмотр' }));
      E.pubPublish = h('button', { class: 'btn btn--primary pub__btn', type: 'button', onclick: function () { openPublish(E.pubPublish); } }, icon('rocket', 'ico--sm'), h('span', { class: 'pub__label', text: 'Опубликовать' }));
      E.pub = h('div', { class: 'pub', role: 'group', 'aria-label': 'Публикация сайта' }, E.pubState, h('span', { class: 'pub__btns' }, E.pubPreview, E.pubPublish));
    }

    var leadsOn = S.features.leads;
    E.topbar = h('header', { class: 'topbar' + (E.pub ? ' topbar--pub' : '') },
      E.burger,
      leadsOn ? E.searchBox : null,
      leadsOn ? h('button', { class: 'icon-btn search-icon-btn', type: 'button', 'aria-label': 'Поиск по заявкам', onclick: function () { openPalette(''); } }, icon('search')) : null,
      h('span', { class: 'topbar__spacer' }),
      E.pub,
      E.themeBtn,
      leadsOn ? E.bell : null,
      h('div', { class: 'who' }, avatar(S.user),
        h('div', { class: 'who__text' }, h('span', { class: 'who__name', text: S.user.name }), h('span', { class: 'who__role', text: lab(ROLE, S.user.role, '') }))));

    E.main = h('main', { class: 'view', id: 'main', tabindex: '-1' });
    E.mainCol = h('div', { class: 'main-col' }, h('div', { class: 'main-inner' }, E.topbar, E.main));
    E.shell = h('div', { class: 'shell' + (collapsed ? ' is-collapsed' : '') },
      h('div', { class: 'haze', 'aria-hidden': 'true' }), E.sidebar, E.scrim, E.mainCol);
    fill(E.app, E.shell);
    E.firstRender = true;
    document.title = wsName();
    renderSideNav();
    renderPubBar();
    syncSidebarInert();
    updateCounts();
    loadTeam().catch(function () {});   // team tints for every avatar on the page
  }

  /* sidebar menu: «Обзор», «Заявки», the schema's groups of sections, «Доступ» */
  var ACCESS_ITEMS = [
    ['#/team', 'users', 'Пользователи', 'team', 'команда роли доступ участники логин пароль владелец менеджер наблюдатель'],
    ['#/indexing', 'compass', 'Индексация', 'indexing', 'яндекс поиск indexnow вебмастер страницы роботы', true],
    ['#/health', 'pulse', 'Проверка сайта', 'health', 'ошибки ссылки сборка картинки поиск seo проверить', true],
    ['#/media', 'image', 'Медиатека', 'media', 'картинки фото изображения загрузки файлы удалить', true],
    ['#/activity', 'clock', 'Журнал действий', 'activity', 'история кто что изменил правки события', true],
    ['#/releases', 'history', 'Резервные копии', 'releases', 'версии публикации история вернуть откат восстановить', true],
    ['#/password', 'lock', 'Сменить пароль', 'password', 'пароль безопасность вход']
  ];
  function norm(s) { return String(s || '').toLowerCase().replace(/ё/g, 'е'); }
  function fieldWords(f, out) {
    if (!f) return out;
    if (f.label) out.push(f.label);
    if (f.hint) out.push(f.hint);
    (f.items || []).forEach(function (x) { fieldWords(x, out); });
    (f.fields || []).forEach(function (x) { fieldWords(x, out); });
    fieldWords(f.item, out);
    return out;
  }
  // «Поиск по сайту»: the sidebar search also finds a section by what it says
  function sectionTexts(sec) {
    var out = [];
    var c = S.site && S.site.content;
    if (!c) return out;
    function walk(v) {
      if (typeof v === 'string') { if (v && !reMediaId.test(v)) out.push(v); }
      else if (Array.isArray(v)) v.forEach(walk);
      else if (v && typeof v === 'object') Object.keys(v).forEach(function (k) { walk(v[k]); });
    }
    sec.fields.forEach(function (f) { walk(getPathOf(c, f.key)); });
    return out;
  }
  function getPathOf(c, key) {
    var i = key.indexOf('.');
    if (i < 0) return c[key];
    var top = c[key.slice(0, i)];
    return top && typeof top === 'object' ? top[key.slice(i + 1)] : undefined;
  }
  function renderSideNav() {
    if (!E.navBox) return;
    var groups = [];
    function link(href, ico, text, key, words) {
      var a = navLink(href, ico, text, key);
      a.dataset.find = norm(text + ' ' + (words || ''));
      return a;
    }
    var top = [link('#/', 'home', 'Обзор', 'dash', 'главная сводка статус публикация')];
    if (S.features.leads) { E.navLeads.dataset.find = norm('Заявки аналитика обращения лиды'); top.push(E.navLeads); }
    groups.push({ items: top });
    var sch = S.site && S.site.schema;
    if (S.features.site && sch) {
      var changed = {};
      (S.site.changed || []).forEach(function (id) { changed[id] = 1; });
      (sch.groups || []).forEach(function (g) {
        var secs = (sch.sections || []).filter(function (x) { return x.group === g.id; });
        if (!secs.length) return;
        groups.push({ title: g.title, items: secs.map(function (sec) {
          var a = link('#/edit/' + encodeURIComponent(sec.id), secIcon(sec.icon), sec.title, 'edit:' + sec.id,
            [sec.where, sec.hint].concat(sec.fields.reduce(function (o, f) { return fieldWords(f, o); }, []), sectionTexts(sec)).join(' '));
          a.setAttribute('title', sec.title);
          if (changed[sec.id] && S.site.current) {
            add(a, h('span', { class: 'nav__dot', title: 'Есть неопубликованные правки' }), h('span', { class: 'sr', text: ', есть неопубликованные правки' }));
          }
          return a;
        }) });
      });
    } else if (S.features.site) {
      var retry = h('button', { class: 'nav__link nav__retry', type: 'button', onclick: function () {
        retry.disabled = true;
        loadSite().then(function () { S.siteErr = null; renderSideNav(); renderPubBar(); onRoute(); }, function () { retry.disabled = false; toast('Не удалось загрузить разделы сайта', 'alert', 'error'); });
      } }, icon('refresh'), h('span', { class: 'nav__text', text: 'Загрузить разделы сайта' }));
      groups.push({ title: 'Сайт', items: [retry] });
    }
    groups.push({ title: 'Доступ', items: ACCESS_ITEMS.filter(function (x) { return !x[5] || S.features.site; }).map(function (x) { return link(x[0], x[1], x[2], x[3], x[4]); }) });
    var kids = groups.map(function (g, i) {
      var id = g.title ? 'navg-' + i : null;
      return h('div', { class: 'nav__group', role: g.title ? 'group' : null, 'aria-labelledby': id },
        g.title ? h('p', { class: 'nav__section', id: id, text: g.title }) : null,
        g.items);
    });
    E.sideEmpty = h('p', { class: 'nav__empty', hidden: true, role: 'status' });
    fill(E.navBox, kids, E.sideEmpty);
    filterSideNav(E.sideSearch ? E.sideSearch.value : '');
    syncNavCurrent();
    if (E.syncNavMore) requestAnimationFrame(E.syncNavMore);
  }
  function filterSideNav(q) {
    if (!E.navBox) return;
    var words = norm(q).trim().split(/\s+/).filter(Boolean);
    var any = false;
    $$('.nav__group', E.navBox).forEach(function (g) {
      var vis = 0;
      $$('.nav__link', g).forEach(function (a) {
        var ok = !words.length || words.every(function (w) { return (a.dataset.find || '').indexOf(w) >= 0; });
        a.hidden = !ok;
        if (ok) vis++;
      });
      g.hidden = !vis;
      if (vis) any = true;
    });
    if (E.sideEmpty) {
      E.sideEmpty.hidden = any;
      E.sideEmpty.textContent = any ? '' : 'Ничего не нашлось';
    }
    E.sidebar && E.sidebar.classList.toggle('is-filtering', !!words.length);
    if (E.syncNavMore) E.syncNavMore();
  }
  function navKey(r) {
    if (!r) return '';
    if (r.name === 'leads' || r.name === 'analytics') return 'leads';
    if (r.name === 'edit') return 'edit:' + r.section;
    return r.name;
  }
  function syncNavCurrent() {
    if (!E.sidebar) return;
    var key = navKey(S.route);
    $$('.nav__link[data-nav]', E.sidebar).forEach(function (a) {
      if (a.dataset.nav === key) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
    });
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
    E.collapseBtn.setAttribute('title', on ? 'Развернуть меню' : 'Свернуть меню');
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
    else if (parts.length === 1 && has(SIMPLE_ROUTES, parts[0])) r.name = parts[0];
    else if (parts[0] === 'edit' && parts.length === 2) {
      r.name = 'edit';
      try { r.section = decodeURIComponent(parts[1]); } catch (e) { r.section = parts[1]; }
    }
    else r.name = 'missing';
    // the leads module and the site module answer only when the workspace has them
    var siteOnly = { edit: 1, releases: 1, health: 1 }, leadsOnly = { leads: 1, analytics: 1 };
    if ((siteOnly[r.name] && !S.features.site) || (leadsOnly[r.name] && !S.features.leads)) r.name = 'missing';
    return r;
  }
  function setHash(hash, replace) {
    if (location.hash === hash || (hash === '#/' && !location.hash)) { if (replace) onRoute(); return; }
    if (replace) { history.replaceState(null, '', hash); onRoute(); }
    else location.hash = hash;
  }

  var SIMPLE_ROUTES = { team: 1, settings: 1, analytics: 1, releases: 1, health: 1, password: 1, media: 1, activity: 1, indexing: 1 };
  var VIEW_TITLE = { dash: 'Обзор', leads: 'Заявки', analytics: 'Аналитика заявок', team: 'Пользователи', settings: 'Рабочее пространство',
    releases: 'Резервные копии', health: 'Проверка сайта', password: 'Сменить пароль', edit: 'Раздел', missing: 'Не найдено',
    media: 'Медиатека', activity: 'Журнал действий', indexing: 'Индексация' };

  function onRoute() {
    if (!S.user || !E.shell) return;
    var r = parseHash();
    if (!r) return;
    // leaving a section with unsaved edits asks first
    var ed = S.editor;
    if (ed && ed.dirty() && !S.leaving && !(r.name === 'edit' && r.section === ed.id)) {
      var target = location.hash || '#/';
      history.replaceState(null, '', ed.hash);
      closeMobileNav(true);
      confirmDialog({ title: 'Уйти без сохранения?', text: 'В разделе «' + ed.title + '» есть несохранённые правки. Если уйти, они пропадут.', ok: 'Уйти без сохранения', cancel: 'Остаться', danger: true, icon: 'alert' })
        .then(function (yes) { if (yes) { S.leaving = true; if (location.hash === target) onRoute(); else location.hash = target; } });
      return;
    }
    S.leaving = false;
    var prev = S.route;
    S.route = r;
    closeMobileNav(true);
    closePalette(true);
    syncTopSearch();
    syncNavCurrent();
    var sec = r.name === 'edit' ? findSection(r.section) : null;
    document.title = (r.lead ? 'Заявка' : sec ? sec.title : VIEW_TITLE[r.name]) + ' · ' + wsName();

    if (r.name !== 'leads' && S.drawer) closeDrawer(true);

    var same = prev && prev.name === r.name && S.view === r.name;
    if (r.name === 'dash') {
      if (S.features.site) { if (!same) render(viewOverview, r); }
      else if (same && S.dash) S.dash.setDays(daysParam(r.params));
      else render(viewDashboard, r);
    } else if (r.name === 'analytics') {
      if (same && S.dash) S.dash.setDays(daysParam(r.params));
      else render(viewDashboard, r);
    } else if (r.name === 'edit') {
      if (!(same && S.editor && S.editor.id === r.section)) render(viewEditor, r);
    } else if (r.name === 'releases') { if (!same) render(viewReleases, r); }
    else if (r.name === 'health') { if (!same) render(viewHealth, r); }
    else if (r.name === 'media') { if (!same) render(viewMedia, r); }
    else if (r.name === 'activity') { if (!same) render(viewActivity, r); }
    else if (r.name === 'indexing') { if (!same) render(viewIndexing, r); }
    else if (r.name === 'password') { if (!same) render(viewPassword, r); }
    else if (r.name === 'leads') {
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
    S.editor = null;
    S.onSite = null;
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

  function pageHead(title, sub, actions, below) {
    return h('div', { class: 'page-head' + (below ? ' page-head--tabs' : '') },
      h('div', { class: 'page-head__text' }, h('h1', { class: 'page-title', text: title }), sub ? h('p', { class: 'page-sub', text: sub }) : null),
      actions ? h('div', { class: 'page-actions' }, actions) : null,
      below || null);
  }
  // «Заявки | Аналитика»: two views of the leads module
  function leadsTabs(active) {
    function tab(href, key, text, ico) {
      return h('a', { class: 'ptab', href: href, 'aria-current': active === key ? 'page' : null }, icon(ico, 'ico--sm'), text);
    }
    return h('nav', { class: 'ptabs', 'aria-label': 'Заявки' }, tab('#/leads', 'leads', 'Заявки', 'inbox'), tab('#/analytics', 'analytics', 'Аналитика', 'dashboard'));
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
    // «Заявки → Аналитика» when the workspace has a site; the home page otherwise
    var analytics = r.name === 'analytics';
    var base = analytics ? '#/analytics' : '#/';
    var seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Период' });
    [7, 30, 90].forEach(function (d) {
      add(seg, h('button', { class: 'seg__opt', type: 'button', 'aria-pressed': String(d === days), data: { days: String(d) }, text: d + NB + 'дней',
        onclick: function () { setHash(d === 30 ? base : base + '?days=' + d); } }));
    });
    var head = analytics
      ? pageHead('Заявки', 'Что приходит с сайта и как быстро команда отвечает', seg, leadsTabs('analytics'))
      : pageHead('С возвращением, ' + firstName(S.user.name) + '!', 'Вот что происходит с заявками', seg);

    var site = safeHttpUrl(S.ws && S.ws.siteURL);
    var newBadge = h('span', { class: 'badge num', 'data-new-count': '', text: String(S.counts.new || 0) });
    newBadge.hidden = !S.counts.new;
    var tiles = [
      h('a', { class: 'qa__tile qa__tile--dark', href: '#/leads?status=new' }, icon('sparkle'), h('span', { class: 'qa__label', text: 'Разобрать новые' }), newBadge),
      h('a', { class: 'qa__tile', href: '#/leads' }, icon('inbox'), h('span', { class: 'qa__label', text: 'Все заявки' })),
      can('export') ? h('button', { class: 'qa__tile', type: 'button', onclick: function (e) { exportCsv({}, e.currentTarget); } }, icon('download'), h('span', { class: 'qa__label', text: 'Экспорт CSV' })) : null,
      site ? h('a', { class: 'qa__tile', href: site, target: '_blank', rel: 'noopener noreferrer' }, icon('globe'), h('span', { class: 'qa__label', text: 'Открыть сайт' }), h('span', { class: 'sr', text: '(в новой вкладке)' })) : null,
      h('a', { class: 'qa__tile', href: '#/team' }, icon('users'), h('span', { class: 'qa__label', text: 'Пользователи' })),
      h('a', { class: 'qa__tile', href: '#/settings' }, icon('settings'), h('span', { class: 'qa__label', text: 'Настройки' }))
    ].filter(Boolean);
    tiles.forEach(function (t, i) { rise(t, i); });
    var qa = analytics ? null : h('nav', { class: 'qa', 'aria-label': 'Быстрые действия', vars: { '--n': String(tiles.length) } }, tiles);
    if (qa) qa.style.setProperty('grid-template-columns', 'repeat(' + tiles.length + ', minmax(0, 1fr))');

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
    var head = pageHead('Заявки', 'Все обращения с сайта: звонки, сообщения и формы', exportBtn, leadsTabs('leads'));

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
    // Ctrl/⌘+S saves the open section (and never opens the browser's «save page»)
    if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.code === 'KeyS' || e.key === 's' || e.key === 'S' || e.key === 'ы' || e.key === 'Ы')) {
      if (S.editor && S.user) { e.preventDefault(); if (!S.layers.length) S.editor.save(); }
      return;
    }
    if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.key === 'k' || e.key === 'K' || e.key === 'л' || e.key === 'Л')) {
      if (!S.user || !E.shell || !S.features.leads) return;
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
        opt.danger ? h('span', { class: 'dialog__ico' }, icon(opt.icon || 'trash')) : opt.icon ? h('span', { class: 'dialog__ico dialog__ico--info' }, icon(opt.icon)) : null,
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
        S.features.site ? ['home', 'Обзор', 'Сайт и публикации', '#/'] : ['users', 'Пользователи', 'Участники и роли', '#/team'],
        ['settings', 'Рабочее пространство', 'Уведомления и данные', '#/settings']
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

  /* ------------------------------------------------------------------ site: state */

  // Section icons come from the site's schema; only names we draw are used.
  var SEC_ICONS = { compass: 1, sun: 1, list: 1, grid: 1, 'check-circle': 1, alert: 1, 'eye-off': 1, sparkle: 1, chart: 1, bulb: 1, ruble: 1, star: 1, help: 1, briefcase: 1, form: 1, shield: 1, diamond: 1, layers: 1, user: 1, users: 1, check: 1, chat: 1, search: 1, pen: 1, phone: 1, send: 1, home: 1, image: 1, settings: 1,
    globe: 1, mail: 1, note: 1, shield: 1, calendar: 1, target: 1, zap: 1, bulb: 1, sparkle: 1, inbox: 1, form: 1, message: 1, clock: 1, key: 1,
    database: 1, eye: 1, bell: 1, history: 1, lock: 1, rocket: 1, type: 1, heading: 1, dashboard: 1 };
  function secIcon(name) { return has(SEC_ICONS, name) ? name : 'note'; }
  function findSection(id) {
    var sch = S.site && S.site.schema;
    if (!sch || !Array.isArray(sch.sections)) return null;
    return sch.sections.filter(function (x) { return x.id === id; })[0] || null;
  }
  function sectionsWord(n) { return plural(n, ['раздел', 'раздела', 'разделов']); }
  function clone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }
  // the same rule as the server: "HERO" or "SITE.operator"
  function getPath(c, key) {
    if (!c) return undefined;
    var i = key.indexOf('.');
    if (i < 0) return c[key];
    var m = c[key.slice(0, i)];
    return m && typeof m === 'object' && !Array.isArray(m) ? m[key.slice(i + 1)] : undefined;
  }

  var siteReq = null;
  function loadSite() {
    if (!siteReq) {
      siteReq = api('/site').then(function (d) { siteReq = null; setSite(d); loadThumbs(); return S.site; },
        function (e) { siteReq = null; throw e; });
    }
    return siteReq;
  }
  // d: the site state from GET /site (with schema) or from a save/restore (without)
  function setSite(d) {
    if (!d) return;
    if (d.enabled === false) { S.site = null; S.features.site = false; return; }
    var schema = d.schema || (S.site && S.site.schema) || null;
    S.site = Object.assign({}, d, { schema: schema, changed: Array.isArray(d.changed) ? d.changed : [] });
    S.siteErr = null;
    var sig = JSON.stringify([schema && schema.sections ? schema.sections.map(function (x) { return x.id; }) : [], S.site.changed, !!S.site.current, S.site.version]);
    if (sig !== E.navSig) { E.navSig = sig; renderSideNav(); }
    renderPubBar();
    if (S.onSite) S.onSite();
  }
  function changedSections() {
    return ((S.site && S.site.changed) || []).map(findSection).filter(Boolean);
  }
  function whoWhen(time, by) {
    var parts = [];
    if (time) parts.push(fullTime(time));
    if (by && by.name) parts.push(by.name);
    return parts.join(' · ');
  }

  /* ------------------------------------------------------------------ site: publish bar */

  function renderPubBar() {
    if (!E.pub) return;
    var st = S.site, text, tone;
    if (!st) { text = S.siteErr ? 'Сайт не загрузился' : 'Загрузка…'; tone = 'off'; }
    else if (!st.current) { text = 'Ещё не опубликован'; tone = 'draft'; }
    else if (st.dirty) { var n = st.changed.length; text = 'Черновик: изменено ' + n + NB + sectionsWord(n); tone = 'draft'; }
    else { text = 'Всё опубликовано'; tone = 'ok'; }
    if (st && st.scheduled && toDate(st.scheduled.at)) { text = 'Публикация ' + fullTime(st.scheduled.at); tone = 'draft'; }
    E.pubState.className = 'pub__state pub__state--' + tone;
    $('.pub__text', E.pubState).textContent = text;
    var names = changedSections().map(function (x) { return x.title; });
    E.pubState.setAttribute('title', st && st.current && names.length ? 'Изменены: ' + names.join(', ') : text);
    if (!S.publishing) {
      E.pubPreview.disabled = !st;
      E.pubPublish.disabled = !st;
    }
  }
  // a button that shows progress: spinner instead of its icon and a new label
  function working(btn, on, label) {
    if (!btn) return;
    var lab = $('.pub__label', btn) || $('.btn__label', btn);
    if (on) {
      if (lab && !btn.dataset.label) btn.dataset.label = lab.textContent;
      if (lab && label) lab.textContent = label;
      btn.disabled = true;
      btn.classList.add('is-working');
      btn.setAttribute('aria-busy', 'true');
    } else {
      if (lab && btn.dataset.label) lab.textContent = btn.dataset.label;
      delete btn.dataset.label;
      btn.disabled = false;
      btn.classList.remove('is-working');
      btn.removeAttribute('aria-busy');
    }
  }
  // unsaved edits in the open section go to the draft before a build
  function ensureSaved() {
    if (!S.editor || !S.editor.dirty()) return Promise.resolve(true);
    return S.editor.save().then(function (ok) {
      if (!ok) toast('Сначала сохраните раздел «' + S.editor.title + '»: в нём есть ошибки', 'alert', 'error');
      return ok;
    });
  }
  var BUILD_TIMEOUT = 200000;   // the server allows a build three minutes

  function previewSite(btn) {
    if (S.publishing || !can('site')) return;
    // The tab is opened now, inside the click: a tab opened after the build
    // finishes would be stopped by the popup blocker.
    var win = null;
    try { win = window.open('', '_blank'); } catch (e) { win = null; }
    if (win) {
      try {
        win.opener = null;
        win.document.title = 'Собираем предпросмотр…';
        var p = win.document.createElement('p');
        p.textContent = 'Собираем предпросмотр сайта…';
        p.style.setProperty('font', '16px/1.5 system-ui, sans-serif');
        p.style.setProperty('margin', '40px');
        p.style.setProperty('color', '#3C4167');
        win.document.body.appendChild(p);
      } catch (e) { /* ignore */ }
    }
    function shut() { try { if (win && !win.closed) win.close(); } catch (e) { /* ignore */ } }
    S.publishing = true;
    working(btn, true, 'Собираем…');
    if (E.pubPublish && btn !== E.pubPublish) E.pubPublish.disabled = true;
    ensureSaved().then(function (ok) {
      if (!ok) { shut(); return; }
      return api('/site/preview', { method: 'POST', body: {}, timeout: BUILD_TIMEOUT }).then(function (d) {
        var url = d && typeof d.url === 'string' && /^\/admin\/preview\/[A-Za-z0-9._\/-]*$/.test(d.url) ? d.url : '/admin/preview/';
        var miss = d && d.build && Array.isArray(d.build.missing) ? d.build.missing.length : 0;
        if (win && !win.closed) { win.location.replace(url); toast('Предпросмотр открыт в новой вкладке', 'eye', 'ok'); }
        else {
          // the browser blocked the tab: a link the person clicks opens it
          var m = modal({ title: 'Предпросмотр готов', icon: 'eye',
            body: [h('p', { class: 'dialog__text', text: 'Браузер не дал открыть вкладку сам. Откройте предпросмотр по кнопке.' })],
            foot: [h('button', { class: 'btn btn--soft', type: 'button', onclick: function () { m.close(); } }, 'Закрыть'),
              h('a', { class: 'btn btn--primary', href: url, target: '_blank', rel: 'noopener noreferrer', onclick: function () { setTimeout(function () { m.close(); }, 0); } }, icon('external', 'ico--sm'), 'Открыть предпросмотр')] });
        }
        if (miss) toast('Картинок пока нет: ' + miss + ' — на их месте заглушки', 'image');
        if (S.site) loadSite().catch(function () {});
      }, function (e) {
        shut();
        if (e.code === 'build') buildErrorDialog(e.data && e.data.build, 'preview');
        else if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
      });
    }).then(function () {
      S.publishing = false;
      working(btn, false);
      renderPubBar();
    });
  }

  function openPublish(btn) {
    if (S.publishing || !can('site') || !S.site) return;
    var st = S.site;
    var names = changedSections();
    var tid = nextId('pubnote');
    var note = h('textarea', { class: 'textarea', id: tid, rows: '3', maxlength: '200', placeholder: 'Например: новые цены в тарифах и два вопроса в FAQ', 'aria-describedby': tid + '-c ' + tid + '-e' });
    var counter = h('span', { class: 'fld__count', id: tid + '-c', text: '0/200' });
    var err = h('p', { class: 'fld__err', id: tid + '-e', role: 'alert', hidden: true });
    note.addEventListener('input', function () { counter.textContent = Array.from(note.value).length + '/200'; err.hidden = true; });
    var lead;
    if (!st.current) lead = 'Сайт ещё не публиковался из панели: на сайт попадёт весь черновик.';
    else if (names.length) lead = 'На сайт попадут правки в ' + names.length + NB + plural(names.length, ['разделе', 'разделах', 'разделах']) + ':';
    else lead = 'С последней публикации ничего не менялось: сайт соберётся заново из того же черновика.';
    var unsaved = S.editor && S.editor.dirty()
      ? h('p', { class: 'note note--warn' }, icon('alert', 'ico--sm'), h('span', { text: 'В разделе «' + S.editor.title + '» есть несохранённые правки — сначала сохраним их.' }))
      : null;
    var ok = h('button', { class: 'btn btn--primary', type: 'submit' }, icon('rocket', 'ico--sm'), h('span', { class: 'btn__label', text: 'Опубликовать' }));
    var cancel = h('button', { class: 'btn btn--soft', type: 'button', text: 'Отмена' });
    // what exactly changes: field by field, loaded when opened
    var diffBox = h('div', { class: 'diff' });
    var diffDet = st.current && names.length ? h('details', { class: 'diff__det' }, h('summary', { class: 'diff__sum', text: 'Подробнее: было → стало' }), diffBox) : null;
    if (diffDet) diffDet.addEventListener('toggle', function () {
      if (!diffDet.open || diffBox.dataset.loaded) return;
      diffBox.dataset.loaded = '1';
      fill(diffBox, h('div', { class: 'skel skel--line', vars: { width: '70%' } }));
      api('/site/diff').then(function (d) {
        var secs = (d && d.sections) || [];
        if (!secs.length) { fill(diffBox, h('p', { class: 'muted', text: 'Отличий нет.' })); return; }
        fill(diffBox, secs.map(function (sec) {
          return h('div', { class: 'diff__sec' }, h('h3', { class: 'diff__title', text: sec.title }),
            h('ul', { class: 'diff__list' }, sec.changes.map(function (c) {
              return h('li', { class: 'diff__item' }, h('span', { class: 'diff__label', text: c.label }),
                h('span', { class: 'diff__vals' }, h('del', { class: 'diff__was', text: c.before || 'пусто' }), h('span', { 'aria-hidden': 'true', text: ' → ' }), h('ins', { class: 'diff__now', text: c.after || 'пусто' })));
            }), sec.more ? h('li', { class: 'diff__more', text: 'и ещё ' + sec.more }) : null));
        }));
      }, function (e) { fill(diffBox, h('p', { class: 'fld__err', text: errText(e) })); });
    });
    // publish later: a time in the browser's clock
    var sid = nextId('later');
    var laterChk = h('input', { type: 'checkbox', id: sid, class: 'check' });
    var pad = function (n) { return (n < 10 ? '0' : '') + n; };
    var localValue = function (d) { return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + 'T' + pad(d.getHours()) + ':' + pad(d.getMinutes()); };
    var soon = new Date(Date.now() + 60 * 60000);
    var laterAt = h('input', { type: 'datetime-local', class: 'input input--date', 'aria-label': 'Когда опубликовать', min: localValue(new Date(Date.now() + 2 * 60000)), hidden: true });
    laterAt.value = localValue(soon);
    laterChk.addEventListener('change', function () {
      laterAt.hidden = !laterChk.checked;
      $('.btn__label', ok).textContent = laterChk.checked ? 'Запланировать' : 'Опубликовать';
    });
    var planned = st.scheduled && toDate(st.scheduled.at) ? h('p', { class: 'note' }, icon('calendar', 'ico--sm'),
      h('span', { text: 'Уже запланировано на ' + fullTime(st.scheduled.at) + (st.scheduled.note ? ' («' + st.scheduled.note + '»)' : '') + '. ' }),
      h('button', { class: 'btn btn--ghost btn--sm', type: 'button', onclick: function (e) {
        var bt = e.currentTarget;
        working(bt, true, 'Отменяем…');
        api('/site/schedule', { method: 'DELETE' }).then(function () { toast('Запланированная публикация отменена', 'calendar', 'ok'); planned.remove(); return loadSite(); },
          function (er) { working(bt, false); toast(errText(er), 'alert', 'error'); });
      } }, h('span', { class: 'btn__label', text: 'Отменить план' }))) : null;
    var later = h('div', { class: 'later' }, h('label', { class: 'later__chk', for: sid }, laterChk, h('span', { text: 'Опубликовать позже' })), laterAt);
    var form = h('form', { class: 'dialog__form', novalidate: true },
      h('p', { class: 'dialog__text', text: lead }),
      st.current && names.length ? h('ul', { class: 'chiplist' }, names.map(function (x) { return h('li', null, h('span', { class: 'chip' }, icon(secIcon(x.icon), 'ico--xs'), x.title)); })) : null,
      diffDet,
      planned,
      unsaved,
      h('div', { class: 'fld fld--wide' },
        h('div', { class: 'fld__top' }, h('label', { class: 'field__label', for: tid, text: 'Что изменили' }), counter),
        note,
        h('p', { class: 'fld__hint', text: 'Необязательно. Видно в резервных копиях — проще найти нужную версию.' }),
        err),
      later,
      h('div', { class: 'dialog__foot' }, cancel, ok));
    var m = modal({ title: 'Опубликовать сайт', icon: 'rocket', body: [form], focus: note });
    cancel.addEventListener('click', function () { m.close(); });
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (S.publishing) return;
      if (laterChk.checked) {
        var at = new Date(laterAt.value);
        if (isNaN(at.getTime()) || at.getTime() < Date.now() + 60000) { err.textContent = 'Выберите время хотя бы через пару минут'; err.hidden = false; laterAt.focus(); return; }
        working(ok, true, 'Планируем…');
        ensureSaved().then(function (saved) {
          if (!saved) { working(ok, false); return; }
          return api('/site/schedule', { method: 'PUT', body: { at: at.toISOString(), note: note.value.trim() } }).then(function () {
            m.close();
            toast('Публикация запланирована на ' + fullTime(at.toISOString()), 'calendar', 'ok');
            return loadSite().catch(function () {});
          }, function (e2) {
            working(ok, false);
            err.textContent = e2.code === 'invalid' && e2.field === 'at' ? 'Время — не раньше чем через минуту и не позже чем через 90 дней' : errText(e2);
            err.hidden = false;
          });
        });
        return;
      }
      S.publishing = true;
      m.locked = true;
      working(ok, true, 'Публикуем…');
      cancel.disabled = true;
      working(btn, true, 'Публикуем…');
      if (E.pubPreview) E.pubPreview.disabled = true;
      var text = note.value.trim();
      ensureSaved().then(function (saved) {
        if (!saved) { m.locked = false; m.close(); return; }
        return api('/site/publish', { method: 'POST', body: { note: text }, timeout: BUILD_TIMEOUT }).then(function (d) {
          m.locked = false;
          m.close();
          var miss = d && d.build && Array.isArray(d.build.missing) ? d.build.missing.length : 0;
          toast('Опубликовано', 'rocket', 'ok');
          if (miss) toast('Картинок пока нет: ' + miss + ' — на их месте заглушки', 'image');
          return loadSite().catch(function () {});
        }, function (e) {
          m.locked = false;
          if (e.code === 'build') { m.close(); buildErrorDialog(e.data && e.data.build, 'publish'); return; }
          working(ok, false);
          cancel.disabled = false;
          if (e.code === 'auth') { m.close(); return; }
          err.textContent = e.code === 'invalid' && e.field === 'note' ? 'Описание не длиннее 200 символов, без служебных символов' : errText(e);
          err.hidden = false;
          if (e.code === 'invalid') note.focus();
        });
      }).then(function () {
        S.publishing = false;
        working(btn, false);
        renderPubBar();
      });
    });
  }

  function lastLines(text, n) {
    var lines = String(text || '').replace(/\s+$/, '').split('\n');
    return lines.slice(Math.max(0, lines.length - n)).join('\n');
  }
  function buildErrorDialog(build, kind) {
    var log = (build && build.log) || 'Сборщик ничего не сообщил.';
    var m = modal({ title: 'Сборка не удалась', icon: 'alert', tone: 'danger', wide: true,
      body: [
        h('p', { class: 'dialog__text', text: (kind === 'publish' ? 'Сайт не изменился: на нём осталась прежняя версия. ' : 'Предпросмотр не обновился. ') + 'Вот что сообщил сборщик — обычно в последних строках видно, что мешает.' }),
        h('pre', { class: 'log', tabindex: '0', 'aria-label': 'Журнал сборки', text: lastLines(log, 120) })],
      foot: [
        h('button', { class: 'btn btn--soft', type: 'button', onclick: function () { copyText(log); } }, icon('copy', 'ico--sm'), 'Скопировать журнал'),
        h('a', { class: 'btn btn--soft', href: '#/health', onclick: function () { m.close(); } }, icon('pulse', 'ico--sm'), 'Проверка сайта'),
        h('button', { class: 'btn btn--primary', type: 'button', onclick: function () { m.close(); } }, 'Закрыть')] });
  }

  /* generic modal: title, body nodes, foot buttons; Esc and the scrim close it unless locked */
  function modal(opt) {
    var tid = nextId('dlg');
    var prevFocus = document.activeElement;
    var overlay = h('div', { class: 'overlay overlay--top' });
    var el = h('div', { class: 'dialog' + (opt.wide ? ' dialog--wide' : ''), role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': tid },
      opt.icon ? h('span', { class: 'dialog__ico dialog__ico--' + (opt.tone || 'info') }, icon(opt.icon)) : null,
      h('h2', { class: 'dialog__title', id: tid, text: opt.title }),
      h('div', { class: 'dialog__body' }, opt.body),
      opt.foot ? h('div', { class: 'dialog__foot' }, opt.foot) : null);
    var layer = { el: el, overlay: overlay };
    var closed = false;
    var m = {
      el: el, locked: false,
      close: function () {
        if (closed) return;
        closed = true;
        popLayer(layer);
        leave(overlay);
        el.remove();
        if (opt.onClose) opt.onClose();
        var back = opt.returnTo || prevFocus;
        if (back && back.isConnected && !S.layers.length) back.focus({ preventScroll: true });
      }
    };
    layer.onEsc = function () { if (!m.locked) m.close(); };
    layer.close = function () { m.close(); };
    overlay.addEventListener('click', function () { if (!m.locked) m.close(); });
    pushLayer(layer);
    E.layers.appendChild(overlay);
    E.layers.appendChild(el);
    setTimeout(function () { var f = opt.focus || focusables(el)[0]; if (f && f.isConnected) f.focus(); }, 0);
    return m;
  }

  /* ------------------------------------------------------------------ site: overview */

  var QUICK_SECTIONS = ['hero', 'tariffs', 'cases', 'blog', 'site'];

  function viewOverview(root, r, token) {
    var hello = 'С возвращением, ' + firstName(S.user.name) + '!';
    var head = pageHead(hello, 'Сайт, заявки и команда — в одном месте');
    var body = h('div', { class: 'dash ov' });
    fill(root, h('div', { class: 'dash' }, head, body));

    var extra = { releases: null, health: null, waiting: null, relErr: null, healthErr: null };
    var ctrl = new AbortController();
    S.cleanups.push(function () { ctrl.abort(); });

    function skeleton() {
      function sk(cls) { return h('div', { class: 'skel ' + (cls || '') }); }
      return [
        h('div', { class: 'row-a' }, h('div', { class: 'card' }, sk('skel--line'), sk('skel--num'), h('div', { class: 'skel', vars: { height: '120px', 'margin-top': '16px' } })),
          h('div', { class: 'card' }, sk('skel--line'), sk('skel--num'))),
        h('div', { class: 'qa' }, [0, 1, 2, 3, 4, 5].map(function () { return h('div', { class: 'qa__tile' }, sk('skel--line')); }))
      ];
    }
    function draw() {
      if (!alive(token)) return;
      if (!S.site) {
        body.setAttribute('aria-busy', S.siteErr ? 'false' : 'true');
        fill(body, S.siteErr ? errorCard(S.siteErr, function () {
          S.siteErr = null; draw();
          loadSite().catch(function (e) { S.siteErr = e; draw(); });
        }) : skeleton());
        return;
      }
      body.removeAttribute('aria-busy');
      // cards rise once; later redraws (releases, health, a poll) only swap content
      var R = animate ? rise : function (el) { return el; };
      var i = 0;
      var rows = [];
      rows.push(h('div', { class: S.features.leads ? 'row-a' : 'row-1' }, R(siteCard(), i++), S.features.leads ? R(leadsCard(), i++) : null));
      rows.push(quickTiles(i, R));
      i += 6;
      rows.push(h('div', { class: 'row-2' }, R(releasesCard(), i++), R(healthCard(), i++)));
      fill(body, rows);
      animate = false;
    }
    var animate = true;

    function siteCard() {
      var st = S.site;
      var cur = st.current;
      var names = changedSections();
      var site = safeHttpUrl(S.ws && S.ws.siteURL);
      var status = cur
        ? h('div', { class: 'sitest' },
            h('span', { class: 'sitest__ico sitest__ico--ok', 'aria-hidden': 'true' }, icon('globe')),
            h('div', { class: 'sitest__text' },
              h('p', { class: 'sitest__title', text: st.dirty ? 'Сайт опубликован, в черновике есть правки' : 'Сайт опубликован, правок нет' }),
              h('p', { class: 'sitest__meta', text: 'Опубликован ' + whoWhen(cur.time, cur.by) }),
              cur.note ? h('p', { class: 'sitest__note', text: '«' + cur.note + '»' }) : null))
        : h('div', { class: 'sitest' },
            h('span', { class: 'sitest__ico', 'aria-hidden': 'true' }, icon('rocket')),
            h('div', { class: 'sitest__text' },
              h('p', { class: 'sitest__title', text: 'Сайт ещё не публиковался из панели' }),
              h('p', { class: 'sitest__meta', text: 'Правьте разделы, смотрите предпросмотр и публикуйте, когда всё готово.' })));
      var changedBox = cur ? null : h('ol', { class: 'tile steps' },
        h('li', null, h('span', null, b('Правьте разделы'), '\u00A0— правки сохраняются в черновик, посетители их не видят.')),
        h('li', null, h('span', null, b('Предпросмотр'), ' покажет сайт с правками в новой вкладке.')),
        h('li', null, h('span', null, b('Опубликовать'), ' выложит правки на сайт; прошлая версия останется в резервных копиях.')));
      if (cur) {
        changedBox = names.length
          ? h('div', { class: 'tile changed' },
              h('p', { class: 'changed__title' }, 'Изменено разделов: ', b(String(names.length))),
              h('ul', { class: 'chiplist' }, names.map(function (x) {
                return h('li', null, h('a', { class: 'chip chip--link', href: '#/edit/' + encodeURIComponent(x.id) }, icon(secIcon(x.icon), 'ico--xs'), x.title));
              })))
          : h('div', { class: 'tile changed changed--ok' }, icon('check-circle', 'ico--sm'), h('span', { text: 'Все правки уже на сайте' }));
      }
      var lb = st.lastBuild;
      var buildLine = lb ? h('p', { class: 'build-line' + (lb.ok ? '' : ' build-line--bad') },
        icon(lb.ok ? 'check' : 'alert', 'ico--xs'),
        h('span', { text: 'Последняя сборка' + NB + '— ' + (lb.kind === 'publish' ? 'публикация' : 'предпросмотр') + ', ' + relTime(lb.time) + (lb.ok ? '' : ': не удалась') }),
        lb.ok ? null : h('a', { class: 'link', href: '#/health', text: 'Подробнее' })) : null;
      var actions = can('site') ? h('div', { class: 'site-card__actions' },
        h('button', { class: 'btn btn--ghost', type: 'button', onclick: function (e) { previewSite(e.currentTarget); } }, icon('eye', 'ico--sm'), h('span', { class: 'btn__label', text: 'Предпросмотр' })),
        h('button', { class: 'btn btn--primary', type: 'button', onclick: function (e) { openPublish(e.currentTarget); } }, icon('rocket', 'ico--sm'), h('span', { class: 'btn__label', text: 'Опубликовать' }))) : null;
      var c = h('section', { class: 'card site-card', 'aria-labelledby': 'h-site' },
        h('div', { class: 'card__head' }, h('h2', { class: 'label', id: 'h-site', text: 'Сайт' }),
          site ? h('a', { class: 'btn btn--sm btn--soft', href: site, target: '_blank', rel: 'noopener noreferrer' }, 'Открыть сайт', icon('arrow-up-right', 'ico--xs'), h('span', { class: 'sr', text: ' (в новой вкладке)' })) : null),
        status, changedBox, h('div', { class: 'site-card__foot' }, buildLine, actions));
      return c;
    }

    function leadsCard() {
      var n = S.counts.new || 0;
      var w = extra.waiting;
      var big = h('div', { class: 'speed__num' }, num(n), h('small', { text: NB + plural(n, ['новая', 'новые', 'новых']) }));
      return h('section', { class: 'card speed leads-card', 'aria-labelledby': 'h-leads' },
        h('div', { class: 'card__head' }, h('h2', { class: 'label', id: 'h-leads', text: 'Заявки' }), h('span', { class: 'round-go round-go--glass', 'aria-hidden': 'true' }, icon('inbox', 'ico--sm'))),
        h('div', null, big, h('p', { class: 'speed__meta', text: n ? plural(n, ['ждёт', 'ждут', 'ждут']) + ' ответа' : 'новых заявок нет — всё разобрано' })),
        w ? h('p', { class: 'leads-card__warn' }, icon('clock', 'ico--sm'), h('span', { text: num(w) + NB + plural(w, ['ждёт', 'ждут', 'ждут']) + ' дольше часа' })) : null,
        h('div', { class: 'leads-card__actions' },
          h('a', { class: 'btn btn--glass', href: n ? '#/leads?status=new' : '#/leads' }, n ? 'Разобрать новые' : 'Все заявки', icon('arrow-right', 'ico--sm')),
          h('a', { class: 'btn btn--glass-ghost', href: '#/analytics' }, icon('dashboard', 'ico--sm'), 'Аналитика')));
    }

    function quickTiles(i0, R) {
      var sch = S.site.schema || { sections: [] };
      var list = QUICK_SECTIONS.map(findSection).filter(Boolean);
      if (list.length < 5) (sch.sections || []).forEach(function (x) { if (list.length < 5 && list.indexOf(x) < 0) list.push(x); });
      var tiles = list.map(function (x, k) {
        return h('a', { class: 'qa__tile' + (k === 0 ? ' qa__tile--dark' : ''), href: '#/edit/' + encodeURIComponent(x.id) }, icon(secIcon(x.icon)), h('span', { class: 'qa__label', text: x.title }));
      });
      tiles.push(h('a', { class: 'qa__tile', href: '#/health' }, icon('pulse'), h('span', { class: 'qa__label', text: 'Проверка сайта' })));
      tiles.forEach(function (t, k) { R(t, i0 + k); });
      var qa = h('nav', { class: 'qa', 'aria-label': 'Частые разделы' }, tiles);
      qa.style.setProperty('grid-template-columns', 'repeat(' + tiles.length + ', minmax(0, 1fr))');
      return qa;
    }

    function releasesCard() {
      var list;
      if (extra.relErr) list = h('p', { class: 'muted small', text: errText(extra.relErr) });
      else if (!extra.releases) list = h('div', { class: 'feed' }, [0, 1, 2].map(function () { return h('div', { class: 'feed__item' }, h('span', { class: 'av skel' }), h('div', { class: 'feed__body' }, h('div', { class: 'skel skel--line' }))); }));
      else if (!extra.releases.length) list = h('p', { class: 'feed__empty', text: 'Публикаций пока нет. После первой здесь появится резервная копия сайта.' });
      else {
        list = h('ul', { class: 'feed' }, extra.releases.slice(0, 3).map(function (rel) {
          var who = rel.by && rel.by.name;
          return h('li', null, h('a', { class: 'feed__item', href: '#/releases' },
            rel.by ? avatar(rel.by) : h('span', { class: 'av av--visitor', 'aria-hidden': 'true' }, icon('rocket', 'ico--sm')),
            h('div', { class: 'feed__body' },
              h('p', { class: 'feed__text' }, who ? [b(who), ' ' + verb(who, 'опубликовал', 'опубликовала') + ' сайт'] : 'Публикация сайта',
                rel.note ? [': «', rel.note, '»'] : ''),
              h('span', { class: 'feed__time', text: listTime(rel.time) })),
            rel.current ? h('span', { class: 'pill st--won', text: 'на сайте' }) : h('span', { class: 'round-go', 'aria-hidden': 'true' }, icon('arrow-up-right', 'ico--sm'))));
        }));
      }
      return h('section', { class: 'card', 'aria-labelledby': 'h-rel' },
        h('div', { class: 'card__head' }, h('h2', { class: 'label', id: 'h-rel', text: 'Последние публикации' }),
          h('a', { class: 'btn btn--sm btn--soft', href: '#/releases' }, 'Все копии', icon('arrow-right', 'ico--xs'))),
        list);
    }

    function healthCard() {
      var inner;
      if (extra.healthErr) inner = h('p', { class: 'muted small', text: errText(extra.healthErr) });
      else if (!extra.health) inner = h('div', { class: 'hsum' }, [0, 1, 2].map(function () { return h('div', { class: 'tile hsum__tile' }, h('div', { class: 'skel skel--line' })); }));
      else {
        var c = { ok: 0, warn: 0, error: 0 };
        extra.health.forEach(function (x) { if (has(c, x.level)) c[x.level]++; });
        var bad = extra.health.filter(function (x) { return x.level !== 'ok'; }).sort(function (a, z) { return (a.level === 'error' ? 0 : 1) - (z.level === 'error' ? 0 : 1); });
        function tile(level, label, n) {
          return h('a', { class: 'tile hsum__tile hsum__tile--' + level, href: '#/health' },
            h('span', { class: 'hsum__num', text: num(n) }), h('span', { class: 'hsum__label', text: label }));
        }
        inner = [
          h('div', { class: 'hsum' },
            tile('error', plural(c.error, ['ошибка', 'ошибки', 'ошибок']), c.error),
            tile('warn', plural(c.warn, ['замечание', 'замечания', 'замечаний']), c.warn),
            tile('ok', 'в порядке', c.ok)),
          bad.length ? h('ul', { class: 'attn' }, bad.slice(0, 3).map(function (x) {
            return h('li', null, h('a', { class: 'attn__item', href: '#/health' },
              h('span', { class: 'hmark hmark--' + x.level, 'aria-hidden': 'true' }, icon(x.level === 'error' ? 'alert' : 'clock', 'ico--xs')),
              h('span', { class: 'attn__body' }, h('span', { class: 'attn__title', text: x.title || '' }),
                h('span', { class: 'attn__hint', text: x.details && x.details.length ? x.details.length + NB + plural(x.details.length, ['пункт', 'пункта', 'пунктов']) : (x.level === 'error' ? 'Ошибка' : 'Замечание') })),
              h('span', { class: 'round-go', 'aria-hidden': 'true' }, icon('arrow-up-right', 'ico--sm'))));
          })) : h('p', { class: 'attn__done' }, icon('check-circle'), 'Проверки пройдены')];
      }
      return h('section', { class: 'card', 'aria-labelledby': 'h-health' },
        h('div', { class: 'card__head' }, h('h2', { class: 'label', id: 'h-health', text: 'Проверка сайта' }),
          h('a', { class: 'btn btn--sm btn--soft', href: '#/health' }, 'Подробнее', icon('arrow-right', 'ico--xs'))),
        inner);
    }

    function loadExtra() {
      if (!S.features.site) return;
      api('/site/releases', { signal: ctrl.signal }).then(function (d) { extra.releases = d.items || []; extra.relErr = null; }, function (e) { if (e.code !== 'abort') extra.relErr = e; })
        .then(function () { if (alive(token) && S.site) draw(); });
      api('/site/health', { signal: ctrl.signal }).then(function (d) { extra.health = d.checks || []; extra.healthErr = null; }, function (e) { if (e.code !== 'abort') extra.healthErr = e; })
        .then(function () { if (alive(token) && S.site) draw(); });
      if (S.features.leads && S.counts.new) {
        api('/leads?status=new&waiting=1&limit=1', { signal: ctrl.signal }).then(function (d) { extra.waiting = d.total || 0; if (alive(token) && S.site) draw(); }, function () {});
      }
    }
    S.onSite = function () { if (alive(token)) draw(); };
    draw();
    if (!S.site && !S.siteErr) loadSite().catch(function (e) { S.siteErr = e; draw(); });
    loadExtra();
  }

  /* ------------------------------------------------------------------ site: section editor */

  // Every value is drawn from the schema and kept in a plain JSON model; the
  // inputs write into it, structural changes (add / remove / move) redraw the
  // list they belong to. Every field wrapper carries data-path — the same
  // path the server names in a refused save ("CASES[2].slug").
  var SIMPLE = { text: 1, textarea: 1, url: 1, email: 1, number: 1, toggle: 1, select: 1, color: 1, date: 1, image: 1 };
  var WIDE = { textarea: 1, image: 1, list: 1, map: 1, blocks: 1, tuple: 1, object: 1 };
  var BLOCK_KINDS = [['p', 'Абзац', 'type'], ['h2', 'Подзаголовок', 'heading'], ['ul', 'Список', 'list'], ['quote', 'Цитата', 'chat']];
  var MEDIA_THUMBS = {};   // media id → thumbnail URL from an upload in this tab (memory only)
  var reMediaId = /^[0-9a-f]{16}$/;
  var reThumb = /^\/admin\/media\/[0-9a-f]{16}-[0-9]{2,4}\.jpg$/;

  function chars(s) { return Array.from(String(s || '')).length; }
  function charsWord(n) { return plural(n, ['символа', 'символов', 'символов']); }   // «не длиннее 21 символа / 40 символов»
  function thumbUrl(id) {
    if (!reMediaId.test(String(id || ''))) return null;
    return MEDIA_THUMBS[id] || '/admin/media/' + id + '-480.jpg';
  }

  // what the server accepts for one value; '' when it looks fine (the server has the last word)
  function localCheck(f, v) {
    if (f.readonly) return '';
    var t = f.type;
    if (t === 'toggle' || t === 'image') return '';
    if (t === 'number') {
      if ((v === null || v === undefined) && !f.required) return '';
      if (typeof v !== 'number' || isNaN(v)) return 'Введите число';
      if (f.integer && Math.floor(v) !== v) return 'Нужно целое число';
      if ((f.min != null && v < f.min) || (f.max != null && v > f.max)) {
        return f.min != null && f.max != null ? 'Число от ' + f.min + ' до ' + f.max : f.min != null ? 'Не меньше ' + f.min : 'Не больше ' + f.max;
      }
      return '';
    }
    var s = typeof v === 'string' ? v : '';
    if (t !== 'textarea') s = s.trim();
    if (!s) return f.required ? (t === 'select' ? 'Выберите вариант' : 'Заполните поле') : '';
    var max = f.max != null ? f.max : (t === 'textarea' ? 4000 : 300);
    if (t !== 'select' && chars(s) > max) return 'Не длиннее ' + max + NB + charsWord(max);
    if (f.min != null && t !== 'select' && chars(s) < f.min) return 'Не короче ' + f.min + NB + charsWord(f.min);
    if (f.pattern) {
      try { if (!new RegExp(f.pattern).test(s)) return 'Не подходит формат' + (f.hint ? ' — см. подсказку' : ''); } catch (e) { /* the server checks */ }
    }
    if (t === 'url' && s !== '#' && !/^https?:\/\/[^\s/?#]+\.[^\s/?#]+/i.test(s)) return 'Нужна ссылка вида https://…';
    if (t === 'email' && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s)) return 'Нужна почта вида name@domain.ru';
    if (t === 'color' && !/^#[0-9A-Fa-f]{6}$/.test(s)) return 'Нужен цвет вида #3341FF';
    if (t === 'date' && !/^\d{4}-\d{2}-\d{2}$/.test(s)) return 'Нужна дата';
    return '';
  }

  function selectOptions(f, ctx) {
    var out = [];
    (f.options || []).forEach(function (o) {
      if (Array.isArray(o)) out.push([String(o[0]), String(o[1] !== undefined && o[1] !== null ? o[1] : o[0])]);
    });
    if (f.optionsFrom) {
      var parts = String(f.optionsFrom).split('.'), name = parts[0], sub = parts[1];
      // values being edited in this section win over the saved draft
      var src = ctx && ctx.values && has(ctx.values, name) ? ctx.values[name] : (S.site && S.site.content ? S.site.content[name] : null);
      if (src && typeof src === 'object' && !Array.isArray(src) && !sub) {
        Object.keys(src).forEach(function (k) { out.push([k, typeof src[k] === 'string' && src[k].trim() ? src[k] : k]); });
      } else if (Array.isArray(src) && sub) {
        src.forEach(function (it) {
          if (!it || typeof it !== 'object' || typeof it[sub] !== 'string' || !it[sub]) return;
          var label = typeof it.title === 'string' && it.title.trim() ? it.title : typeof it.name === 'string' && it.name.trim() ? it.name : it[sub];
          out.push([it[sub], label]);
        });
      }
    }
    return out;
  }

  function defValue(f, ctx) {
    switch (f.type) {
      case 'number': return f.min !== undefined && f.min !== null ? f.min : 0;
      case 'toggle': return false;
      case 'color': return '#4348DB';
      case 'select':
        if (f.required) { var op = selectOptions(f, ctx); return op.length ? op[0][0] : ''; }
        return '';
      case 'tuple': return (f.items || []).map(function (x) { return x.readonly ? null : defValue(x, ctx); });
      case 'object':
        var o = {};
        (f.fields || []).forEach(function (k) { if (!k.readonly) o[k.key] = defValue(k, ctx); });
        return o;
      case 'list':
        var a = [];
        for (var i = 0; i < (f.min || 0); i++) a.push(defValue(f.item, ctx));
        return a;
      case 'blocks': return (f.min || 0) > 0 ? [['p', '']] : [];
      case 'map': return {};
    }
    return '';
  }

  // the value as the server expects it: object keys from the schema only
  // (other sections' keys of "SITE" stay on the server), lists of lines tidy
  function pick(f, v) {
    if (v === undefined) return undefined;
    switch (f.type) {
      case 'object':
        if (!v || typeof v !== 'object' || Array.isArray(v)) return v;
        var o = {};
        (f.fields || []).forEach(function (k) { if (Object.prototype.hasOwnProperty.call(v, k.key)) o[k.key] = pick(k, v[k.key]); });
        return o;
      case 'tuple': return Array.isArray(v) ? v.map(function (x, i) { return f.items && f.items[i] ? pick(f.items[i], x) : x; }) : v;
      case 'list': return Array.isArray(v) ? v.map(function (x) { return pick(f.item, x); }) : v;
      case 'map':
        if (!v || typeof v !== 'object' || Array.isArray(v)) return v;
        var m = {};
        Object.keys(v).forEach(function (k) { m[k] = pick(f.item, v[k]); });
        return m;
      case 'blocks':
        return Array.isArray(v) ? v.map(function (bl) {
          if (Array.isArray(bl) && bl[0] === 'ul' && Array.isArray(bl[1])) return ['ul', bl[1].map(function (x) { return String(x).trim(); }).filter(Boolean)];
          return bl;
        }) : v;
      case 'number': return typeof v === 'number' && !isNaN(v) ? v : null;
    }
    return v;
  }

  function itemTitle(f, x, i) {
    var t = f.itemTitle, s = '';
    if (typeof t === 'number' && Array.isArray(x)) s = x[t];
    else if (typeof t === 'string' && x && typeof x === 'object' && !Array.isArray(x)) s = x[t];
    else if (t === undefined && Array.isArray(x) && f.item && f.item.items) {
      // no title in the schema: the first visible text of the item
      for (var k = 0; k < f.item.items.length; k++) { if (!f.item.items[k].hidden && typeof x[k] === 'string') { s = x[k]; break; } }
    }
    s = typeof s === 'number' ? String(s) : typeof s === 'string' ? s.trim() : '';
    return s || 'Пункт ' + (i + 1);
  }

  function autosize(ta) {
    if (!ta || !ta.isConnected || ta.offsetParent === null) return;
    ta.style.setProperty('height', 'auto');
    ta.style.setProperty('height', (ta.scrollHeight + 2) + 'px');
  }
  function autosizeIn(root) { $$('textarea.fld__ta', root).forEach(autosize); }

  /* one field: label row (label + counter), control, hint, message */
  function fieldShell(f, path, ctx, o, build) {
    var id = nextId('f');
    var wrap = h('div', { class: 'fld fld--' + f.type + (WIDE[f.type] ? ' fld--wide' : '') + (o.compact ? ' fld--compact' : ''), data: { path: path } });
    var label = o.label !== undefined ? o.label : (f.label || '');
    var msg = h('p', { class: 'fld__err', id: id + '-e', hidden: true });
    var hint = f.hint && !o.noHint ? h('p', { class: 'fld__hint', id: id + '-h', text: f.hint }) : null;
    var count = f.max && (f.type === 'text' || f.type === 'textarea') && !f.readonly ? h('span', { class: 'fld__count num', id: id + '-c', 'aria-hidden': 'true' }) : null;
    var touched = false, serverMsg = '';
    var api_ = {};
    var ctl = build(id, function () { return [hint && id + '-h', id + '-e'].filter(Boolean).join(' '); }, api_);
    var focusEl = api_.focusEl || ctl;
    if (label && f.type !== 'toggle') {
      add(wrap, h('div', { class: 'fld__top' },
        h('label', { class: o.keyLabel ? 'field__label fld__key' : 'field__label', for: id, text: label },
          f.required && !f.readonly && !ctx.ro ? h('span', { class: 'fld__req', 'aria-hidden': 'true', text: ' *' }) : null),
        count));
    } else if (count) add(wrap, h('div', { class: 'fld__top fld__top--count' }, count));
    if (!label && o.sr && focusEl && focusEl.setAttribute) focusEl.setAttribute('aria-label', o.sr);
    add(wrap, ctl, hint, msg);
    function value() { return api_.get ? api_.get() : undefined; }
    function show(text, kind) {
      msg.textContent = '';
      if (text) add(msg, icon(kind === 'error' ? 'alert' : 'alert', 'ico--xs'), h('span', { text: text }));
      msg.hidden = !text;
      msg.className = 'fld__err' + (kind === 'warn' ? ' fld__err--warn' : '');
      wrap.classList.toggle('is-error', kind === 'error' && !!text);
      wrap.classList.toggle('is-warn', kind === 'warn' && !!text);
      if (focusEl && focusEl.setAttribute) focusEl.setAttribute('aria-invalid', kind === 'error' && text ? 'true' : 'false');
      if (kind === 'error' && text) msg.setAttribute('role', 'alert'); else msg.removeAttribute('role');
    }
    function check(live) {
      if (count) {
        var n = chars(value());
        count.textContent = n + '/' + f.max;
        count.classList.toggle('is-over', n > f.max);
      }
      if (serverMsg) return;
      var m = localCheck(f, value());
      // while typing only the hard limits show; the rest after the field is left
      if (m && (touched || /^Не длиннее/.test(m))) show(m, 'warn');
      else if (!live || !m) show('', '');
    }
    wrap.axm = {
      show: function (text) { serverMsg = text; show(text, 'error'); },
      clear: function () { if (serverMsg) { serverMsg = ''; show('', ''); check(); } },
      focus: function () { if (focusEl && focusEl.focus) focusEl.focus({ preventScroll: true }); },
      changed: function () { if (serverMsg) { serverMsg = ''; show('', ''); } check(true); },
      blur: function () { touched = true; check(); }
    };
    check();
    return wrap;
  }

  function renderScalar(f, ref, path, ctx, o) {
    o = o || {};
    var ro = ctx.ro || !!f.readonly;
    function get() { return ref.obj[ref.key]; }
    function set(v) { if (ref.attach) ref.attach(); ref.obj[ref.key] = v; wrap.axm.changed(); ctx.changed(); }
    var wrap = fieldShell(f, path, ctx, o, function (id, described, api_) {
      api_.get = get;
      var v = get();
      var el;
      switch (f.type) {
        case 'textarea':
          el = h('textarea', { class: 'textarea fld__ta', id: id, rows: o.rows || '2', disabled: ro, spellcheck: 'true', 'aria-describedby': described() });
          el.value = typeof v === 'string' ? v : '';
          el.addEventListener('input', function () { set(el.value); autosize(el); });
          requestAnimationFrame(function () { autosize(el); });
          break;
        case 'number':
          el = h('input', { class: 'input input--num', id: id, type: 'number', inputmode: f.integer ? 'numeric' : 'decimal', step: f.integer ? '1' : 'any',
            min: f.min !== undefined && f.min !== null ? String(f.min) : null, max: f.max !== undefined && f.max !== null ? String(f.max) : null, disabled: ro, 'aria-describedby': described() });
          el.value = typeof v === 'number' ? String(v) : '';
          el.addEventListener('input', function () { set(el.value === '' ? null : Number(el.value)); });
          break;
        case 'toggle':
          var inp = h('input', { class: 'switch__input', id: id, type: 'checkbox', role: 'switch', disabled: ro, 'aria-describedby': described() });
          inp.checked = v === true;
          inp.addEventListener('change', function () { set(inp.checked); });
          api_.focusEl = inp;
          el = h('label', { class: 'switch', for: id }, inp, h('span', { class: 'switch__track', 'aria-hidden': 'true' }), h('span', { class: 'switch__text', text: o.label !== undefined ? o.label : f.label || '' }));
          break;
        case 'select':
          el = h('select', { class: 'select', id: id, disabled: ro, 'aria-describedby': described() });
          var refill = function () {
            var cur = get();
            var opts = selectOptions(f, ctx);
            clear(el);
            if (!f.required || !cur) add(el, h('option', { value: '', text: f.required ? 'Выберите…' : 'Не выбрано' }));
            var seen = false;
            opts.forEach(function (op) { if (op[0] === cur) seen = true; add(el, h('option', { value: op[0], text: op[1] })); });
            if (cur && !seen) add(el, h('option', { value: cur, text: cur + ' (нет в списке)' }));
            el.value = typeof cur === 'string' ? cur : '';
          };
          refill();
          // options can come from this very form (renamed cases): refresh before opening
          el.addEventListener('focus', refill);
          el.addEventListener('change', function () { set(el.value); });
          break;
        case 'color':
          var hex = typeof v === 'string' ? v : '';
          var picker = h('input', { class: 'color__pick', type: 'color', disabled: ro, 'aria-label': (o.label !== undefined ? o.label : f.label || 'Цвет') + ': выбрать', tabindex: ro ? '-1' : null });
          picker.value = /^#[0-9A-Fa-f]{6}$/.test(hex) ? hex.toLowerCase() : '#000000';
          var txt = h('input', { class: 'input color__hex', id: id, type: 'text', maxlength: '7', spellcheck: 'false', autocomplete: 'off', disabled: ro, 'aria-describedby': described(),
            placeholder: f.required ? null : 'как у дизайнера' });
          txt.value = hex;
          // an empty optional colour is shown as empty, not as black
          var markEmpty = function () { el.classList.toggle('color--empty', !/^#[0-9A-Fa-f]{6}$/.test(txt.value)); };
          picker.addEventListener('input', function () { txt.value = picker.value.toUpperCase(); set(txt.value); markEmpty(); });
          txt.addEventListener('input', function () {
            if (/^#[0-9A-Fa-f]{6}$/.test(txt.value)) picker.value = txt.value.toLowerCase();
            set(txt.value);
            markEmpty();
          });
          api_.focusEl = txt;
          el = h('div', { class: 'color' }, picker, txt);
          markEmpty();
          break;
        case 'date':
          el = h('input', { class: 'input input--date', id: id, type: 'date', disabled: ro, 'aria-describedby': described() });
          el.value = typeof v === 'string' ? v : '';
          el.addEventListener('input', function () { set(el.value); });
          break;
        case 'image':
          el = imageControl(f, get, set, id, ro, described);
          api_.focusEl = el.axmFocus;
          break;
        default: // text, url, email
          el = h('input', { class: 'input', id: id, type: f.type === 'url' ? 'url' : f.type === 'email' ? 'email' : 'text',
            inputmode: f.type === 'url' ? 'url' : f.type === 'email' ? 'email' : null, autocomplete: 'off',
            spellcheck: f.type === 'text' && !f.pattern ? 'true' : 'false', disabled: ro, 'aria-describedby': described() });
          el.value = typeof v === 'string' ? v : (v === null || v === undefined ? '' : String(v));
          el.addEventListener('input', function () { set(el.value); });
      }
      return el;
    });
    wrap.addEventListener('focusout', function (e) { if (!wrap.contains(e.relatedTarget)) wrap.axm.blur(); });
    return wrap;
  }

  /* image: thumbnail, «Загрузить», «Убрать»; the value is the media id */
  function imageControl(f, get, set, id, ro, described) {
    var box = h('div', { class: 'img' });
    var file = h('input', { type: 'file', accept: 'image/jpeg,image/png', class: 'sr', tabindex: '-1', 'aria-hidden': 'true' });
    var up = h('button', { class: 'btn btn--soft btn--sm', type: 'button', id: id, 'aria-describedby': described() }, icon('upload', 'ico--sm'), h('span', { class: 'btn__label', text: 'Загрузить' }));
    var rm = h('button', { class: 'btn btn--ghost btn--sm', type: 'button' }, icon('close', 'ico--sm'), 'Убрать');
    var pick = h('button', { class: 'btn btn--ghost btn--sm', type: 'button' }, icon('grid', 'ico--sm'), 'Из медиатеки');
    pick.addEventListener('click', function () {
      pickMedia(!!f.icon, pick).then(function (id) { if (id) { set(id); draw(); note.textContent = 'Выбрано из медиатеки. Сохраните раздел.'; pick.focus(); } });
    });
    var note = h('p', { class: 'img__note', role: 'status' });
    box.axmFocus = up;
    function draw() {
      var val = get();
      var src = thumbUrl(val);
      var frame = h('div', { class: 'img__frame' });
      if (src) {
        var im = h('img', { class: 'img__thumb', alt: f.label ? f.label + ': текущая картинка' : 'Текущая картинка', src: src, decoding: 'async' });
        im.addEventListener('error', function () { fill(frame, h('span', { class: 'img__empty' }, icon('image'), h('span', { text: 'Превью недоступно' }))); }, { once: true });
        add(frame, im);
      } else add(frame, h('span', { class: 'img__empty' }, icon('image'), h('span', { text: val ? 'Картинка не найдена' : 'Нет картинки' })));
      $('.btn__label', up).textContent = val ? 'Заменить' : 'Загрузить';
      if (f.icon && !val) $('.btn__label', up).textContent = 'Загрузить значок';
      fill(box, frame, h('div', { class: 'img__side' },
        h('p', { class: 'img__meta' }, val ? ['Загружена · ', h('code', { text: String(val) })] : f.icon ? 'Квадратный PNG или JPEG от 512' + NB + 'px' : 'JPEG или PNG, до 12' + NB + 'МБ'),
        ro ? null : h('div', { class: 'img__btns' }, up, pick, val ? rm : null),
        note, file));
    }
    function fail(text) { note.textContent = text; note.classList.add('img__note--bad'); }
    function upload(fl) {
      note.classList.remove('img__note--bad');
      if (!fl) return;
      if (!/^image\/(jpeg|png)$/.test(fl.type)) { fail('Нужен файл JPEG или PNG'); return; }
      if (fl.size > 12 * 1024 * 1024) { fail('Файл больше 12' + NB + 'МБ — уменьшите его'); return; }
      var fd = new FormData();
      fd.append('file', fl, fl.name || 'image');
      if (f.icon) fd.append('kind', 'icon');
      working(up, true, 'Загрузка…');
      note.textContent = 'Загружаем ' + (fl.name || 'картинку') + '…';
      // api() adds X-CSRF-Token to every write, the multipart one too
      api('/site/media', { method: 'POST', form: fd, timeout: 120000 }).then(function (d) {
        working(up, false);
        if (!d || !reMediaId.test(String(d.id || ''))) { fail('Сервер ответил без картинки'); return; }
        if (typeof d.thumb === 'string' && reThumb.test(d.thumb)) MEDIA_THUMBS[d.id] = d.thumb;
        set(d.id);
        draw();
        note.textContent = 'Загружено: ' + (d.w || '?') + '×' + (d.h || '?') + NB + 'px. Сохраните раздел, чтобы картинка попала в черновик.';
        up.focus();
      }, function (e) {
        working(up, false);
        if (e.code === 'format') fail('Это не JPEG и не PNG');
        else if (e.code === 'size') fail('Файл больше 12' + NB + 'МБ — уменьшите его');
        else if (e.code === 'dimensions') fail('Картинка должна быть от 16 до 6000' + NB + 'px по каждой стороне');
        else if (e.code !== 'auth') fail(errText(e));
      });
    }
    up.addEventListener('click', function () { file.value = ''; file.click(); });
    file.addEventListener('change', function () { upload(file.files && file.files[0]); });
    rm.addEventListener('click', function () { set(''); draw(); note.textContent = 'Картинка убрана. Сохраните раздел.'; up.focus(); });
    if (!ro) {
      box.addEventListener('dragover', function (e) { e.preventDefault(); box.classList.add('is-drop'); });
      box.addEventListener('dragleave', function () { box.classList.remove('is-drop'); });
      box.addEventListener('drop', function (e) { e.preventDefault(); box.classList.remove('is-drop'); upload(e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]); });
    }
    draw();
    return box;
  }

  function renderField(f, ref, path, ctx, o) {
    o = o || {};
    if (f.type === 'tuple' || f.type === 'object') return renderGroup(f, ref, path, ctx, o);
    if (f.type === 'list') return renderList(f, ref, path, ctx, o);
    if (f.type === 'map') return renderMap(f, ref, path, ctx, o);
    if (f.type === 'blocks') return renderBlocks(f, ref, path, ctx, o);
    return renderScalar(f, ref, path, ctx, o);
  }

  // a message slot for containers (lists, groups): «нужно хотя бы 1», «неполное значение»
  function containerApi(el, msg, focusFn) {
    el.axm = {
      show: function (text) { msg.textContent = ''; add(msg, icon('alert', 'ico--xs'), h('span', { text: text })); msg.hidden = false; msg.setAttribute('role', 'alert'); el.classList.add('is-error'); },
      clear: function () { msg.hidden = true; msg.removeAttribute('role'); el.classList.remove('is-error'); },
      focus: focusFn
    };
  }

  /* tuple (positional) and object (named) fields shown as one group */
  function renderGroup(f, ref, path, ctx, o) {
    var v = ref.obj[ref.key];
    if (f.type === 'tuple' && !Array.isArray(v)) { v = defValue(f, ctx); ref.obj[ref.key] = v; }
    if (f.type === 'object' && (!v || typeof v !== 'object' || Array.isArray(v))) { v = defValue(f, ctx); ref.obj[ref.key] = v; }
    var defs = f.type === 'tuple'
      ? (f.items || []).map(function (it, i) { return [it, { obj: v, key: i }, path + '[' + i + ']']; })
      : (f.fields || []).map(function (k) {
          // a key the content lacks stays absent until it is typed in, so
          // opening a card never counts as an edit
          return [k, { obj: v, key: k.key }, path + '.' + k.key];
        });
    var kids = defs.filter(function (x) { return !x[0].hidden; }).map(function (x) { return renderField(x[0], x[1], x[2], ctx); });
    var grid = h('div', { class: 'fgrid' }, kids);
    var msg = h('p', { class: 'fld__err', hidden: true });
    var el;
    if (o.bare || !f.label) el = h('div', { class: 'fgroup' + (o.bare ? ' fgroup--bare' : ' fld--wide'), data: { path: path } }, grid, msg);
    else {
      el = h('fieldset', { class: 'fset fld--wide', data: { path: path } },
        h('legend', { class: 'fset__legend', text: f.label }),
        f.hint ? h('p', { class: 'fld__hint', text: f.hint }) : null,
        grid, msg);
    }
    containerApi(el, msg, function () { var c = focusables(el)[0]; if (c) c.focus({ preventScroll: true }); });
    return el;
  }

  /* lists: cards for structured items, lines for simple ones */
  function renderList(f, ref, path, ctx, o) {
    o = o || {};
    // a list the content lacks is attached to the model on the first change
    var arr = Array.isArray(ref.obj[ref.key]) ? ref.obj[ref.key] : (o.detached || []);
    function attach() { if (ref.obj[ref.key] !== arr) ref.obj[ref.key] = arr; }
    var item = f.item || { type: 'text' };
    var simple = !!SIMPLE[item.type];
    var fixed = !!f.fixed;
    var max = f.max !== undefined && f.max !== null ? f.max : 200;
    var min = f.min || 0;
    var editable = !ctx.ro;
    var el = h('div', { class: 'lst' + (o.bare ? ' lst--bare' : ' fld--wide'), data: { path: path } });
    var msg = h('p', { class: 'fld__err', hidden: true });

    function redraw(focus) {
      attach();
      var n = renderList(f, ref, path, ctx, o);
      el.replaceWith(n);
      ctx.changed();
      if (focus) focus(n);
      return n;
    }
    function itemEl(root, i) { return $$('[data-path]', root).filter(function (x) { return x.dataset.path === path + '[' + i + ']'; })[0]; }
    function move(i, d) {
      var j = i + d;
      if (j < 0 || j >= arr.length) return;
      var x = arr.splice(i, 1)[0];
      arr.splice(j, 0, x);
      redraw(function (n) {
        var it = itemEl(n, j);
        var btn = it && ($('[data-act="' + (d < 0 ? 'up' : 'down') + '"]', it));
        if (btn && btn.disabled) btn = $('[data-act="' + (d < 0 ? 'down' : 'up') + '"]', it);
        if (btn) btn.focus();
        toast('Пункт перемещён на место ' + (j + 1), 'check-circle');
      });
    }
    function removeAt(i) {
      var x = arr[i];
      var empty = x === '' || x === null || (simple && typeof x === 'string' && !x.trim());
      var name = simple ? (typeof x === 'string' && x.trim() ? '«' + x.trim().slice(0, 60) + (x.trim().length > 60 ? '…' : '') + '»' : 'пункт ' + (i + 1)) : '«' + itemTitle(f, x, i) + '»';
      var go = function () {
        arr.splice(i, 1);
        redraw(function (n) {
          var next = itemEl(n, Math.min(i, arr.length - 1));
          var target = next ? ($('.icard__toggle', next) || $('input,textarea,select', next)) : $('[data-act="add"]', n);
          if (target) target.focus();
        });
      };
      if (empty) { go(); return; }
      confirmDialog({ title: 'Удалить ' + name + '?', text: 'Пункт исчезнет из черновика, когда вы сохраните раздел. С сайта он пропадёт после публикации.', ok: 'Удалить', danger: true })
        .then(function (yes) { if (yes) go(); });
    }
    function actions(i, title) {
      if (!editable || fixed) return null;
      var who = title ? ' «' + title + '»' : ' ' + (i + 1);
      return h('div', { class: 'acts' },
        h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm', type: 'button', 'data-act': 'up', 'aria-label': 'Выше: пункт' + who, title: 'Выше', disabled: i === 0, onclick: function () { move(i, -1); } }, icon('arrow-up', 'ico--sm')),
        h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm', type: 'button', 'data-act': 'down', 'aria-label': 'Ниже: пункт' + who, title: 'Ниже', disabled: i === arr.length - 1, onclick: function () { move(i, 1); } }, icon('arrow-down', 'ico--sm')),
        h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm acts__del', type: 'button', 'data-act': 'del', 'aria-label': 'Удалить пункт' + who, title: arr.length <= min ? 'Нужно хотя бы ' + min : 'Удалить', disabled: arr.length <= min, onclick: function () { removeAt(i); } }, icon('trash', 'ico--sm')));
    }

    if (!o.bare && f.label) {
      add(el, h('div', { class: 'lst__head' }, h('h3', { class: 'lst__title', text: f.label })), f.hint ? h('p', { class: 'fld__hint', text: f.hint }) : null);
    }
    var items = h(simple ? 'ol' : 'div', { class: simple ? 'lines' : 'icards' });
    arr.forEach(function (x, i) {
      var p = path + '[' + i + ']';
      if (simple) {
        var fld = renderScalar(item, { obj: arr, key: i, attach: attach }, p, ctx, { label: '', sr: (f.label || 'Пункт') + ', ' + (i + 1), compact: true, noHint: i > 0, rows: '1' });
        add(items, h('li', { class: 'line' }, h('span', { class: 'line__n num', 'aria-hidden': 'true', text: String(i + 1) }), fld, actions(i, '')));
        return;
      }
      add(items, itemCard(f, item, arr, i, p, ctx, actions, attach));
    });
    add(el, items);
    if (!arr.length) add(el, h('p', { class: 'lst__empty', text: editable && !fixed ? 'Пока пусто. Добавьте первый пункт.' : 'Пусто' }));
    var foot = h('div', { class: 'lst__foot' });
    if (editable && !fixed) {
      var addBtn = h('button', { class: 'btn btn--soft btn--sm', type: 'button', 'data-act': 'add', disabled: arr.length >= max, title: arr.length >= max ? 'Не больше ' + max : null }, icon('plus', 'ico--sm'), 'Добавить');
      addBtn.addEventListener('click', function () {
        var nv = defValue(item, ctx);
        arr.push(nv);
        if (nv && typeof nv === 'object') ctx.open.set(nv, true);
        var at = arr.length - 1;
        redraw(function (n) {
          var it = itemEl(n, at);
          if (!it) return;
          it.scrollIntoView({ block: 'nearest', behavior: reduceMotion.matches ? 'auto' : 'smooth' });
          var c = $('.icard__body input:not([disabled]), .icard__body textarea:not([disabled]), .icard__body select:not([disabled])', it) || $('input,textarea,select', it);
          if (c) c.focus({ preventScroll: true });
        });
      });
      add(foot, addBtn);
    }
    if (!fixed || arr.length > 3) add(foot, h('span', { class: 'lst__count num', text: fixed ? num(arr.length) : num(arr.length) + NB + 'из' + NB + num(max) }));
    if (foot.firstChild) add(el, foot);
    add(el, msg);
    containerApi(el, msg, function () { var c = $('[data-act="add"]', el) || focusables(el)[0]; if (c) c.focus({ preventScroll: true }); });
    return el;
  }

  // one structured list item: a collapsible card; its body is drawn on first open
  function itemCard(f, item, arr, i, p, ctx, actions, attach) {
    var x = arr[i];
    var key = x && typeof x === 'object' ? x : null;
    var open = key && ctx.open.has(key) ? ctx.open.get(key)
      : ctx.openInit && ctx.openInit.has(p) ? true
      : !(item.type === 'object' || arr.length > 4);
    if (key) ctx.open.set(key, open);
    var bid = nextId('ic');
    var title = itemTitle(f, x, i);
    var titleEl = h('span', { class: 'icard__title', text: title });
    var toggle = h('button', { class: 'icard__toggle', type: 'button', 'aria-expanded': String(open), 'aria-controls': bid },
      icon('chevron-down', 'ico--sm icard__chev'),
      h('span', { class: 'icard__n num', 'aria-hidden': 'true', text: (i + 1 < 10 ? '0' : '') + (i + 1) }),
      titleEl,
      item.type === 'object' ? itemMeta(item, x) : null);
    var body = h('div', { class: 'icard__body', id: bid, role: 'group', 'aria-label': title, hidden: !open });
    var card = h('div', { class: 'icard' + (open ? ' is-open' : ''), data: { path: p } }, h('div', { class: 'icard__head' }, toggle, actions(i, title)), body);
    function drawBody() { if (!body.firstChild) fill(body, renderField(item, { obj: arr, key: i, attach: attach }, p, ctx, { bare: true })); }
    function setOpen(on) {
      open = on;
      if (key) ctx.open.set(key, on);
      if (on) drawBody();
      body.hidden = !on;
      card.classList.toggle('is-open', on);
      toggle.setAttribute('aria-expanded', String(on));
      if (on) requestAnimationFrame(function () { autosizeIn(body); });
    }
    toggle.addEventListener('click', function () { setOpen(!open); });
    if (open) drawBody();
    // the card title follows the field it is made of
    body.addEventListener('input', function () { var t = itemTitle(f, arr[i], i); titleEl.textContent = t; body.setAttribute('aria-label', t); });
    card.axm = { open: function () { setOpen(true); }, isOpen: function () { return open; } };
    return card;
  }
  // small facts on a collapsed card: the address key and a «на главной» mark
  function itemMeta(item, x) {
    if (!x || typeof x !== 'object') return null;
    var bits = [];
    (item.fields || []).forEach(function (k) {
      if (k.key === 'slug' && typeof x.slug === 'string' && x.slug) bits.push('/' + x.slug);
      if (k.type === 'toggle' && x[k.key] === true && /главн|популяр/i.test(k.label || '')) bits.push(/популяр/i.test(k.label) ? 'популярный' : 'на главной');
      if (k.type === 'date' && typeof x[k.key] === 'string' && x[k.key]) bits.push(plainDate(x[k.key]));
    });
    return bits.length ? h('span', { class: 'icard__meta', text: bits.join(' · ') }) : null;
  }

  /* map with fixed keys: one row per key, the key is the label */
  function renderMap(f, ref, path, ctx, o) {
    var m = ref.obj[ref.key];
    if (!m || typeof m !== 'object' || Array.isArray(m)) { m = {}; ref.obj[ref.key] = m; }
    var item = f.item || { type: 'text' };
    var rows = Object.keys(m).map(function (k) { return renderField(item, { obj: m, key: k }, path + '.' + k, ctx, { label: k, keyLabel: true, noHint: true }); });
    var msg = h('p', { class: 'fld__err', hidden: true });
    var el = h('div', { class: 'lst' + (o.bare ? ' lst--bare' : ' fld--wide'), data: { path: path } },
      !o.bare && f.label ? h('div', { class: 'lst__head' }, h('h3', { class: 'lst__title', text: f.label })) : null,
      h('p', { class: 'fld__hint', text: f.fixedKeys ? 'Ключи связаны с сайтом и не меняются — правятся только подписи.' : (f.hint || '') }),
      h('div', { class: 'fgrid fgrid--map' }, rows),
      rows.length ? null : h('p', { class: 'lst__empty', text: 'Пусто' }),
      msg);
    containerApi(el, msg, function () { var c = focusables(el)[0]; if (c) c.focus({ preventScroll: true }); });
    return el;
  }

  /* article body: paragraphs, subheadings and lists */
  function renderBlocks(f, ref, path, ctx, o) {
    var arr = Array.isArray(ref.obj[ref.key]) ? ref.obj[ref.key] : (o.detached || []);
    function attach() { if (ref.obj[ref.key] !== arr) ref.obj[ref.key] = arr; }
    var editable = !ctx.ro;
    var max = f.max !== undefined && f.max !== null ? f.max : 200;
    var min = f.min || 0;
    var msg = h('p', { class: 'fld__err', hidden: true });
    var el = h('div', { class: 'lst blocks' + (o.bare ? ' lst--bare' : ' fld--wide'), data: { path: path } });
    function redraw(focus) { attach(); var n = renderBlocks(f, ref, path, ctx, Object.assign({}, o, { detached: arr })); el.replaceWith(n); ctx.changed(); if (focus) focus(n); return n; }
    function blockEl(root, i) { return $$('.blk', root).filter(function (x) { return x.dataset.path === path + '[' + i + ']'; })[0]; }
    function textOf(bl) { return Array.isArray(bl[1]) ? bl[1].join('\n') : typeof bl[1] === 'string' ? bl[1] : ''; }
    function convert(i, kind) {
      var bl = arr[i];
      if (bl[0] === kind) return;
      var t = textOf(bl);
      if (kind === 'ul') bl[1] = t.split('\n').map(function (s) { return s.trim(); }).filter(Boolean);
      else bl[1] = kind === 'h2' ? t.replace(/\s*\n\s*/g, ' ') : t;
      bl[0] = kind;
      redraw(function (n) { var be = blockEl(n, i); var btn = be && $('[data-kind="' + kind + '"]', be); if (btn) btn.focus(); });
    }
    function move(i, d) {
      var j = i + d;
      if (j < 0 || j >= arr.length) return;
      arr.splice(j, 0, arr.splice(i, 1)[0]);
      redraw(function (n) {
        var be = blockEl(n, j);
        var btn = be && $('[data-act="' + (d < 0 ? 'up' : 'down') + '"]', be);
        if (btn && btn.disabled) btn = $('[data-act="' + (d < 0 ? 'down' : 'up') + '"]', be);
        if (btn) btn.focus();
      });
    }
    function removeAt(i) {
      var go = function () {
        arr.splice(i, 1);
        redraw(function (n) { var be = blockEl(n, Math.min(i, arr.length - 1)); var c = be ? $('textarea,input', be) : $('[data-add]', n); if (c) c.focus(); });
      };
      if (!textOf(arr[i]).trim()) { go(); return; }
      confirmDialog({ title: 'Удалить блок ' + (i + 1) + '?', text: 'Текст блока исчезнет из статьи, когда вы сохраните раздел.', ok: 'Удалить', danger: true }).then(function (yes) { if (yes) go(); });
    }
    function addBlock(kind) {
      arr.push(kind === 'ul' ? ['ul', ['']] : [kind, '']);
      var at = arr.length - 1;
      redraw(function (n) { var be = blockEl(n, at); if (be) { be.scrollIntoView({ block: 'nearest' }); var c = $('textarea,input', be); if (c) c.focus({ preventScroll: true }); } });
    }
    var list = h('div', { class: 'blks' });
    arr.forEach(function (bl, i) {
      if (!Array.isArray(bl)) { bl = ['p', String(bl || '')]; arr[i] = bl; }
      var p = path + '[' + i + ']';
      var kind = bl[0];
      var known = kind === 'p' || kind === 'h2' || kind === 'ul' || kind === 'quote';
      var seg = h('div', { class: 'seg seg--sm', role: 'group', 'aria-label': 'Тип блока ' + (i + 1) },
        BLOCK_KINDS.map(function (k) {
          return h('button', { class: 'seg__opt', type: 'button', 'data-kind': k[0], 'aria-pressed': String(kind === k[0]), disabled: !editable, onclick: function () { convert(i, k[0]); } }, icon(k[2], 'ico--xs'), h('span', { class: 'seg__text', text: k[1] }));
        }));
      var id = nextId('b');
      var ctl;
      if (kind === 'h2') {
        ctl = h('input', { class: 'input blk__h2', id: id, type: 'text', disabled: !editable, 'aria-label': 'Подзаголовок, блок ' + (i + 1) });
        ctl.value = typeof bl[1] === 'string' ? bl[1] : '';
        ctl.addEventListener('input', function () { attach(); bl[1] = ctl.value; wrapApi.changed(); ctx.changed(); });
      } else {
        ctl = h('textarea', { class: 'textarea fld__ta', id: id, rows: '2', disabled: !editable,
          'aria-label': (kind === 'ul' ? 'Список, каждый пункт с новой строки' : kind === 'p' ? 'Абзац' : kind === 'quote' ? 'Цитата' : 'Текст') + ', блок ' + (i + 1) });
        ctl.value = textOf(bl);
        ctl.addEventListener('input', function () {
          attach();
          if (kind === 'ul') bl[1] = ctl.value.split('\n');
          else if (known) bl[1] = ctl.value;
          autosize(ctl); wrapApi.changed(); ctx.changed();
        });
        requestAnimationFrame(function () { autosize(ctl); });
      }
      var bmsg = h('p', { class: 'fld__err', hidden: true });
      var be = h('div', { class: 'blk blk--' + (known ? kind : 'other'), data: { path: p } },
        h('div', { class: 'blk__head' },
          h('span', { class: 'icard__n num', 'aria-hidden': 'true', text: (i + 1 < 10 ? '0' : '') + (i + 1) }),
          seg,
          editable ? h('div', { class: 'acts' },
            h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm', type: 'button', 'data-act': 'up', 'aria-label': 'Выше: блок ' + (i + 1), title: 'Выше', disabled: i === 0, onclick: function () { move(i, -1); } }, icon('arrow-up', 'ico--sm')),
            h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm', type: 'button', 'data-act': 'down', 'aria-label': 'Ниже: блок ' + (i + 1), title: 'Ниже', disabled: i === arr.length - 1, onclick: function () { move(i, 1); } }, icon('arrow-down', 'ico--sm')),
            h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm acts__del', type: 'button', 'data-act': 'del', 'aria-label': 'Удалить блок ' + (i + 1), title: 'Удалить', disabled: arr.length <= min, onclick: function () { removeAt(i); } }, icon('trash', 'ico--sm'))) : null),
        known ? null : h('p', { class: 'note note--warn' }, icon('alert', 'ico--sm'), h('span', { text: 'Блок типа «' + String(kind) + '» сайт не принимает. Выберите «Абзац», «Подзаголовок», «Список» или «Цитата» — текст сохранится.' })),
        ctl,
        kind === 'ul' ? h('p', { class: 'fld__hint', text: 'Каждый пункт списка — с новой строки' }) : null,
        bmsg);
      var wrapApi = {
        changed: function () { if (!bmsg.hidden) { bmsg.hidden = true; be.classList.remove('is-error'); } }
      };
      be.axm = {
        show: function (text) { bmsg.textContent = ''; add(bmsg, icon('alert', 'ico--xs'), h('span', { text: text })); bmsg.hidden = false; bmsg.setAttribute('role', 'alert'); be.classList.add('is-error'); },
        clear: function () { bmsg.hidden = true; be.classList.remove('is-error'); },
        focus: function () { ctl.focus({ preventScroll: true }); }
      };
      add(list, be);
    });
    if (!o.bare && f.label) add(el, h('div', { class: 'lst__head' }, h('h3', { class: 'lst__title', text: f.label })));
    add(el, list);
    if (!arr.length) add(el, h('p', { class: 'lst__empty', text: 'В статье пока нет текста.' }));
    if (editable) {
      add(el, h('div', { class: 'lst__foot' },
        BLOCK_KINDS.map(function (k) {
          return h('button', { class: 'btn btn--soft btn--sm', type: 'button', 'data-add': k[0], disabled: arr.length >= max, onclick: function () { addBlock(k[0]); } }, icon('plus', 'ico--sm'), k[1]);
        }),
        h('span', { class: 'lst__count num', text: num(arr.length) + NB + plural(arr.length, ['блок', 'блока', 'блоков']) })));
    }
    add(el, msg);
    containerApi(el, msg, function () { var c = $('textarea,input,button', el); if (c) c.focus({ preventScroll: true }); });
    return el;
  }

  function viewEditor(root, r, token) {
    if (!S.site) {
      fill(root, S.siteErr ? errorCard(S.siteErr, function () { S.siteErr = null; render(viewEditor, r); }) : h('div', { class: 'card' }, h('div', { class: 'skel skel--line' }), h('div', { class: 'skel', vars: { height: '200px', 'margin-top': '16px' } })));
      if (!S.siteErr) loadSite().then(function () { if (alive(token)) render(viewEditor, r); }, function (e) { if (alive(token) && e.code !== 'auth') { S.siteErr = e; render(viewEditor, r); } });
      return;
    }
    var sec = findSection(r.section);
    if (!sec) { viewMissing(root); return; }
    var ro = !can('site');
    var ctx = { ro: ro, open: new WeakMap(), openInit: null, values: null, changed: null };
    var values, baseJSON, serverJSON, version, saving = false, conflict = false, retries = 0;
    var formId = nextId('edform');

    function fromContent(content) {
      var o = {};
      sec.fields.forEach(function (f) { var v = getPath(content, f.key); o[f.key] = v === undefined ? defValue(f, ctx) : clone(v); });
      return o;
    }
    function picked(vals) { var o = {}; sec.fields.forEach(function (f) { o[f.key] = pick(f, vals[f.key]); }); return o; }
    function reset(content, ver) {
      values = fromContent(content);
      ctx.values = values;
      baseJSON = JSON.stringify(picked(values));
      serverJSON = baseJSON;
      version = ver;
    }
    function dirty() { return !ro && JSON.stringify(picked(values)) !== baseJSON; }
    reset(S.site.content, S.site.version);

    // head
    var state = h('span', { class: 'ed-state' });
    function syncState() {
      var st = S.site;
      clear(state);
      if (!st || !st.current) return;
      var ch = st.changed.indexOf(sec.id) >= 0;
      add(state, h('span', { class: 'pill ' + (ch ? 'st--proposal' : 'st--won'), text: ch ? 'Есть неопубликованные правки' : 'Как на сайте' }));
    }
    var head = h('div', { class: 'page-head ed-head' },
      h('div', { class: 'page-head__text' },
        sec.where ? h('p', { class: 'ed-where' }, icon(secIcon(sec.icon), 'ico--xs'), sec.where) : null,
        h('h1', { class: 'page-title', text: sec.title }),
        sec.hint ? h('p', { class: 'page-sub ed-hint', text: sec.hint }) : null),
      h('div', { class: 'page-actions' }, state, ro ? h('span', { class: 'ro-hint' }, icon('eye', 'ico--xs'), 'Только просмотр') : null));

    var form = h('form', { class: 'ed-form', id: formId, novalidate: true, 'aria-label': sec.title });
    // blocks of a kind the site no longer takes are listed up front: the
    // server names only the first one per save
    var problems = h('div', { class: 'ed-problems', hidden: true });
    function walkOdd(f, v, path, trail, out) {
      if (!f || v === undefined || v === null) return;
      if (f.type === 'object' && typeof v === 'object') (f.fields || []).forEach(function (k) { walkOdd(k, v[k.key], path + '.' + k.key, trail, out); });
      else if (f.type === 'tuple' && Array.isArray(v)) (f.items || []).forEach(function (k, i) { walkOdd(k, v[i], path + '[' + i + ']', trail, out); });
      else if (f.type === 'list' && Array.isArray(v)) v.forEach(function (x, i) { walkOdd(f.item, x, path + '[' + i + ']', '«' + itemTitle(f, x, i) + '»', out); });
      else if (f.type === 'blocks' && Array.isArray(v)) v.forEach(function (bl, i) {
        if (!Array.isArray(bl) || ['p', 'h2', 'ul', 'quote'].indexOf(bl[0]) < 0) out.push({ path: path + '[' + i + ']', text: (trail ? trail + ', ' : '') + 'блок ' + (i + 1) });
      });
    }
    function syncProblems() {
      var out = [];
      sec.fields.forEach(function (f) { walkOdd(f, values[f.key], f.key, '', out); });
      problems.hidden = !out.length;
      if (!out.length) { clear(problems); return; }
      fill(problems, h('p', { class: 'note note--warn' }, icon('alert', 'ico--sm'),
        h('span', null, 'Раздел не сохранится, пока в нём есть блоки, которые сайт больше не принимает: ', b(String(out.length)), '. Смените их тип на «Абзац», «Подзаголовок», «Список» или «Цитата»\u00A0— текст останется.')),
        h('ul', { class: 'chiplist' }, out.slice(0, 30).map(function (x) {
          return h('li', null, h('button', { class: 'chip chip--link', type: 'button', onclick: function () {
            var t = revealPath(x.path);
            if (t) { t.scrollIntoView({ block: 'center', behavior: reduceMotion.matches ? 'auto' : 'smooth' }); t.axm.focus(); }
          } }, icon('arrow-down', 'ico--xs'), x.text));
        })));
    }
    form.addEventListener('submit', function (e) { e.preventDefault(); save(); });

    function card(title, hint, kids, i) {
      return rise(h('section', { class: 'card ed-card' },
        title ? h('div', { class: 'ed-card__head' }, h('h2', { class: 'ed-card__title', text: title }), hint ? h('p', { class: 'ed-card__hint', text: hint }) : null) : null,
        kids), i);
    }
    function renderForm() {
      var cards = [], run = [], i = 0;
      function flush() { if (run.length) { cards.push(card(null, null, h('div', { class: 'fgrid' }, run), i++)); run = []; } }
      sec.fields.forEach(function (f) {
        if (f.hidden) return;
        var ref = { obj: values, key: f.key };
        if (SIMPLE[f.type]) { run.push(renderScalar(f, ref, f.key, ctx)); return; }
        flush();
        cards.push(card(f.label, f.hint, renderField(f, ref, f.key, ctx, { bare: true }), i++));
      });
      flush();
      fill(form, cards);
      syncProblems();
      requestAnimationFrame(function () { autosizeIn(form); });
    }

    // the sticky bar
    var barDot = h('span', { class: 'savebar__dot', 'aria-hidden': 'true' });
    var barText = h('span', { class: 'savebar__text' });
    var barStatus = h('div', { class: 'savebar__state', role: 'status' }, barDot, barText);
    var barErr = h('p', { class: 'savebar__err', hidden: true });
    var reloadBtn = h('button', { class: 'btn btn--ghost', type: 'button', hidden: true }, icon('refresh', 'ico--sm'), 'Обновить');
    var revertBtn = h('button', { class: 'btn btn--soft', type: 'button', hidden: true }, icon('undo', 'ico--sm'), h('span', { class: 'savebar__long', text: 'Отменить правки' }), h('span', { class: 'savebar__short', text: 'Отменить' }));
    var saveBtn = h('button', { class: 'btn btn--primary', type: 'submit', form: formId },
      icon('save', 'ico--sm'), h('span', { class: 'btn__label', text: 'Сохранить' }), h('span', { class: 'kbd kbd--inv', 'aria-hidden': 'true', text: isMac ? '⌘S' : 'Ctrl+S' }));
    var bar = ro ? null : h('div', { class: 'savebar' }, h('div', { class: 'savebar__main' }, barStatus, h('div', { class: 'savebar__btns' }, reloadBtn, revertBtn, saveBtn)), barErr);
    var lastText = '';
    function syncBar() {
      if (!bar) return;
      var d = dirty(), text, tone;
      if (saving) { text = 'Сохраняем…'; tone = 'busy'; }
      else if (conflict) { text = 'Раздел только что изменил кто-то ещё'; tone = 'warn'; }
      else if (d) { text = 'Есть несохранённые правки'; tone = 'dirty'; }
      else { text = 'Все изменения сохранены в черновик'; tone = 'ok'; }
      bar.className = 'savebar savebar--' + tone;
      if (text !== lastText) { barText.textContent = text; lastText = text; }
      revertBtn.hidden = !d || saving;
      reloadBtn.hidden = !conflict;
      if (!saving) saveBtn.disabled = !d;
    }
    var changedSoon = debounce(function () { syncBar(); syncProblems(); }, 120);
    ctx.changed = function () { if (!barErr.hidden && !conflict) barErr.hidden = true; changedSoon(); };

    function clearErrors() { $$('.is-error[data-path]', form).forEach(function (x) { if (x.axm && x.axm.clear) x.axm.clear(); }); barErr.hidden = true; }
    function byPath(p) { return $$('[data-path]', form).filter(function (x) { return x.dataset.path === p && x.axm && x.axm.show; })[0] || null; }
    function parentPath(p) { var m = /^(.*)(\[\d+\]|\.[^.\[\]]+)$/.exec(p); return m ? m[1] : ''; }
    function revealPath(p) {
      for (var guard = 0; guard < 16; guard++) {
        var hit = byPath(p);
        if (hit) {
          // a field inside a collapsed card: open every card around it
          for (var c = hit.closest('.icard'); c; c = c.parentElement && c.parentElement.closest('.icard')) { if (c.axm && !c.axm.isOpen()) c.axm.open(); }
          return hit;
        }
        var best = null;
        $$('.icard', form).forEach(function (c) {
          var cp = c.dataset.path;
          if ((p.indexOf(cp + '.') === 0 || p.indexOf(cp + '[') === 0) && (!best || cp.length > best.dataset.path.length)) best = c;
        });
        if (best && best.axm && !best.axm.isOpen()) { best.axm.open(); continue; }
        break;
      }
      for (var q = parentPath(p); q; q = parentPath(q)) { var up = byPath(q); if (up) return up; }
      return null;
    }
    function sectionOfPath(p) {
      var best = null, bestLen = -1;
      (S.site.schema.sections || []).forEach(function (s2) {
        s2.fields.forEach(function (f) {
          if ((p === f.key || p.indexOf(f.key + '.') === 0 || p.indexOf(f.key + '[') === 0) && f.key.length > bestLen) { best = s2; bestLen = f.key.length; }
        });
      });
      return best;
    }
    function showServerError(p, message) {
      var text = message ? message.charAt(0).toUpperCase() + message.slice(1) : 'Сервер не принял это значение';
      var other = p ? sectionOfPath(p) : null;
      var target = p && (!other || other.id === sec.id) ? revealPath(p) : null;
      if (target) {
        target.axm.show(text);
        target.scrollIntoView({ block: 'center', behavior: reduceMotion.matches ? 'auto' : 'smooth' });
        target.axm.focus();
        barErr.textContent = '';
        add(barErr, icon('alert', 'ico--xs'), h('span', { text: 'Не сохранено: ' + text.charAt(0).toLowerCase() + text.slice(1) + '. Поле отмечено выше.' }));
      } else {
        // a value in another section points at what this save changes (a review at a case)
        barErr.textContent = '';
        add(barErr, icon('alert', 'ico--xs'),
          h('span', { text: 'Не сохранено: ' + (other && other.id !== sec.id ? 'в разделе «' + other.title + '» ' : '') + text.charAt(0).toLowerCase() + text.slice(1) + '.' }),
          other && other.id !== sec.id ? h('a', { class: 'link', href: '#/edit/' + encodeURIComponent(other.id), text: 'Открыть «' + other.title + '»' }) : null);
      }
      barErr.hidden = false;
    }

    function save() {
      if (ro || saving) return Promise.resolve(false);
      if (!dirty() && !conflict) return Promise.resolve(true);
      clearErrors();
      saving = true;
      working(saveBtn, true, 'Сохраняем…');
      syncBar();
      var sent = picked(values), sentJSON = JSON.stringify(sent);
      return api('/site/sections/' + encodeURIComponent(sec.id), { method: 'PUT', body: { version: version, values: sent } }).then(function (st) {
        saving = false; conflict = false; retries = 0;
        working(saveBtn, false);
        baseJSON = sentJSON;
        version = st.version;
        serverJSON = JSON.stringify(picked(fromContent(st.content)));
        setSite(st);
        syncBar();
        toast('Сохранено в черновик', 'check-circle', 'ok');
        return true;
      }, function (e) {
        saving = false;
        working(saveBtn, false);
        if (e.code === 'conflict') return onConflict();
        syncBar();
        if (e.code === 'invalid') { showServerError(e.field, e.data && e.data.message); return false; }
        if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
        return false;
      });
    }
    // Someone saved the draft in between. When this section is as we left
    // it (they edited another one), save again on top; otherwise ask.
    function onConflict() {
      return loadSite().then(function () {
        var now = JSON.stringify(picked(fromContent(S.site.content)));
        if (now === serverJSON && retries < 2) { retries++; version = S.site.version; return save(); }
        conflict = true;
        syncBar();
        barErr.textContent = '';
        add(barErr, icon('alert', 'ico--xs'), h('span', { text: 'Пока вы правили, этот раздел сохранил другой человек. «Обновить» покажет его версию; ваши несохранённые правки при этом пропадут.' }));
        barErr.hidden = false;
        return false;
      }, function (e) { if (e.code !== 'auth') toast(errText(e), 'alert', 'error'); syncBar(); return false; });
    }
    function reloadFromServer() {
      var go = function () {
        conflict = false;
        reset(S.site.content, S.site.version);
        renderForm();
        barErr.hidden = true;
        syncBar();
        toast('Показана последняя версия раздела', 'refresh');
      };
      if (dirty()) confirmDialog({ title: 'Обновить раздел?', text: 'Ваши несохранённые правки в разделе «' + sec.title + '» пропадут.', ok: 'Обновить', cancel: 'Отмена', danger: true, icon: 'refresh' }).then(function (y) { if (y) go(); });
      else go();
    }
    function revert() {
      confirmDialog({ title: 'Отменить правки?', text: 'Раздел вернётся к последнему сохранённому состоянию.', ok: 'Отменить правки', cancel: 'Оставить', danger: true, icon: 'undo' }).then(function (y) {
        if (!y) return;
        values = JSON.parse(baseJSON);
        // keys the server keeps but we never send stay as loaded
        sec.fields.forEach(function (f) { if (values[f.key] === undefined) values[f.key] = defValue(f, ctx); });
        ctx.values = values;
        renderForm();
        clearErrors();
        syncBar();
      });
    }
    reloadBtn.addEventListener('click', reloadFromServer);
    revertBtn.addEventListener('click', revert);

    fill(root, h('div', { class: 'ed' }, head, problems, form, bar));
    renderForm();
    syncProblems();
    syncState();
    syncBar();
    S.onSite = function () { if (alive(token)) syncState(); };
    S.editor = {
      id: sec.id, title: sec.title, hash: '#/edit/' + encodeURIComponent(sec.id),
      dirty: dirty, save: save,
      discard: function () { baseJSON = JSON.stringify(picked(values)); }
    };
  }

  /* ------------------------------------------------------------------ site: releases */

  function viewReleases(root, r, token) {
    var list = h('div', { class: 'rels', 'aria-busy': 'true' }, [0, 1, 2].map(function () {
      return h('div', { class: 'card rel' }, h('div', { class: 'skel skel--line', vars: { width: '40%' } }), h('div', { class: 'skel skel--line', vars: { width: '70%', 'margin-top': '12px' } }));
    }));
    var intro = h('p', { class: 'note' }, icon('shield', 'ico--sm'),
      h('span', { text: can('restore')
        ? 'Каждая публикация сохраняется целиком. Если что-то пошло не так, верните сайт к одной из последних версий — это займёт секунду.'
        : 'Каждая публикация сохраняется целиком. Вернуть сайт к прошлой версии может владелец.' }));
    fill(root, pageHead('Резервные копии', 'Версии сайта после каждой публикации'), intro, list);

    function load() {
      api('/site/releases').then(function (d) {
        if (!alive(token)) return;
        list.removeAttribute('aria-busy');
        var items = d.items || [];
        if (!items.length) {
          fill(list, h('div', { class: 'empty fade' },
            h('span', { class: 'empty__art' }, icon('history')),
            h('h2', { class: 'empty__title', text: 'Публикаций пока нет' }),
            h('p', { class: 'empty__text', text: 'После первой публикации здесь появится копия сайта, к которой можно вернуться.' })));
          return;
        }
        fill(list, items.map(function (rel, i) {
          var who = rel.by && rel.by.name;
          var restore = can('restore') && !rel.current
            ? h('button', { class: 'btn btn--ghost btn--sm', type: 'button', onclick: function (e) { restoreRelease(rel, e.currentTarget); } }, icon('history', 'ico--sm'), h('span', { class: 'btn__label', text: 'Вернуть эту версию' }))
            : null;
          return rise(h('article', { class: 'card rel' + (rel.current ? ' rel--current' : '') },
            h('div', { class: 'rel__main' },
              h('div', { class: 'rel__top' },
                h('h2', { class: 'rel__time' }, h('time', { datetime: rel.time || '', text: fullTime(rel.time) })),
                rel.current ? h('span', { class: 'pill st--won', text: 'на сайте сейчас' }) : null),
              h('p', { class: 'rel__note' + (rel.note ? '' : ' rel__note--empty'), text: rel.note ? '«' + rel.note + '»' : 'Без описания' }),
              h('p', { class: 'rel__by' }, rel.by ? avatar(rel.by, 'sm') : null, h('span', { text: who ? who : 'Неизвестно кто' }), h('span', { class: 'muted', text: ' · ' + relTime(rel.time) }))),
            restore), Math.min(i, 10));
        }));
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        list.removeAttribute('aria-busy');
        fill(list, errorCard(e, load));
      });
    }
    function restoreRelease(rel, btn) {
      confirmDialog({
        title: 'Вернуть версию от ' + fullTime(rel.time) + '?',
        text: 'Сайт сразу станет таким, как в этой версии, а черновик заменится её содержимым. Несохранённые и неопубликованные правки пропадут.',
        ok: 'Вернуть эту версию', cancel: 'Отмена', danger: true, icon: 'history'
      }).then(function (yes) {
        if (!yes) return;
        working(btn, true, 'Возвращаем…');
        api('/site/releases/' + encodeURIComponent(rel.id) + '/restore', { method: 'POST', body: {} }).then(function (st) {
          setSite(st);
          toast('Сайт возвращён к версии от ' + fullTime(rel.time), 'history', 'ok');
          if (alive(token)) load();
        }, function (e) {
          working(btn, false);
          if (e.code === 'not_found') { toast('Этой версии уже нет', 'alert', 'error'); load(); }
          else if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
        });
      });
    }
    load();
  }

  /* ------------------------------------------------------------------ site: media library */

  // thumbnails of every uploaded picture, so icons and small pictures show
  // in the editor after a reload too
  function loadThumbs() {
    if (!S.features.site) return Promise.resolve([]);
    return api('/site/media').then(function (d) {
      var items = (d && d.items) || [];
      items.forEach(function (it) { if (reMediaId.test(it.id) && reThumb.test(String(it.thumb || ''))) MEDIA_THUMBS[it.id] = it.thumb; });
      return items;
    }, function () { return []; });
  }
  function mediaName(it) { return it.name || ('Картинка ' + it.id.slice(0, 6)); }

  // a dialog to choose an uploaded picture; resolves with its id or null
  function pickMedia(iconOnly, returnTo) {
    return new Promise(function (resolve) {
      var grid = h('div', { class: 'mlib mlib--pick', 'aria-busy': 'true' }, h('div', { class: 'skel skel--line', vars: { width: '60%' } }));
      var chosen = null;
      var m = modal({ title: 'Медиатека', icon: 'image', wide: true, returnTo: returnTo, body: [
        h('p', { class: 'dialog__text', text: iconOnly ? 'Значки, загруженные раньше. Новый — кнопкой «Загрузить значок».' : 'Картинки, загруженные раньше. Нажмите на нужную.' }), grid],
        foot: [h('button', { class: 'btn btn--soft', type: 'button', onclick: function () { m.close(); } }, 'Отмена')],
        onClose: function () { resolve(chosen); } });
      loadThumbs().then(function (items) {
        grid.removeAttribute('aria-busy');
        items = items.filter(function (it) { return iconOnly ? it.icon : true; });
        if (!items.length) { fill(grid, h('p', { class: 'muted', text: iconOnly ? 'Значков пока нет.' : 'Картинок пока нет.' })); return; }
        fill(grid, items.map(function (it) {
          return h('button', { class: 'mlib__item', type: 'button', onclick: function () { chosen = it.id; m.close(); } },
            h('span', { class: 'mlib__frame' }, it.thumb ? h('img', { src: it.thumb, alt: '', decoding: 'async', loading: 'lazy' }) : icon('image')),
            h('span', { class: 'mlib__name', text: mediaName(it) }),
            h('span', { class: 'mlib__meta', text: it.w + '×' + it.h }));
        }));
      });
    });
  }

  function viewMedia(root, r, token) {
    var file = h('input', { type: 'file', accept: 'image/jpeg,image/png', class: 'sr', tabindex: '-1', 'aria-hidden': 'true', multiple: true });
    var up = can('site') ? h('button', { class: 'btn btn--primary', type: 'button' }, icon('upload', 'ico--sm'), h('span', { class: 'btn__label', text: 'Загрузить' })) : null;
    var filterBox = h('div', { class: 'seg', role: 'group', 'aria-label': 'Показать' });
    var grid = h('div', { class: 'mlib', 'aria-busy': 'true' }, [0, 1, 2, 3].map(function () { return h('div', { class: 'card mlib__card' }, h('div', { class: 'skel', vars: { height: '120px' } })); }));
    var status = h('p', { class: 'sr', role: 'status' });
    var intro = h('p', { class: 'note' }, icon('image', 'ico--sm'), h('span', { text: 'Все картинки, загруженные в панель. Удалить можно только ту, что нигде не используется — ни в черновике, ни в сохранённых копиях сайта.' }));
    fill(root, pageHead('Медиатека', 'Картинки сайта и где они стоят', [up, file]), intro, filterBox, status, grid);
    var all = [], mode = 'all';
    var MODES = [['all', 'Все'], ['used', 'Используются'], ['free', 'Не используются']];
    function drawFilter() {
      fill(filterBox, MODES.map(function (x) {
        return h('button', { class: 'seg__opt', type: 'button', 'aria-pressed': String(mode === x[0]), onclick: function () { mode = x[0]; drawFilter(); draw(); } }, x[1]);
      }));
    }
    function draw() {
      var items = all.filter(function (it) { return mode === 'all' || (mode === 'used') === (it.uses.length > 0 || it.inRelease); });
      status.textContent = items.length + ' ' + plural(items.length, ['картинка', 'картинки', 'картинок']);
      if (!items.length) {
        fill(grid, h('div', { class: 'empty fade' }, h('span', { class: 'empty__art' }, icon('image')),
          h('h2', { class: 'empty__title', text: all.length ? 'Здесь пусто' : 'Картинок пока нет' }),
          h('p', { class: 'empty__text', text: all.length ? 'Под этот фильтр ничего не подходит.' : 'Загрузите картинку здесь или прямо в разделе, где она нужна.' })));
        return;
      }
      fill(grid, items.map(function (it, i) {
        var used = it.uses.length > 0;
        var del = can('site') ? h('button', { class: 'btn btn--ghost btn--sm', type: 'button', disabled: used || it.inRelease,
          title: used ? 'Используется — сначала уберите её из разделов' : it.inRelease ? 'Есть в сохранённой копии сайта' : null,
          onclick: function (e) { remove(it, e.currentTarget); } }, icon('trash', 'ico--sm'), h('span', { class: 'btn__label', text: 'Удалить' })) : null;
        return rise(h('article', { class: 'card mlib__card' },
          h('div', { class: 'mlib__frame mlib__frame--lg' + (it.icon ? ' mlib__frame--icon' : '') }, it.thumb ? h('img', { src: it.thumb, alt: mediaName(it), decoding: 'async', loading: 'lazy' }) : icon('image')),
          h('div', { class: 'mlib__body' },
            h('h2', { class: 'mlib__title', text: mediaName(it) }),
            h('p', { class: 'mlib__meta', text: [it.w + '×' + it.h + NB + 'px', it.icon ? 'значок' : '', it.time ? relTime(it.time) : '', it.by && it.by.name ? it.by.name : ''].filter(Boolean).join(' · ') }),
            used ? h('ul', { class: 'chiplist chiplist--sm' }, it.uses.map(function (t) { return h('li', null, h('span', { class: 'chip', text: t })); }))
              : h('p', { class: 'mlib__free', text: it.inRelease ? 'В черновике не используется, есть в копии сайта' : 'Нигде не используется' }),
            del)), Math.min(i, 12));
      }));
    }
    function load() {
      loadThumbs().then(function (items) {
        if (!alive(token)) return;
        grid.removeAttribute('aria-busy');
        all = items;
        drawFilter();
        draw();
      });
    }
    function remove(it, btn) {
      confirmDialog({ title: 'Удалить «' + mediaName(it) + '»?', text: 'Картинка пропадёт из медиатеки. Вернуть её можно, только загрузив снова.', ok: 'Удалить', cancel: 'Отмена', danger: true, icon: 'trash' })
        .then(function (yes) {
          if (!yes) return;
          working(btn, true, 'Удаляем…');
          api('/site/media/' + encodeURIComponent(it.id), { method: 'DELETE' }).then(function () {
            toast('Картинка удалена', 'trash', 'ok');
            delete MEDIA_THUMBS[it.id];
            if (alive(token)) load();
          }, function (e) {
            working(btn, false);
            if (e.code === 'in_use') toast('Картинка используется — сначала уберите её из разделов', 'alert', 'error');
            else if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
            if (alive(token)) load();
          });
        });
    }
    if (up) {
      up.addEventListener('click', function () { file.value = ''; file.click(); });
      file.addEventListener('change', function () {
        var files = Array.prototype.slice.call(file.files || []);
        if (!files.length) return;
        working(up, true, 'Загрузка…');
        var done = 0, bad = 0;
        files.reduce(function (p, fl) {
          return p.then(function () {
            if (!/^image\/(jpeg|png)$/.test(fl.type) || fl.size > 12 * 1024 * 1024) { bad++; return; }
            var fd = new FormData();
            fd.append('file', fl, fl.name || 'image');
            return api('/site/media', { method: 'POST', form: fd, timeout: 120000 }).then(function () { done++; }, function () { bad++; });
          });
        }, Promise.resolve()).then(function () {
          working(up, false);
          if (done) toast('Загружено: ' + done, 'upload', 'ok');
          if (bad) toast('Не загрузилось: ' + bad + ' (нужен JPEG или PNG до 12' + NB + 'МБ)', 'alert', 'error');
          if (alive(token)) load();
        });
      });
    }
    load();
  }

  /* ------------------------------------------------------------------ site: activity log */

  function activityText(it, who) {
    var sec = it.sectionTitle ? '«' + it.sectionTitle + '»' : 'раздел';
    var v = function (m, f) { return verb(who, m, f); };
    switch (it.kind) {
      case 'save': return v('сохранил', 'сохранила') + ' ' + sec;
      case 'publish': return v('опубликовал', 'опубликовала') + ' сайт' + (it.note ? ': «' + it.note + '»' : '');
      case 'restore': return v('вернул', 'вернула') + ' сайт к прошлой версии';
      case 'upload': return v('загрузил', 'загрузила') + ' картинку' + (it.note ? ' «' + it.note + '»' : '');
      case 'delete': return v('удалил', 'удалила') + ' картинку' + (it.note ? ' «' + it.note + '»' : '');
      case 'schedule': return v('запланировал', 'запланировала') + ' публикацию на ' + (toDate(it.note) ? fullTime(it.note) : it.note);
      case 'unschedule': return v('отменил', 'отменила') + ' запланированную публикацию';
      case 'publish_failed': return '— запланированная публикация не прошла: ' + (it.note || '');
      case 'indexnow': return v('отправил', 'отправила') + ' страницы сайта в Яндекс';
    }
    return it.kind;
  }
  var ACT_ICON = { save: 'save', publish: 'rocket', restore: 'history', upload: 'upload', 'delete': 'trash', schedule: 'calendar', unschedule: 'calendar', publish_failed: 'alert', indexnow: 'compass' };
  function viewActivity(root, r, token) {
    var list = h('div', { class: 'card alog', 'aria-busy': 'true' }, h('div', { class: 'skel skel--line', vars: { width: '60%' } }));
    fill(root, pageHead('Журнал действий', 'Кто и что менял на сайте'), list);
    function load() {
      api('/site/activity?limit=300').then(function (d) {
        if (!alive(token)) return;
        list.removeAttribute('aria-busy');
        var items = (d && d.items) || [];
        if (!items.length) {
          fill(list, h('div', { class: 'empty fade' }, h('span', { class: 'empty__art' }, icon('clock')),
            h('h2', { class: 'empty__title', text: 'Пока пусто' }), h('p', { class: 'empty__text', text: 'Здесь появятся сохранения разделов, публикации и загрузки картинок.' })));
          return;
        }
        var day = '', kids = [];
        items.forEach(function (it) {
          var dt = toDate(it.time);
          var key = dt ? ymd(dt) : '';
          if (key !== day) { day = key; kids.push(h('h2', { class: 'alog__day', text: dt ? dm(dt, true) : '—' })); }
          var who = (it.by && it.by.name) || 'Кто-то';
          var link = it.section && it.kind === 'save' ? h('a', { class: 'alog__link', href: '#/edit/' + encodeURIComponent(it.section) }, 'Открыть') : null;
          kids.push(h('div', { class: 'alog__row' + (it.kind === 'publish_failed' ? ' alog__row--bad' : '') },
            h('span', { class: 'alog__ico' }, icon(ACT_ICON[it.kind] || 'dot', 'ico--sm')),
            h('p', { class: 'alog__text' }, b(who), ' ' + activityText(it, who)),
            h('time', { class: 'alog__time', datetime: it.time || '', text: dt ? hm(dt) : '' }),
            link));
        });
        fill(list, kids);
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        list.removeAttribute('aria-busy');
        fill(list, errorCard(e, load));
      });
    }
    load();
  }

  /* ------------------------------------------------------------------ site: indexing */

  function viewIndexing(root, r, token) {
    var box = h('div', { class: 'idx', 'aria-busy': 'true' }, h('div', { class: 'card' }, h('div', { class: 'skel skel--line', vars: { width: '50%' } })));
    fill(root, pageHead('Индексация', 'Как поисковики узнают о сайте'), box);
    function row(ok, title, text, action) {
      return h('div', { class: 'idx__row' },
        h('span', { class: 'idx__mark idx__mark--' + (ok === true ? 'ok' : ok === false ? 'bad' : 'warn') }, icon(ok === true ? 'check' : 'alert', 'ico--sm')),
        h('div', { class: 'idx__body' }, h('h3', { class: 'idx__title', text: title }), h('p', { class: 'idx__text', text: text }), action || null));
    }
    function load() {
      api('/site/indexing').then(function (d) {
        if (!alive(token)) return;
        box.removeAttribute('aria-busy');
        var last = d.last;
        var send = can('site') ? h('button', { class: 'btn btn--primary', type: 'button', disabled: !d.ready || !d.published }, icon('compass', 'ico--sm'), h('span', { class: 'btn__label', text: 'Отправить страницы в Яндекс' })) : null;
        if (send) send.addEventListener('click', function () {
          working(send, true, 'Отправляем…');
          api('/site/indexing', { method: 'POST', body: {} }).then(function (res) {
            working(send, false);
            var l = res && res.last;
            if (l && l.ok) toast('Яндекс получил ' + l.count + ' ' + plural(l.count, ['страницу', 'страницы', 'страниц']), 'compass', 'ok');
            else toast((l && l.error) || 'Не получилось отправить', 'alert', 'error');
            if (alive(token)) load();
          }, function (e) {
            working(send, false);
            if (e.code === 'republish') toast('Опубликуйте сайт ещё раз: в текущей версии нет ключа для Яндекса', 'alert', 'error');
            else if (e.code === 'not_ready') toast('Сайт закрыт от поиска или у него ещё нет настоящего адреса', 'alert', 'error');
            else if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
          });
        });
        var hostOk = !!d.host && d.host.indexOf('.') > 0 && !/\.(example|local|test)$/.test(d.host);
        fill(box,
          h('section', { class: 'card idx__card' },
            row(hostOk, 'Адрес сайта', !d.host ? 'Не указан. Укажите настоящий адрес (https://…) в «Контакты, меню, реквизиты».'
              : hostOk ? d.host : d.host + ' — это адрес-пример. Укажите настоящий домен сайта.',
              h('a', { class: 'link', href: '#/edit/site' }, 'Изменить адрес')),
            row(d.open, d.open ? 'Сайт открыт для поисковиков' : 'Сайт закрыт от поисковиков',
              d.open ? 'Яндекс и Google могут показывать его в выдаче.' : 'Так и должно быть, пока сайт не готов. Открыть — в «Поиск — настройки сайта».',
              h('a', { class: 'link', href: '#/edit/seo_site' }, 'Настройки поиска')),
            row(d.ready && last ? last.ok : d.ready ? null : false, 'Сообщения Яндексу (IndexNow)',
              !d.ready ? 'Не работают: сайт закрыт, IndexNow выключен или адрес ненастоящий (например, .example).'
                : last ? (last.ok ? 'Последний раз ' + fullTime(last.time) + ': отправлено ' + last.count + ' ' + plural(last.count, ['страница', 'страницы', 'страниц']) + '.' : 'Последний раз ' + fullTime(last.time) + ': ' + (last.error || 'ошибка') + '.')
                  : 'После каждой публикации Яндекс сам узнаёт о новых и изменённых страницах.',
              send)),
          h('p', { class: 'note' }, icon('help', 'ico--sm'), h('span', { text: 'Чтобы видеть, как сайт индексируется, подключите Яндекс Вебмастер и Google Search Console: код подтверждения вставляется в «Поиск — настройки сайта». Карта сайта — по адресу ' + (d.host ? 'https://' + d.host : '') + '/sitemap.xml.' })));
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        box.removeAttribute('aria-busy');
        fill(box, errorCard(e, load));
      });
    }
    load();
  }

  /* ------------------------------------------------------------------ site: health */

  var LEVEL = { ok: 'В порядке', warn: 'Замечание', error: 'Ошибка' };
  function viewHealth(root, r, token) {
    var again = h('button', { class: 'btn btn--primary', type: 'button' }, icon('refresh', 'ico--sm'), h('span', { class: 'btn__label', text: 'Проверить снова' }));
    var sum = h('div', { class: 'hsum hsum--page' });
    var list = h('div', { class: 'checks', 'aria-busy': 'true' }, [0, 1, 2].map(function () { return h('div', { class: 'card check' }, h('div', { class: 'skel skel--line', vars: { width: '50%' } })); }));
    var sr = h('p', { class: 'sr', role: 'status' });
    fill(root, pageHead('Проверка сайта', 'Черновик, последняя сборка и ссылки опубликованного сайта', again), sum, sr, list);
    // a detail like «Первый экран: HERO.lead: заполните поле» links to its section
    function detailNode(text) {
      var t = String(text);
      var sch = S.site && S.site.schema;
      var sec = sch ? (sch.sections || []).filter(function (x) { return t.indexOf(x.title + ':') === 0; })[0] : null;
      if (sec) return h('li', null, h('a', { class: 'link', href: '#/edit/' + encodeURIComponent(sec.id), text: sec.title }), t.slice(sec.title.length));
      return h('li', { text: t });
    }
    function load(manual) {
      working(again, true, 'Проверяем…');
      list.setAttribute('aria-busy', 'true');
      api('/site/health').then(function (d) {
        if (!alive(token)) return;
        working(again, false);
        list.removeAttribute('aria-busy');
        var checks = (d.checks || []).slice().sort(function (a, z) { var o = { error: 0, warn: 1, ok: 2 }; return (o[a.level] || 0) - (o[z.level] || 0); });
        var c = { ok: 0, warn: 0, error: 0 };
        checks.forEach(function (x) { if (has(c, x.level)) c[x.level]++; });
        fill(sum,
          h('div', { class: 'tile hsum__tile hsum__tile--error' }, h('span', { class: 'hsum__num', text: num(c.error) }), h('span', { class: 'hsum__label', text: plural(c.error, ['ошибка', 'ошибки', 'ошибок']) })),
          h('div', { class: 'tile hsum__tile hsum__tile--warn' }, h('span', { class: 'hsum__num', text: num(c.warn) }), h('span', { class: 'hsum__label', text: plural(c.warn, ['замечание', 'замечания', 'замечаний']) })),
          h('div', { class: 'tile hsum__tile hsum__tile--ok' }, h('span', { class: 'hsum__num', text: num(c.ok) }), h('span', { class: 'hsum__label', text: 'в порядке' })));
        sr.textContent = 'Проверка завершена: ошибок ' + c.error + ', замечаний ' + c.warn + '.';
        fill(list, checks.map(function (x, i) {
          var lvl = has(LEVEL, x.level) ? x.level : 'warn';
          var det = Array.isArray(x.details) ? x.details.filter(function (s) { return typeof s === 'string' && s; }) : [];
          var multi = det.length === 1 && det[0].indexOf('\n') >= 0;
          return rise(h('article', { class: 'card check check--' + lvl },
            h('span', { class: 'hmark hmark--' + lvl }, icon(lvl === 'ok' ? 'check' : lvl === 'error' ? 'alert' : 'clock', 'ico--sm'), h('span', { class: 'sr', text: LEVEL[lvl] + ': ' })),
            h('div', { class: 'check__body' },
              h('h2', { class: 'check__title', text: x.title || '' }),
              multi ? h('pre', { class: 'log log--sm', tabindex: '0', text: det[0] })
                : det.length ? h('ul', { class: 'check__list' }, det.slice(0, 50).map(detailNode), det.length > 50 ? h('li', { class: 'muted', text: 'и ещё ' + (det.length - 50) }) : null) : null)), manual ? 0 : i);
        }));
        if (manual) toast('Проверка завершена', 'pulse', 'ok');
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        working(again, false);
        list.removeAttribute('aria-busy');
        fill(list, errorCard(e, function () { load(true); }));
      });
    }
    again.addEventListener('click', function () { load(true); });
    load(false);
  }

  /* ------------------------------------------------------------------ password */

  function viewPassword(root, r, token) {
    var MIN = 10;
    function pwField(id, label, auto, hint) {
      var inp = h('input', { class: 'input', id: id, type: 'password', autocomplete: auto, required: true, spellcheck: 'false', 'aria-describedby': id + '-e' + (hint ? ' ' + id + '-h' : '') });
      var eye = h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm input-wrap__btn', type: 'button', 'aria-label': 'Показать пароль', 'aria-pressed': 'false', 'aria-controls': id }, icon('eye', 'ico--sm'));
      eye.addEventListener('click', function () {
        var show = inp.type === 'password';
        inp.type = show ? 'text' : 'password';
        eye.setAttribute('aria-pressed', String(show));
        eye.setAttribute('aria-label', show ? 'Скрыть пароль' : 'Показать пароль');
        fill(eye, icon(show ? 'eye-off' : 'eye', 'ico--sm'));
      });
      var err = h('p', { class: 'fld__err', id: id + '-e', hidden: true });
      var wrap = h('div', { class: 'fld fld--wide' },
        h('label', { class: 'field__label', for: id, text: label }),
        h('div', { class: 'input-wrap' }, inp, eye),
        hint ? h('p', { class: 'fld__hint', id: id + '-h', text: hint }) : null, err);
      return { wrap: wrap, input: inp, err: function (text) {
        err.textContent = '';
        if (text) add(err, icon('alert', 'ico--xs'), h('span', { text: text }));
        err.hidden = !text;
        if (text) err.setAttribute('role', 'alert'); else err.removeAttribute('role');
        inp.setAttribute('aria-invalid', text ? 'true' : 'false');
        wrap.classList.toggle('is-error', !!text);
      } };
    }
    var cur = pwField('pw-cur', 'Текущий пароль', 'current-password');
    var nw = pwField('pw-new', 'Новый пароль', 'new-password', 'Не короче ' + MIN + NB + 'символов. Удобно взять 3–4 случайных слова.');
    var rep = pwField('pw-rep', 'Повторите новый пароль', 'new-password');
    var submit = h('button', { class: 'btn btn--primary', type: 'submit' }, icon('lock', 'ico--sm'), h('span', { class: 'btn__label', text: 'Сменить пароль' }));
    // a username field lets password managers file the new password under the right login
    var user = h('input', { class: 'sr', type: 'text', name: 'username', autocomplete: 'username', tabindex: '-1', 'aria-hidden': 'true', readonly: true });
    user.value = S.user.login || '';
    var form = h('form', { class: 'pw-form', novalidate: true }, user, cur.wrap, nw.wrap, rep.wrap,
      h('div', { class: 'pw-form__foot' }, submit));
    var card = h('section', { class: 'card pw-card', 'aria-labelledby': 'h-pw' },
      h('div', { class: 'set-head' }, h('span', { class: 'set-head__ico', 'aria-hidden': 'true' }, icon('key')), h('h2', { class: 'set-head__title', id: 'h-pw', text: S.user.login || S.user.name })),
      form);
    var aside = h('section', { class: 'card pw-aside' },
      h('h2', { class: 'label', text: 'Что произойдёт' }),
      h('ul', { class: 'pw-list' },
        h('li', null, icon('check', 'ico--sm'), h('span', { text: 'В этом браузере вы останетесь в панели.' })),
        h('li', null, icon('check', 'ico--sm'), h('span', { text: 'На других устройствах сеансы завершатся — там нужно будет войти с новым паролем.' })),
        h('li', null, icon('check', 'ico--sm'), h('span', { text: 'Забыли пароль? Владелец выдаст новый в разделе «Пользователи».' }))));
    fill(root, pageHead('Сменить пароль', 'Пароль для входа в панель'), h('div', { class: 'pw-grid' }, rise(card, 0), rise(aside, 1)));
    [cur, nw, rep].forEach(function (x) { x.input.addEventListener('input', function () { x.err(''); }); });
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (submit.disabled) return;
      var c = cur.input.value, n = nw.input.value, rp = rep.input.value, bad = null;
      cur.err(''); nw.err(''); rep.err('');
      if (!c) { cur.err('Введите текущий пароль'); bad = bad || cur; }
      if (chars(n) < MIN) { nw.err('Не короче ' + MIN + NB + 'символов'); bad = bad || nw; }
      else if (n === c) { nw.err('Новый пароль совпадает с текущим'); bad = bad || nw; }
      if (!bad && n !== rp) { rep.err('Пароли не совпадают'); bad = rep; }
      if (bad) { bad.input.focus(); return; }
      working(submit, true, 'Меняем…');
      api('/me/password', { method: 'PUT', body: { current: c, new: n }, timeout: 30000 }).then(function () {
        working(submit, false);
        cur.input.value = ''; nw.input.value = ''; rep.input.value = '';
        toast('Пароль изменён. Другие сеансы завершены', 'lock', 'ok');
        loadSession(true).catch(function () {});   // the fresh session has its own CSRF token
      }, function (e2) {
        working(submit, false);
        if (e2.code === 'invalid' && e2.field === 'current') { cur.err('Неверный текущий пароль'); cur.input.select(); cur.input.focus(); }
        else if (e2.code === 'invalid' && e2.field === 'new') { nw.err('Пароль не подходит: не короче ' + MIN + NB + 'символов и не такой, как текущий'); nw.input.focus(); }
        else if (e2.code === 'rate') { var m = Math.max(1, Math.ceil((parseInt(e2.retry, 10) || 60) / 60)); cur.err('Слишком много попыток — подождите ' + m + NB + 'мин'); }
        else if (e2.code !== 'auth') toast(errText(e2), 'alert', 'error');
      });
    });
  }

  /* ------------------------------------------------------------------ team */

  function seenText(u) {
    if (!u.lastSeen) return { on: false, text: verb(u.name, 'ещё не заходил', 'ещё не заходила') };
    var d = toDate(u.lastSeen);
    if (!d) return { on: false, text: '' };
    if (Date.now() - d.getTime() < 5 * 60000) return { on: true, text: 'в сети' };
    return { on: false, text: verb(u.name, 'был', 'была') + ' в сети ' + relTime(u.lastSeen) };
  }

  var ROLE_HINT = {
    owner: 'всё, включая пользователей и возврат версий сайта',
    manager: 'правит и публикует сайт, работает с заявками',
    viewer: 'только смотрит, ничего не меняет'
  };

  function viewTeam(root, r, token) {
    var owner = can('team');
    var list = h('div', { class: 'team', 'aria-busy': 'true' }, [0, 1, 2, 3].map(function () {
      return h('div', { class: 'card member' }, h('div', { class: 'member__head' }, h('span', { class: 'av av--xl skel' }), h('div', { class: 'member__text' }, h('div', { class: 'skel skel--line' }), h('div', { class: 'skel skel--line', vars: { width: '60%' } })))) ;
    }));
    var addBtn = owner ? h('button', { class: 'btn btn--primary', type: 'button', onclick: function () { addUserDialog(); } }, icon('user-plus', 'ico--sm'), 'Добавить пользователя') : null;
    var roles = h('section', { class: 'card roles-card', 'aria-labelledby': 'h-roles' },
      h('h2', { class: 'label', id: 'h-roles', text: 'Роли' }),
      h('dl', { class: 'roles' }, Object.keys(ROLE).map(function (k) {
        return h('div', { class: 'roles__row' }, h('dt', null, h('span', { class: 'pill pill--plain role--' + k, text: ROLE[k] })), h('dd', { text: ROLE_HINT[k] }));
      })),
      owner ? null : h('p', { class: 'muted small', text: 'Добавляет пользователей и меняет роли владелец.' }));
    fill(root, pageHead('Пользователи', owner ? 'Кто работает в панели и с какими правами' : 'Кто работает в панели', addBtn), list, roles);

    function reload() { S.team = null; return load(); }
    function load() {
      return api('/team').then(function (d) {
        if (!alive(token)) return;
        S.team = d.items || [];
        setTeamTints(S.team);
        list.removeAttribute('aria-busy');
        if (!S.team.length) { fill(list, h('p', { class: 'muted', text: 'Пользователей пока нет.' })); return; }
        fill(list, S.team.map(function (u, i) { return rise(memberCard(u), i); }));
      }, function (e) {
        if (!alive(token) || e.code === 'auth') return;
        list.removeAttribute('aria-busy');
        fill(list, errorCard(e, load));
      });
    }

    function memberCard(u) {
      var seen = seenText(u);
      var me = S.user && u.id === S.user.id;
      var off = u.active === false;
      var tools = null;
      if (owner && !me) {
        var sel = h('select', { class: 'select select--sm member__role', 'aria-label': 'Роль: ' + u.name },
          Object.keys(ROLE).map(function (k) { return h('option', { value: k, text: ROLE[k] }); }));
        sel.value = has(ROLE, u.role) ? u.role : 'viewer';
        sel.addEventListener('change', function () {
          var want = sel.value;
          change(u, { role: want }, 'Роль изменена: ' + u.name + NB + '— ' + ROLE[want], function () { sel.value = u.role; });
        });
        tools = h('div', { class: 'member__tools' },
          sel,
          h('button', { class: 'btn btn--soft btn--sm', type: 'button', onclick: function (e) { resetPassword(u, e.currentTarget); } }, icon('key', 'ico--sm'), 'Новый пароль'),
          h('button', { class: 'btn btn--soft btn--sm', type: 'button', onclick: function () { toggleUser(u); } }, icon('power', 'ico--sm'), off ? 'Включить' : 'Отключить'),
          h('button', { class: 'icon-btn icon-btn--flat icon-btn--sm acts__del', type: 'button', 'aria-label': 'Удалить пользователя ' + u.name, title: 'Удалить', onclick: function () { removeUser(u); } }, icon('trash', 'ico--sm')));
      }
      return h('article', { class: 'card member' + (me ? ' member--me' : '') + (off ? ' member--off' : '') },
        h('div', { class: 'member__head' }, avatar(u, 'xl'),
          h('div', { class: 'member__text' },
            h('h2', { class: 'member__name', text: u.name, title: u.name }),
            h('p', { class: 'member__login', text: u.login || '', title: u.login || null }))),
        h('div', { class: 'member__foot' },
          h('span', { class: 'online' + (seen.on && !off ? ' online--on' : '') + (off ? ' online--off' : ''), text: off ? 'Доступ отключён' : seen.text }),
          h('span', { class: 'member__pills' },
            me ? h('span', { class: 'pill pill--plain st--new', text: 'Это вы' }) : null,
            owner && !me ? null : h('span', { class: 'pill pill--plain role--' + (has(ROLE, u.role) ? u.role : 'viewer'), text: lab(ROLE, u.role, u.role) }))),
        tools);
    }

    function change(u, body, okText, undo) {
      return api('/users/' + encodeURIComponent(u.id), { method: 'PATCH', body: body }).then(function (d) {
        toast(okText, 'check-circle', 'ok');
        reload();
        return d;
      }, function (e) {
        if (undo) undo();
        if (e.code === 'not_found') { toast('Такого пользователя уже нет', 'alert', 'error'); reload(); }
        else if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
        return null;
      });
    }
    function toggleUser(u) {
      var off = u.active === false;
      if (off) { change(u, { disabled: false }, 'Доступ включён: ' + u.name); return; }
      confirmDialog({ title: 'Отключить ' + u.name + '?', text: 'Человек сразу выйдет из панели и не сможет войти, пока вы не включите доступ снова. Данные и история останутся.', ok: 'Отключить', danger: true, icon: 'power' })
        .then(function (y) { if (y) change(u, { disabled: true }, 'Доступ отключён: ' + u.name); });
    }
    function resetPassword(u, btn) {
      confirmDialog({ title: 'Выдать новый пароль?', text: 'Старый пароль ' + u.name + ' перестанет работать, открытые сеансы завершатся. Новый пароль покажем один раз.', ok: 'Выдать пароль', icon: 'key' })
        .then(function (y) {
          if (!y) return;
          working(btn, true);
          api('/users/' + encodeURIComponent(u.id), { method: 'PATCH', body: { resetPassword: true } }).then(function (d) {
            working(btn, false);
            if (d && d.password) passwordDialog(d.user || u, d.password, false, btn);
          }, function (e) {
            working(btn, false);
            if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
          });
        });
    }
    function removeUser(u) {
      confirmDialog({ title: 'Удалить ' + u.name + '?', text: 'Пользователь исчезнет из панели и больше не сможет войти. Его имя останется в истории заявок и публикаций.', ok: 'Удалить', danger: true })
        .then(function (y) {
          if (!y) return;
          api('/users/' + encodeURIComponent(u.id), { method: 'DELETE' }).then(function () {
            toast('Пользователь удалён: ' + u.name, 'check-circle', 'ok');
            reload();
          }, function (e) {
            if (e.code === 'not_found') { toast('Такого пользователя уже нет', 'alert', 'error'); reload(); }
            else if (e.code !== 'auth') toast(errText(e), 'alert', 'error');
          });
        });
    }

    function addUserDialog() {
      function field(id, label, input, hint) {
        var err = h('p', { class: 'fld__err', id: id + '-e', hidden: true });
        input.setAttribute('aria-describedby', id + '-e' + (hint ? ' ' + id + '-h' : ''));
        var wrap = h('div', { class: 'fld fld--wide' }, h('label', { class: 'field__label', for: id, text: label }), input, hint ? h('p', { class: 'fld__hint', id: id + '-h', text: hint }) : null, err);
        return { wrap: wrap, input: input, err: function (t) {
          err.textContent = '';
          if (t) add(err, icon('alert', 'ico--xs'), h('span', { text: t }));
          err.hidden = !t;
          if (t) err.setAttribute('role', 'alert'); else err.removeAttribute('role');
          input.setAttribute('aria-invalid', t ? 'true' : 'false');
        } };
      }
      var login = field('nu-login', 'Логин (почта)', h('input', { class: 'input', id: 'nu-login', type: 'email', inputmode: 'email', autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false', maxlength: '120', required: true }), 'По нему человек входит в панель');
      var name = field('nu-name', 'Имя и фамилия', h('input', { class: 'input', id: 'nu-name', type: 'text', autocomplete: 'off', maxlength: '80', required: true }));
      var roleSel = h('select', { class: 'select', id: 'nu-role' }, Object.keys(ROLE).map(function (k) { return h('option', { value: k, text: ROLE[k] + NB + '— ' + ROLE_HINT[k] }); }));
      roleSel.value = 'manager';
      var role = field('nu-role', 'Роль', roleSel);
      var formErr = h('p', { class: 'fld__err', hidden: true });
      var ok = h('button', { class: 'btn btn--primary', type: 'submit' }, icon('user-plus', 'ico--sm'), h('span', { class: 'btn__label', text: 'Добавить' }));
      var cancel = h('button', { class: 'btn btn--soft', type: 'button', text: 'Отмена' });
      var form = h('form', { class: 'dialog__form', novalidate: true },
        h('p', { class: 'dialog__text', text: 'Пароль сгенерируем и покажем один раз — передайте его человеку лично.' }),
        login.wrap, name.wrap, role.wrap, formErr,
        h('div', { class: 'dialog__foot' }, cancel, ok));
      var m = modal({ title: 'Новый пользователь', icon: 'user-plus', body: [form], focus: login.input });
      cancel.addEventListener('click', function () { m.close(); });
      [login, name].forEach(function (x) { x.input.addEventListener('input', function () { x.err(''); formErr.hidden = true; }); });
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        if (ok.disabled) return;
        var l = login.input.value.trim(), n = name.input.value.trim(), bad = null;
        login.err(''); name.err(''); formErr.hidden = true;
        if (chars(l) < 3 || /\s/.test(l)) { login.err('Введите логин без пробелов, например name@domain.ru'); bad = bad || login; }
        if (!n) { name.err('Введите имя'); bad = bad || name; }
        if (bad) { bad.input.focus(); return; }
        working(ok, true, 'Добавляем…');
        m.locked = true;
        api('/users', { method: 'POST', body: { login: l, name: n, role: roleSel.value } }).then(function (d) {
          m.locked = false;
          m.close();
          S.team = null;
          load();
          if (d && d.password) passwordDialog(d.user || { name: n, login: l }, d.password, true, addBtn);
        }, function (e2) {
          m.locked = false;
          working(ok, false);
          if (e2.code === 'duplicate' || (e2.status === 409)) { login.err('Такой логин уже есть'); login.input.focus(); }
          else if (e2.code === 'invalid' && e2.field === 'login') { login.err('Логин не подходит: от 3 до 120 символов, без пробелов'); login.input.focus(); }
          else if (e2.code === 'invalid' && e2.field === 'name') { name.err('Имя не подходит: до 80 символов, без служебных знаков'); name.input.focus(); }
          else if (e2.code !== 'auth') { formErr.textContent = errText(e2); formErr.hidden = false; }
        });
      });
    }
    load();
  }

  // The password is shown once and lives only in this dialog: nothing keeps it after it closes.
  function passwordDialog(u, password, isNew, returnTo) {
    var code = h('code', { class: 'secret__text', text: password });
    var copy = h('button', { class: 'btn btn--soft btn--sm', type: 'button', onclick: function () { copyText(password); } }, icon('copy', 'ico--sm'), 'Скопировать');
    var done = h('button', { class: 'btn btn--primary', type: 'button', text: 'Готово' });
    var m = modal({ title: isNew ? 'Пользователь добавлен' : 'Новый пароль', icon: 'key', returnTo: returnTo,
      body: [
        h('p', { class: 'dialog__text' }, 'Пароль для ', b(u.name || ''), u.login ? ' (' + u.login + ')' : '', ':'),
        h('div', { class: 'secret' }, code, copy),
        h('p', { class: 'note note--warn' }, icon('alert', 'ico--sm'), h('span', { text: 'Пароль показывается один раз. Скопируйте его и передайте лично или через защищённый мессенджер — после закрытия окна его не увидит никто.' }))],
      foot: [done], focus: copy });
    done.addEventListener('click', function () { m.close(); });
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
