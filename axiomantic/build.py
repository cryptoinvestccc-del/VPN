#!/usr/bin/env python3
"""Builds the Axiomantic site from content.py into site/.

    python3 axiomantic/build.py

The markup is the designer's (Tailwind classes, compiled once into
site/assets/css/main.css by tailwind/); every text, number, link and
picture comes from content.py, or from the panel's draft through
AXM_CONTENT. Links between pages are relative, so the folder works from any
web server; open it through one (python3 -m http.server -d axiomantic/site).

Nothing is loaded from other servers: the font, icons and scripts are
local; Yandex Metrika loads only after the visitor accepts cookies, when a
counter is set. The output is deterministic — CI rebuilds it and fails if
site/ differs from git.
"""
import hashlib
import html
import json
import os
import re
import shutil
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
# The panel builds previews and releases elsewhere: AXM_OUT is a copy of
# site/ to write into, AXM_MEDIA the folder of uploaded pictures.
OUT = os.environ.get('AXM_OUT') or os.path.join(HERE, 'site')
MEDIA_DIR = os.environ.get('AXM_MEDIA', '')
sys.path.insert(0, HERE)
sys.dont_write_bytecode = True  # no __pycache__ beside the sources
import content as C  # noqa: E402

S = C.SITE
T = C.TEXTS
IMG = C.IMAGES


def e(s):
    return html.escape(str(s), quote=True)


class Page:
    """A page at a folder path such as '' or 'cases/tenders/'."""

    def __init__(self, path, absolute=False):
        self.path = path
        self.depth = path.count('/')
        self.absolute = absolute

    def u(self, target=''):
        if self.absolute:
            return '/' + target
        return '../' * self.depth + target or './'


SAFE_URL = re.compile(r'^(https?://[^\s"<>]+|mailto:[^\s"<>]+|tel:\+?[0-9]+|#[A-Za-z0-9_-]*|[a-z0-9][a-z0-9/_-]*/?(#[A-Za-z0-9_-]*)?)$')


def href(p, url):
    """An address from content: outside links as they are, the site's own
    paths ("privacy/") relative to the page; anything else is dropped."""
    url = url.strip()
    if not SAFE_URL.match(url):
        return ''
    if re.match(r'^(https?:|mailto:|tel:|#)', url):
        return url
    return p.u(url)


def md(p, text):
    """Escaped text with **bold**, [links](path) and line breaks."""
    out = []
    pos = 0
    for m in re.finditer(r'\*\*(.+?)\*\*|\[([^\]]+)\]\(([^)\s]+)\)', text):
        out.append(e(text[pos:m.start()]))
        if m.group(1) is not None:
            out.append(f'<strong>{e(m.group(1))}</strong>')
        else:
            url = href(p, m.group(3))
            ext = ' target="_blank" rel="noopener"' if url.startswith('http') else ''
            out.append(f'<a href="{e(url)}"{ext}>{e(m.group(2))}</a>' if url else e(m.group(2)))
        pos = m.end()
    out.append(e(text[pos:]))
    return ''.join(out).replace('\n', '<br>')


def fa(name, cls=''):
    """A Font Awesome icon; names come from the panel's list."""
    style = 'fa-brands' if name in ('fa-telegram', 'fa-vk', 'fa-whatsapp') else 'fa-solid'
    if not re.match(r'^fa-[a-z0-9-]+$', name or ''):
        name = 'fa-circle'
    return f'<i class="{style} {name}{" " + cls if cls else ""}" aria-hidden="true"></i>'


MONTHS = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа',
          'сентября', 'октября', 'ноября', 'декабря']


def date_ru(iso):
    y, m, d = iso.split('-')
    return f'{int(d)} {MONTHS[int(m) - 1]} {y}'


def asset_version(*parts):
    h = hashlib.sha256()
    for part in parts:
        with open(os.path.join(OUT, 'assets', part), 'rb') as f:
            h.update(f.read())
    return h.hexdigest()[:10]


# ------------------------------------------------------------- pictures

MEDIA = None


def media_entry(mid):
    """A picture uploaded in the panel, as AXM_MEDIA/manifest.json describes
    it (JPEG at several widths, and square PNGs for an icon). The files are
    copied next to the pages."""
    global MEDIA
    if MEDIA is None:
        MEDIA = {}
        mp = os.path.join(MEDIA_DIR, 'manifest.json') if MEDIA_DIR else ''
        if mp and os.path.exists(mp):
            with open(mp, encoding='utf-8') as f:
                MEDIA = json.load(f)
    m = MEDIA.get(mid) if mid else None
    if not m:
        return None
    os.makedirs(os.path.join(OUT, 'assets', 'media'), exist_ok=True)
    for _, name in m.get('jpg', []) + m.get('png', []):
        dst = os.path.join(OUT, 'assets', 'media', name)
        if not os.path.exists(dst):
            shutil.copyfile(os.path.join(MEDIA_DIR, name), dst)
    return m


def media_img(p, mid, alt, sizes, cls, eager=False):
    """An uploaded picture filling its box, or None when there is none."""
    m = media_entry(mid)
    if not m or not m.get('jpg'):
        return None
    srcset = ', '.join(f'{p.u("assets/media/" + name)} {w}w' for w, name in m['jpg'])
    load = 'loading="eager" fetchpriority="high"' if eager else 'loading="lazy"'
    alt_attr = f'alt="{e(alt)}"' if alt else 'alt=""'
    return (f'<img class="{cls}" src="{p.u("assets/media/" + m["jpg"][-1][1])}" srcset="{srcset}" sizes="{sizes}" '
            f'width="{m["w"]}" height="{m["h"]}" {alt_attr} {load} decoding="async">')


def favicon_links(p):
    m = media_entry(IMG.get('favicon'))
    if m and m.get('png'):
        icons = dict((w, name) for w, name in m['png'])
        small = icons.get(32) or m['png'][0][1]
        big = icons.get(180) or m['png'][-1][1]
        return (f'<link rel="icon" href="{p.u("assets/media/" + small)}" type="image/png" sizes="32x32">\n'
                f'  <link rel="apple-touch-icon" href="{p.u("assets/media/" + big)}">')
    return f'<link rel="icon" href="{p.u("assets/favicon.svg")}" type="image/svg+xml">'


def og_image():
    m = media_entry(IMG.get('og'))
    if m and m.get('jpg'):
        w, name = m['jpg'][-1]
        return f'{S["url"]}/assets/media/{name}', w, round(m['h'] * w / m['w'])
    return '', 0, 0


def logo(p, cls):
    m = media_entry(IMG.get('logo'))
    if m and m.get('jpg'):
        w, name = m['jpg'][0]
        return (f'<img src="{p.u("assets/media/" + name)}" width="{w}" height="{round(m["h"] * w / m["w"])}" '
                f'alt="{e(S["name"])}" class="h-9 w-auto">')
    return e(S['logo'])


# ---------------------------------------------------------------- frame

def modal_btn(cls, label, interest=''):
    data = f' data-interest="{e(interest)}"' if interest else ''
    return (f'<button type="button" data-project-modal{data} aria-haspopup="dialog" aria-controls="project-modal" '
            f'class="{cls}">{e(label)}</button>')


def nav_items():
    items = list(C.NAV)
    if C.ABOUT.get('show') and C.ABOUT.get('nav'):
        items.append(['about', C.ABOUT['title'], 'about/'])
    for x in C.LANDINGS:
        if x.get('show', True) and x.get('nav'):
            items.append(['l-' + x['slug'], x.get('menu') or x['title'], x['slug'] + '/'])
    return items


def header(p, active):
    def links(cls_on, cls):
        out = []
        for key, label, path in nav_items():
            cur = ' aria-current="page"' if key == active else ''
            out.append(f'<a href="{href(p, path)}" class="{cls_on if key == active else cls}"{cur}>{e(label)}</a>')
        return '\n        '.join(out)
    desk = links('nav-link transition-colors duration-300 hover:text-text-heading text-text-heading',
                 'nav-link transition-colors duration-300 hover:text-text-heading')
    mob = links('rounded-full px-4 py-3 hover:bg-surface-muted', 'rounded-full px-4 py-3 hover:bg-surface-muted')
    H = T['header']
    return f'''<header class="fixed top-0 left-0 right-0 z-50 h-20 border-b border-slate-200 bg-white/80 backdrop-blur-md">
  <div class="mx-auto flex h-full max-w-container items-center justify-between gap-6 px-6 lg:px-8">
    <a href="{p.u()}" aria-label="{e(S["name"])} — на главную" class="text-2xl font-extrabold tracking-tighter text-text-heading">{logo(p, "")}</a>
    <div class="flex items-center gap-6 lg:gap-8">
      <nav aria-label="Основная навигация" class="hidden items-center gap-6 text-sm font-medium text-text-body lg:flex">
        {desk}
      </nav>
      {modal_btn("hidden rounded-full px-6 py-3 text-sm font-medium sm:inline-flex btn-dark", H["cta"])}
      <button id="menu-toggle" aria-controls="mobile-menu" aria-expanded="false" type="button" aria-label="{e(H["menu_open"])}" data-label-open="{e(H["menu_open"])}" data-label-close="{e(H["menu_close"])}" class="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-full lg:hidden btn-dark">
        <i class="fa-solid fa-bars" aria-hidden="true"></i>
      </button>
    </div>
  </div>
  <nav id="mobile-menu" aria-label="Мобильная навигация" hidden class="absolute inset-x-0 top-20 border-b border-slate-200 bg-surface px-6 py-6 lg:hidden">
    <div class="mx-auto flex max-w-container flex-col gap-1 font-medium text-text-heading">
      {mob}
      {modal_btn("mt-3 rounded-full px-8 py-4 text-center btn-dark", H["cta"])}
    </div>
  </nav>
</header>'''


MAIL_SVG = ('<svg viewBox="0 0 24 24" fill="currentColor" focusable="false"><path fill-rule="evenodd" d="M4.5 4a2.5 2.5 0 0 0-2.5 2.5v11A2.5 2.5 0 0 0 4.5 20h15a2.5 2.5 0 0 0 2.5-2.5v-11A2.5 2.5 0 0 0 19.5 4h-15Zm-.02 2.86a.8.8 0 0 0-.96 1.28l8 6a.8.8 0 0 0 .96 0l8-6a.8.8 0 1 0-.96-1.28L12 12.5 4.48 6.86Z" clip-rule="evenodd"/></svg>')
TG_SVG = ('<svg viewBox="0 0 24 24" fill="currentColor" focusable="false"><path d="M21.4 3.5 18.2 20c-.2 1.2-.9 1.5-1.9.9l-4.9-3.6-2.4 2.3c-.3.3-.5.5-1 .5l.4-5 9.1-8.2c.4-.4-.1-.6-.6-.2L5.6 13.8.8 12.3c-1-.3-1-1 .2-1.5L20 3.4c.9-.3 1.7.2 1.4 1.1Z"/></svg>')


def footer(p):
    nav = ''.join(f'<a href="{href(p, path)}">{e(label)}</a>' for _, label, path in nav_items())
    social = ''
    if S.get('email'):
        social += f'<a href="mailto:{e(S["email"])}" aria-label="Почта: {e(S["email"])}"><span class="ax-social-tile" aria-hidden="true">{MAIL_SVG}</span></a>'
    if S.get('telegram'):
        social += f'<a href="{e(S["telegram"])}" aria-label="Telegram"><span class="ax-social-tile" aria-hidden="true">{TG_SVG}</span></a>'
    if S.get('max'):
        social += (f'<a href="{e(S["max"])}" aria-label="MAX"><span class="ax-social-tile" aria-hidden="true"><img class="ax-max-logo" '
                   f'src="{p.u("assets/images/social/max.png")}" alt="" width="26" height="26" decoding="async"></span></a>')
    tagline = '<br>'.join(e(x) for x in S['tagline'])
    F = T['footer']
    return (f'<footer class="site-footer"><div class="af"><div class="af-top"><a class="ax-logo" href="{p.u()}" aria-label="{e(S["name"])} — главная страница">'
            f'{logo(p, "")}</a><nav class="ax-nav" aria-label="Навигация по сайту">{nav}</nav></div><div class="af-row"><p class="ax-tagline">{tagline}</p>'
            f'<address class="ax-contact"><a class="ax-phone" href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a><div class="ax-social">{social}</div>'
            f'</address></div><div class="af-bottom"><div class="ax-legal"><p class="ax-copyright">© <span id="year">{S["year"]}</span> {e(S["name"])} · {e(S["city"])}</p>'
            f'<p>{e(S["legal_name"])}</p><p>{e(S["legal_ids"])}</p></div><div class="ax-docs"><a href="{p.u("privacy/")}">{e(F["privacy"])}</a>'
            f'<a href="{p.u("consent/")}">{e(F["consent"])}</a></div></div></div></footer>')


def lead_form(p, fid, source, cls):
    """The designer's form. main.js sends it to SITE.endpoint; ids differ in
    the dialog so a page can hold both."""
    F = C.FORM
    pre = 'modal-' if fid == 'project-modal-form' else ''
    contact_id = pre + 'contact-value'
    name_id = pre + 'name'
    task_id = pre + 'project'
    priv_id = pre + 'privacy'
    status_id = 'modal-form-status' if pre else 'form-status'
    sel_id = 'modal-selected-project' if pre else 'selected-project'
    return f'''<form id="{fid}" class="{cls}" data-source="{source}" novalidate>
          <input id="{sel_id}" type="hidden" name="interest" value="">
          <div class="hp" aria-hidden="true"><label>Не заполняйте это поле <input type="text" name="website" tabindex="-1" autocomplete="off"></label></div>
          <div>
            <label for="{name_id}" class="mb-2 block text-sm font-medium text-text-heading">{e(F["name"])} <span aria-hidden="true">*</span></label>
            <input id="{name_id}" name="name" type="text" autocomplete="name" placeholder="{e(F["name_ph"])}" required maxlength="80" class="w-full min-w-0 rounded-xl border-none bg-surface px-5 py-4 text-base text-text-heading placeholder:text-slate-400">
          </div>
          <div class="mt-5">
            <label for="{contact_id}" class="mb-2 block text-sm font-medium text-text-heading">{e(F["contact"])} <span aria-hidden="true">*</span></label>
            <input id="{contact_id}" name="contact" type="text" inputmode="tel" placeholder="{e(F["contact_ph"])}" required minlength="3" maxlength="120" class="w-full min-w-0 rounded-xl border-none bg-surface px-5 py-4 text-base text-text-heading placeholder:text-slate-400">
          </div>
          <div class="mt-5">
            <label for="{task_id}" class="mb-2 block text-sm font-medium text-text-heading">{e(F["task"])}</label>
            <textarea id="{task_id}" name="message" rows="4" maxlength="3000" placeholder="{e(F["task_ph"])}" class="block w-full min-w-0 resize-y rounded-xl border-none bg-surface px-5 py-4 text-base text-text-heading placeholder:text-slate-400"></textarea>
          </div>
          <div class="mt-6 flex items-start gap-3">
            <input type="checkbox" id="{priv_id}" name="consent" value="accepted" required class="mt-1 h-5 w-5 shrink-0 rounded border-slate-300 text-text-heading focus:ring-brand">
            <label for="{priv_id}" class="text-sm text-text-body leading-snug consent-text">{md(p, F["consent"])}</label>
          </div>
          <button type="submit" disabled class="mt-6 w-full rounded-full py-4 font-medium btn-dark">{e(F["submit"])}</button>
          <p class="mt-3 text-sm leading-relaxed text-text-body">
            <i class="fa-solid fa-check text-brand mr-1" aria-hidden="true"></i> {e(F["note"])}
          </p>
          <p id="{status_id}" role="status" aria-live="polite" aria-atomic="true" class="mt-4 text-sm font-medium text-text-body"></p>
        <noscript><p>{e(F["nojs"])}</p></noscript>
        </form>'''


def modal(p):
    M = T['modal']
    form = lead_form(p, 'project-modal-form', 'modal', 'min-w-0 rounded-card bg-surface-muted p-8 lg:p-10')
    return f'''<dialog id="project-modal" class="project-modal" aria-labelledby="project-modal-title">
  <div class="project-modal-heading">
    <h2 id="project-modal-title">{e(M["title"])}</h2>
    <button type="button" class="project-modal-close" data-close-modal aria-label="{e(M["close"])}">×</button>
  </div>
  {form}
  <p class="project-modal-direct">{e(M["direct"])} <a href="{e(S["telegram"])}" target="_blank" rel="noopener">{e(M["direct_tg"])}</a> / <a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></p>
</dialog>'''


def cookie(p):
    """Shown only with a counter: Metrika starts after «Принять»."""
    K = C.CONNECT
    if not K.get('metrika'):
        return ''
    return f'''<div id="cookie-banner" hidden class="fixed bottom-4 left-1/2 z-[100] w-[calc(100%-2rem)] max-w-2xl -translate-x-1/2 translate-y-24 opacity-0 transition-all duration-500 ease-in-out">
  <div class="flex flex-col items-center justify-between gap-4 rounded-card bg-dark p-5 text-white shadow-xl sm:flex-row sm:p-6">
    <p class="text-sm text-dark-text text-center sm:text-left">{e(K["cookie_text"])} <a href="{p.u("privacy/")}" class="underline">{e(K["cookie_more"])}</a></p>
    <div class="flex shrink-0 gap-2">
      <button id="decline-cookies" type="button" class="rounded-full px-5 py-2.5 text-sm font-medium text-white ring-1 ring-white/30 hover:bg-white/10">{e(K["cookie_decline"])}</button>
      <button id="accept-cookies" type="button" class="rounded-full px-6 py-2.5 text-sm font-medium btn-inverse">{e(K["cookie_accept"])}</button>
    </div>
  </div>
</div>'''


def body_data():
    """What the scripts need besides markup: endpoints, messages, counter."""
    out = f' data-endpoint="{e(S["endpoint"])}" data-msg="{e(json.dumps(T["msg"], ensure_ascii=False, separators=(",", ":")))}"'
    if C.CONNECT.get('metrika'):
        out += f' data-metrika="{e(C.CONNECT["metrika"])}"'
        if C.CONNECT.get('webvisor'):
            out += ' data-webvisor'
    return out


def ld(obj):
    return ('<script type="application/ld+json">'
            + json.dumps(obj, ensure_ascii=False, separators=(',', ':')).replace('</', '<\\/')
            + '</script>')


def design_css():
    """The panel's colours and sizes over the designer's variables. Nothing
    is written while every value is empty."""
    D = C.DESIGN
    tokens = []
    for key, var in (('brand', 'brand'), ('brand_hover', 'brand-hover'), ('brand_light', 'brand-light'), ('heading', 'heading'),
                     ('body', 'body'), ('surface', 'surface'), ('surface_muted', 'surface-muted'), ('dark', 'dark'), ('dark_text', 'dark-text')):
        v = D.get(key) or ''
        if re.match(r'^#[0-9A-Fa-f]{6}$', v):
            r, g, b = int(v[1:3], 16), int(v[3:5], 16), int(v[5:7], 16)
            tokens.append(f'--c-{var}:{r} {g} {b}')
    num = lambda k: D.get(k) if isinstance(D.get(k), (int, float)) and not isinstance(D.get(k), bool) else None  # noqa: E731
    if num('radius') is not None:
        n = num('radius')
        tokens.append(f'--r-card:{n:g}px;--r-section:{n * 1.25:g}px;--r-inner:{n * .75:g}px')
    if num('width') is not None:
        tokens.append(f'--w-container:{num("width"):g}px')
    if not tokens:
        return ''
    return '\n/* panel: design settings (generated) */\n:root{' + ';'.join(tokens) + '}\n'


VERSIONS = {}
WRITTEN = set()  # index.html files this run wrote; anything else is stale

CSS = ['css/main.css', 'css/footer.css', 'css/layout.css', 'css/modal.css', 'css/site.css']


def render(p, *, title, desc, body, active=None, kind='inner', extra_css=(), extra_js=(), jsonld=(), index=True):
    url = S['url'] + '/' + p.path
    robots = '' if index and C.SEO.get('index', True) else '<meta name="robots" content="noindex">\n  '
    og_src, og_w, og_h = og_image()
    og_img = (f'<meta property="og:image" content="{e(og_src)}">\n  <meta property="og:image:width" content="{og_w}">\n  '
              f'<meta property="og:image:height" content="{og_h}">\n  ') if og_src else ''
    verify = ''
    if C.SEO.get('yandex_verification'):
        verify += f'<meta name="yandex-verification" content="{e(C.SEO["yandex_verification"])}">\n  '
    if C.SEO.get('google_verification'):
        verify += f'<meta name="google-site-verification" content="{e(C.SEO["google_verification"])}">\n  '
    css = ''.join(f'\n  <link rel="stylesheet" href="{p.u("assets/" + c)}?v={VERSIONS["css"]}">' for c in CSS + list(extra_css))
    js = ''.join(f'\n  <script src="{p.u("assets/js/" + j)}?v={VERSIONS["js"]}" defer></script>' for j in ['main.js'] + list(extra_js))
    body_cls = {'home': '', 'inner': 'inner-page ', 'legal': 'inner-page legal-page '}[kind]
    doc = f'''<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="{e(C.SEO.get("theme_color") or "#ffffff")}">
  <title>{e(title)}</title>
  <meta name="description" content="{e(desc)}">
  {robots}<link rel="canonical" href="{e(url)}">
  <meta property="og:type" content="website">
  <meta property="og:site_name" content="{e(S["name"])}">
  <meta property="og:locale" content="ru_RU">
  <meta property="og:title" content="{e(title)}">
  <meta property="og:description" content="{e(desc)}">
  <meta property="og:url" content="{e(url)}">
  {og_img}{verify}{favicon_links(p)}
  <link rel="preload" href="{p.u("assets/fonts/inter-cyrillic-opsz-normal.woff2")}" as="font" type="font/woff2" crossorigin>
  <link rel="stylesheet" href="{p.u("assets/vendor/fontawesome/css/all.min.css")}">{css}{js}
  {"".join(ld(j) for j in jsonld)}
</head>
<body id="top" class="{body_cls}bg-surface font-sans text-base leading-relaxed text-text-body"{body_data()}>
  <a href="#main" class="sr-only z-[60] rounded-full bg-dark px-6 py-3 text-white focus:not-sr-only focus:fixed focus:left-4 focus:top-4">{e(T["skip"])}</a>

  {header(p, active)}

  <main id="main">
{body}
  </main>

  {footer(p)}
  {modal(p)}
  {cookie(p)}
</body>
</html>
'''
    out_dir = os.path.join(OUT, p.path)
    os.makedirs(out_dir, exist_ok=True)
    out_file = os.path.join(out_dir, 'index.html')
    with open(out_file, 'w', encoding='utf-8') as f:
        f.write(doc)
    WRITTEN.add(os.path.normpath(out_file))
    return url


def breadcrumbs_ld(items):
    return {'@context': 'https://schema.org', '@type': 'BreadcrumbList', 'itemListElement': [
        {'@type': 'ListItem', 'position': i + 1, 'name': name, 'item': S['url'] + '/' + path}
        for i, (name, path) in enumerate(items)]}


# --------------------------------------------------------------- pieces

def check_points(p, points, icon_cls='text-brand'):
    return ''.join(f'''
              <li class="flex gap-3"><i class="fa-solid fa-check mt-1 text-xs {icon_cls}" aria-hidden="true"></i><span><strong>{e(b)}</strong> {md(p, t)}</span></li>'''
                   for b, t in points)


def dark_cta(cta, button, cls='px-6 py-24', inner=''):
    return f'''    <section class="{cls}">
      <div class="{inner}mx-auto max-w-4xl rounded-section bg-dark p-8 text-center text-white sm:p-12">
        <h2 class="text-4xl font-bold tracking-tight sm:text-5xl">{e(cta["title"])}</h2>
        <p class="mx-auto mt-5 max-w-lg text-base leading-relaxed text-dark-text lg:text-lg">{e(cta["text"])}</p>
        {modal_btn("mt-8 inline-flex items-center justify-center rounded-full px-8 py-4 font-medium btn-inverse", button)}
      </div>
    </section>'''


def contact_block(p, tag, title_id, source, intro_cls=''):
    K = C.CONTACT_BLOCK
    title = C.CONTACTS['title'] if tag == 'h1' else K['title']
    lead = C.CONTACTS['lead'] if tag == 'h1' else K['lead']
    tg = (f'''
              <a href="{e(S["telegram"])}" target="_blank" rel="noopener" class="flex items-center gap-4 rounded-full px-5 py-3 font-medium btn-light">
                <i class="fa-brands fa-telegram text-lg text-brand" aria-hidden="true"></i> {e(K["telegram"])}
              </a>''' if S.get('telegram') else '')
    mx = (f'''
              <a href="{e(S["max"])}" target="_blank" rel="noopener" class="flex items-center gap-4 rounded-full px-5 py-3 font-medium btn-light">
                <i class="fa-solid fa-comment-dots text-lg text-brand" aria-hidden="true"></i> {e(K["max"])}
              </a>''' if S.get('max') else '')
    container = 'page-container ' if tag == 'h1' else ''
    return f'''    <section id="contact" aria-labelledby="{title_id}" class="{intro_cls}py-24">
      <div class="{container}mx-auto grid max-w-container grid-cols-1 gap-12 px-6 lg:grid-cols-2 lg:px-8">
        <div>
          <{tag} id="{title_id}" class="text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(title)}</{tag}>
          <p class="mt-6 max-w-sm text-base leading-relaxed text-text-body lg:text-lg">{e(lead)}</p>
          <div class="mt-10 space-y-4">
            <div class="rounded-2xl bg-surface-muted p-5 flex items-start gap-4">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-surface text-brand">
                <i class="fa-solid fa-phone text-sm" aria-hidden="true"></i>
              </div>
              <div>
                <p class="text-xs font-medium uppercase tracking-wide text-text-body">{e(K["call_label"])}</p>
                <a href="tel:{e(S["phone_href"])}" class="mt-1 block text-xl font-semibold text-text-heading hover:text-brand transition-colors">{e(S["phone"])}</a>
                <p class="mt-1 text-xs text-text-body">{e(S["hours"])}</p>
              </div>
            </div>
            <div class="flex flex-col gap-3">{tg}{mx}
            </div>
          </div>
        </div>
        {lead_form(p, "contact-form", source, "min-w-0 rounded-card bg-surface-muted p-8 lg:p-10")}
      </div>
    </section>'''


def placeholder(icon, label, aspect, extra='', big=False):
    size = 'text-5xl mb-4 opacity-50' if big else 'text-4xl mb-3 opacity-40'
    text = 'text-lg font-medium text-text-body' if big else 'font-medium text-text-body'
    return f'''<div class="flex {aspect} items-center justify-center rounded-card {extra or "bg-surface-muted"} text-text-muted">
              <div class="text-center">
                <i class="{"fa-regular" if icon == "fa-image" else "fa-solid"} {e(icon)} {size}" aria-hidden="true"></i>
                <p class="{text}">{e(label)}</p>
              </div>
            </div>'''


def picture_box(p, image, icon, label, aspect, sizes, extra='', big=False, eager=False):
    """An uploaded screen in the designer's box, or the designer's placeholder."""
    img = media_img(p, image, label, sizes, 'h-full w-full object-cover', eager) if image else None
    if img:
        return f'<div class="flex {aspect} overflow-hidden rounded-card {extra or "bg-surface-muted"}">{img}</div>'
    return placeholder(icon, label, aspect, extra, big)


def case_card(p, c, home=False):
    tag = c.get('home_tag') if home and c.get('home_tag') else c['card_tag']
    text = c.get('home_text') if home and c.get('home_text') else c['card_text']
    n, label, green = c['card_metric']
    hl = 'h3' if home else 'h2'
    cover = media_img(p, c.get('cover'), '', '(min-width: 768px) 33vw, 100vw', 'h-full w-full object-cover') if c.get('cover') else None
    if cover:
        media = f'<div class="flex aspect-[4/3] overflow-hidden rounded-inner bg-slate-200">{cover}</div>'
    elif home:
        media = '<div class="flex aspect-[4/3] items-center justify-center rounded-inner bg-slate-200 text-sm text-text-body">Превью</div>'
    else:
        media = ('<div class="flex aspect-[4/3] items-center justify-center rounded-inner border-2 border-dashed border-slate-300 bg-surface/50 text-slate-400 '
                 'group-hover:border-slate-400 transition-colors">\n              <i class="fa-regular fa-image text-3xl" aria-hidden="true"></i>\n            </div>')
    text_cls = 'text-base' if home else 'text-sm'
    return f'''
          <a href="{p.u("cases/" + c["slug"] + "/")}" class="group flex min-w-0 flex-col rounded-card bg-surface-muted p-4 transition-all duration-300 hover:-translate-y-1 hover:bg-slate-200">
            {media}
            <div class="flex flex-1 flex-col p-4 pt-6">
              <span class="self-start rounded-full bg-surface px-3 py-1 text-xs font-medium text-brand">{e(tag)}</span>
              <{hl} class="mt-4 text-xl font-semibold text-text-heading lg:text-2xl group-hover:text-brand transition-colors">{e(c["card_title"])}</{hl}>
              <p class="mb-6 mt-3 {text_cls} leading-relaxed text-text-body">{e(text)}</p>
              <p class="mt-auto border-t border-slate-200 pt-5 text-sm text-text-body"><span class="mr-2 text-2xl font-bold {"text-emerald-600" if green else "text-text-heading"}">{e(n)}</span>{e(label)}</p>
            </div>
          </a>'''


# -------------------------------------------------- blocks of the old admin

def problems_sec(p):
    K = C.PROBLEMS
    if not K.get('show'):
        return ''
    cards = ''.join(f'''
          <article class="flex flex-col rounded-card bg-surface-muted p-8 lg:p-10">
            <span class="text-sm font-medium text-brand">{i + 1:02d}</span>
            <h3 class="mt-6 text-2xl font-semibold leading-tight tracking-tight text-text-heading">{e(t)}</h3>
            <p class="mt-4 text-base leading-relaxed lg:text-lg text-text-body">{e(x)}</p>
          </article>''' for i, (t, x) in enumerate(K['items']))
    lead = f'<p class="mx-auto mt-5 max-w-xl text-center text-base leading-relaxed lg:text-lg text-text-body">{e(K["lead"])}</p>' if K.get('lead') else ''
    return f'''    <section id="problems" class="py-24">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(K["title"])}</h2>{lead}
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">{cards}
        </div>
      </div>
    </section>
'''


def compare_sec(p):
    K = C.COMPARE
    if not K.get('show'):
        return ''
    rows = ''.join(f'<tr class="border-t border-slate-200"><th scope="row" class="px-6 py-5 text-left font-semibold text-text-heading">{e(a)}</th>'
                   f'<td class="px-6 py-5 text-text-body">{e(b)}</td><td class="px-6 py-5 font-semibold text-text-heading">{e(c)}</td></tr>'
                   for a, b, c in K['rows'])
    return f'''    <section id="compare" class="mx-4 rounded-card bg-surface-muted py-24 lg:mx-8">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(K["title"])}</h2>
        <div class="mt-14 overflow-x-auto rounded-card bg-surface">
          <table class="w-full min-w-[560px] border-collapse text-base">
            <thead><tr><td class="px-6 py-5"></td><th scope="col" class="px-6 py-5 text-left text-sm font-medium text-text-body">{e(K["left"])}</th><th scope="col" class="px-6 py-5 text-left text-sm font-medium text-brand">{e(K["right"])}</th></tr></thead>
            <tbody>{rows}</tbody>
          </table>
        </div>
      </div>
    </section>
'''


def terms_sec(p):
    K = C.TERMS
    if not K.get('show'):
        return ''
    cards = ''.join(f'''
          <article class="flex flex-col rounded-card bg-surface-muted p-8">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-brand-light text-lg text-brand"><i class="fa-solid fa-check" aria-hidden="true"></i></div>
            <h3 class="mt-6 text-2xl font-semibold tracking-tight text-text-heading">{e(t)}</h3>
            <p class="mt-3 text-base leading-relaxed text-text-body">{e(x)}</p>
          </article>''' for t, x in K['items'])
    return f'''    <section id="terms" class="py-24">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(K["title"])}</h2>
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">{cards}
        </div>
      </div>
    </section>
'''


def prose(p, body):
    out = []
    for kind, val in body:
        if kind == 'p':
            out.append(f'<p>{md(p, val)}</p>')
        elif kind == 'h2':
            out.append(f'<h2>{e(val)}</h2>')
        elif kind == 'ul':
            out.append('<ul>' + ''.join(f'<li>{md(p, x)}</li>' for x in val) + '</ul>')
        elif kind == 'quote':
            out.append(f'<blockquote><p>{md(p, val)}</p></blockquote>')
    return '\n'.join(out)


# ---------------------------------------------------------------- pages

def home():
    p = Page('')
    H = C.HERO
    title = '<br class="hidden sm:block"> '.join(e(x) for x in H['title'])
    teams = '<li aria-hidden="true" class="font-normal text-slate-300">·</li>\n            '.join(f'<li>{e(t)}</li>' for t in H['teams'])
    hero = f'''    <section id="hero" aria-labelledby="hero-title" class="px-6 py-24 lg:py-32">
      <div class="mx-auto max-w-4xl text-center">
        <div class="mb-8 inline-flex items-center gap-2.5 rounded-full bg-surface-muted px-4 py-1.5 text-sm font-medium text-text-heading">
          <span class="h-2 w-2 rounded-full bg-brand" aria-hidden="true"></span>
          {e(H["badge"])}
        </div>
        <h1 id="hero-title" class="text-5xl font-extrabold leading-[1.05] tracking-tighter text-text-heading sm:text-6xl lg:text-7xl">{title}</h1>
        <p class="mx-auto mt-6 max-w-2xl text-xl leading-relaxed text-text-body">{e(H["lead"])}</p>
        <div class="mt-10 flex flex-col items-stretch justify-center gap-3 sm:flex-row sm:items-center">
          {modal_btn("inline-flex items-center justify-center rounded-full px-8 py-4 font-medium btn-dark", H["cta"])}
          <a href="{p.u("portfolio/")}" class="inline-flex items-center justify-center gap-3 rounded-full px-8 py-4 font-medium btn-light">{e(H["link"])} <i class="fa-solid fa-arrow-right text-sm" aria-hidden="true"></i></a>
        </div>
        <div class="mt-16">
          <p class="text-sm text-text-body">{e(H["teams_label"])}</p>
          <ul aria-label="Опыт команды" class="mt-5 flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-xl font-bold tracking-tight text-text-heading sm:gap-x-8 sm:text-2xl">
            {teams}
          </ul>
        </div>
      </div>
    </section>
'''
    A = C.AUDIENCE
    audience = f'''    <section aria-labelledby="audience-title" class="py-24">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 id="audience-title" class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(A["title"])}</h2>
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">''' + ''.join(f'''
          <article class="flex flex-col rounded-card bg-surface-muted p-8 lg:p-10">
            <span class="text-sm font-medium text-brand">{e(lbl)}</span>
            <h3 class="mt-10 text-2xl font-semibold leading-tight tracking-tight text-text-heading">{e(t)}</h3>
            <p class="mt-4 text-base leading-relaxed lg:text-lg text-text-body">{e(x)}</p>
          </article>''' for lbl, t, x in A['items']) + '''
        </div>
      </div>
    </section>
'''
    P = C.PROCESS
    pill = 'rounded-full border border-slate-200 bg-surface px-5 py-4'
    row = lambda items: ''.join(f'\n            <li data-flow-target class="{pill}">{e(x)}</li>' for x in items)  # noqa: E731
    down = ('<div class="relative h-12"><div class="absolute inset-y-0 left-1/2 w-0 border-l border-dashed border-slate-300"></div>'
            '<div class="absolute left-[16.666667%] right-[16.666667%] top-0 hidden h-6 border-x border-b border-dashed border-slate-300 md:block"></div></div>')
    up = ('<div class="relative h-12"><div class="absolute inset-y-0 left-1/2 w-0 border-l border-dashed border-slate-300"></div>'
          '<div class="absolute bottom-0 left-[16.666667%] right-[16.666667%] hidden h-6 border-x border-t border-dashed border-slate-300 md:block"></div></div>')
    process = f'''    <section id="axiomantic-process" aria-labelledby="process-title" class="mx-4 rounded-card bg-surface-muted py-24 lg:mx-8">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 id="process-title" class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(P["title"])}</h2>
        <div class="text-center mb-12 mt-6">
          <div class="inline-flex items-center gap-2 rounded-full bg-dark px-6 py-3 text-sm font-medium text-white sm:text-base">
            {e(P["badge"])}
          </div>
        </div>
        <div class="mx-auto mt-8 max-w-3xl flow-board" data-flow-board>
          <div class="flow-overlay text-brand" data-flow-overlay aria-hidden="true"></div>
          <ul class="grid grid-cols-1 gap-3 text-center font-medium text-text-heading md:grid-cols-3 md:gap-6">{row(P["top"])}
          </ul>
          {down}
          <div class="flex justify-center"><div data-flow-hub class="rounded-full bg-dark px-10 py-5 text-2xl font-extrabold tracking-tighter text-white">{e(P["hub"])}</div></div>
          {up}
          <ul class="grid grid-cols-1 gap-3 text-center font-medium text-text-heading md:grid-cols-3 md:gap-6">{row(P["bottom"])}
          </ul>
          {down}
          <div class="flex justify-center"><div data-flow-target class="rounded-full border border-slate-200 bg-surface px-6 py-4 text-center font-medium text-text-heading sm:px-10">{e(P["last"])}</div></div>
        </div>
      </div>
    </section>
'''
    AP = C.APPROACH
    approach = f'''    <section aria-labelledby="product-title" class="py-24">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <div class="max-w-3xl">
          <h2 id="product-title" class="text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{"<br>".join(e(x) for x in AP["title"])}</h2>
          <p class="mt-5 text-lg leading-relaxed text-text-body">{e(AP["lead"])}</p>
        </div>
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">''' + ''.join(f'''
          <article class="flex flex-col rounded-card bg-surface-muted p-8">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-brand-light text-lg text-brand">{fa(it["icon"])}</div>
            <h3 class="mt-6 text-2xl font-semibold tracking-tight text-text-heading">{e(it["title"])}</h3>
            <p class="mt-3 text-base leading-relaxed text-text-body">{e(it["text"])}</p>
            <ul class="mt-6 space-y-4 text-sm text-text-body">{check_points(p, it["points"])}
            </ul>
          </article>''' for it in AP['items']) + '''
        </div>
      </div>
    </section>
'''
    SV = C.SERVICES_HOME
    services = f'''    <section id="services" aria-labelledby="services-title" class="py-24">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 id="services-title" class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(SV["title"])}</h2>
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">''' + ''.join(f'''
          <article class="rounded-card bg-surface-muted p-8 lg:p-10"><div class="flex h-12 w-12 items-center justify-center rounded-full bg-brand-light text-lg text-brand">{fa(ic)}</div><h3 class="mt-8 text-2xl font-semibold text-text-heading">{e(t)}</h3><p class="mt-3 text-base leading-relaxed lg:text-lg text-text-body">{e(x)}</p></article>'''
                                                                                                for ic, t, x in SV['items']) + '''
        </div>
      </div>
    </section>
'''
    CH = C.CASES_HOME
    featured = [c for c in C.CASES if c.get('featured')][:3]
    cases = f'''    <section id="cases" aria-labelledby="cases-title" class="py-24">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <div class="flex flex-wrap items-center justify-between gap-6">
          <h2 id="cases-title" class="text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(CH["title"])}</h2>
          <a href="{p.u("portfolio/")}" class="inline-flex min-h-11 items-center gap-2 rounded-full px-5 text-sm font-medium btn-light">{e(CH["button"])} <i class="fa-solid fa-arrow-right text-xs" aria-hidden="true"></i></a>
        </div>
        <div id="cases-grid" class="mt-10 grid grid-cols-1 gap-6 md:grid-cols-3">{"".join(case_card(p, c, home=True) for c in featured)}
        </div>
      </div>
    </section>
''' if featured else ''
    PR = C.PRICING
    plans = []
    for pl in PR['plans']:
        dark = pl.get('dark')
        prefix = f'<span class="text-base font-normal {"text-dark-text" if dark else "text-text-body"}">{e(pl["prefix"])} </span>' if pl.get('prefix') else ''
        price = e(pl['price']).replace(' ', '&nbsp;')
        feats = ''.join(f'\n              <li class="flex gap-3"><i class="fa-solid fa-check mt-1 text-xs {"text-white" if dark else "text-brand"}" aria-hidden="true"></i><span>{e(f)}</span></li>'
                        for f in pl['features'])
        if dark:
            plans.append(f'''
          <article class="flex min-w-0 flex-col rounded-card border border-slate-900 bg-dark p-8 text-white">
            <h3 class="text-lg font-semibold lg:text-2xl">{e(pl["name"])}</h3>
            <p class="mt-3 text-sm leading-relaxed text-dark-text">{e(pl["text"])}</p>
            <p class="mt-8 text-3xl font-bold lg:text-4xl">{prefix}{price}</p>
            <ul class="mb-8 mt-8 space-y-4 border-t border-slate-700 pt-8 text-sm text-dark-text flex-1">{feats}
            </ul>
            {modal_btn("mt-auto inline-flex items-center justify-center rounded-full px-6 py-4 text-center font-medium btn-inverse", pl["button"], pl.get("interest", ""))}
          </article>''')
        else:
            plans.append(f'''
          <article class="flex min-w-0 flex-col rounded-card border border-slate-200 bg-surface p-8">
            <h3 class="text-lg font-semibold text-text-heading lg:text-2xl">{e(pl["name"])}</h3>
            <p class="mt-3 text-sm leading-relaxed text-text-body">{e(pl["text"])}</p>
            <p class="mt-8 text-3xl font-bold text-text-heading lg:text-4xl">{prefix}{price}</p>
            <ul class="mb-8 mt-8 space-y-4 border-t border-slate-200 pt-8 text-sm flex-1 text-text-body">{feats}
            </ul>
            {modal_btn("mt-auto inline-flex items-center justify-center rounded-full px-6 py-4 text-center font-medium btn-dark", pl["button"], pl.get("interest", ""))}
          </article>''')
    pricing = f'''    <section id="pricing" aria-labelledby="pricing-title" class="mx-4 rounded-card bg-surface-muted py-24 lg:mx-8">
      <div class="mx-auto max-w-container px-6 lg:px-8">
        <h2 id="pricing-title" class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(PR["title"])}</h2>
        <p class="mx-auto mt-5 max-w-xl text-center text-base leading-relaxed lg:text-lg text-text-body">{e(PR["lead"])}</p>
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">{"".join(plans)}
        </div>
      </div>
    </section>
'''
    CT = C.CTA_HOME
    cta = f'''    <section aria-labelledby="cta-title" class="px-6 py-24">
      <div class="mx-auto max-w-4xl rounded-section bg-dark p-8 text-center text-white sm:p-12">
        <h2 id="cta-title" class="text-4xl font-bold tracking-tight sm:text-5xl">{e(CT["title"])}</h2>
        <p class="mx-auto mt-5 max-w-lg text-base leading-relaxed text-dark-text lg:text-lg">{e(CT["text"])}</p>
        {modal_btn("mt-8 inline-flex items-center justify-center rounded-full px-8 py-4 font-medium btn-inverse", CT["button"])}
      </div>
    </section>
'''
    contact = contact_block(p, 'h2', 'contact-title', 'home') + '\n' if C.HOME_FORM.get('show', True) else ''
    body = (hero + problems_sec(p) + audience + process + approach + services + compare_sec(p) + cases + pricing
            + terms_sec(p) + cta + contact)
    org = {'@context': 'https://schema.org', '@type': 'Organization', 'name': S['name'], 'url': S['url'],
           'email': S['email'], 'telephone': S['phone_href'], 'description': C.SEO.get('org_desc', '')}
    return render(p, title=C.SEO['home_title'], desc=C.SEO['home_desc'], body=body, active='home', kind='home',
                  extra_css=['css/home.css'], extra_js=['process-flow.js'], jsonld=(org,))


def services():
    p = Page('services/')
    K = C.SERVICES_PAGE
    items = ''.join(f'''
          <article class="flex min-w-0 flex-col gap-6 rounded-card bg-surface-muted p-8 sm:flex-row lg:p-10">
            <div class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-brand-light text-lg text-brand">
              {fa(ic)}
            </div>
            <div class="min-w-0">
              <h2 class="text-2xl font-semibold tracking-tight text-text-heading">{e(t)}</h2>
              <p class="mt-3 text-base leading-relaxed text-text-body lg:text-lg">{e(x)}</p>
            </div>
          </article>''' for ic, t, x in K['items'])
    body = f'''    <section aria-labelledby="hero-title" class="page-intro px-6 pt-24 pb-16">
      <div class="page-container mx-auto max-w-4xl text-center">
        <h1 id="hero-title" class="text-5xl font-extrabold leading-[1.05] tracking-tighter text-text-heading sm:text-6xl lg:text-7xl">{e(K["title"])}</h1>
        <p class="mx-auto mt-6 max-w-2xl text-xl leading-relaxed text-text-body">{e(K["lead"])}</p>
      </div>
    </section>

    <section aria-label="Список услуг" class="pb-24">
      <div class="page-container mx-auto max-w-container px-6 lg:px-8">
        <div class="flex flex-col gap-6">{items}
        </div>
      </div>
    </section>

{dark_cta(K["cta"], K["cta"]["button"], inner="page-container ")}
'''
    return render(p, title=C.SEO['services_title'], desc=C.SEO['services_desc'], body=body, active='services',
                  jsonld=(breadcrumbs_ld([('Главная', ''), (K['title'], p.path)]),))


def portfolio():
    p = Page('portfolio/')
    K = C.PORTFOLIO
    body = f'''    <section class="page-intro px-6 py-24">
      <div class="page-container mx-auto max-w-container">
        <div class="max-w-3xl">
          <h1 class="text-5xl font-extrabold leading-[1.05] tracking-tighter text-text-heading sm:text-6xl lg:text-7xl">{e(K["title"])}</h1>
          <p class="mt-6 text-xl leading-relaxed text-text-body">{e(K["lead"])}</p>
        </div>
        <div class="mt-16 grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">{"".join(case_card(p, c) for c in C.CASES)}
        </div>
      </div>
    </section>

{dark_cta(K["cta"], K["cta"]["button"], inner="page-container ")}
'''
    return render(p, title=C.SEO['portfolio_title'], desc=C.SEO['portfolio_desc'], body=body, active='portfolio',
                  jsonld=(breadcrumbs_ld([('Главная', ''), (K['title'], p.path)]),))


def case_page(c):
    p = Page(f'cases/{c["slug"]}/')
    tags = ''.join(f'\n          <span class="rounded-full bg-surface-muted px-4 py-1.5 text-sm font-medium {"text-brand" if i == 0 else "text-text-heading"}">{e(t)}</span>'
                   for i, t in enumerate(c['tags']))
    meta = ''.join(f'''
          <div>
            <p class="text-xs font-semibold uppercase tracking-wider text-text-body mb-2">{e(k)}</p>
            <p class="text-base font-medium text-text-heading">{e(v)}</p>
          </div>''' for k, v in c['meta'])

    def card(k):
        return f'''
          <article class="flex flex-col rounded-card bg-surface-muted p-8 lg:p-10">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-surface text-lg text-brand mb-6">
              {fa(k["icon"])}
            </div>
            <h2 class="text-2xl font-semibold tracking-tight text-text-heading mb-3">{e(k["title"])}</h2>
            <p class="text-base leading-relaxed text-text-body mb-6">{e(k["text"])}</p>
            <ul class="space-y-4 text-sm text-text-body mt-auto">{check_points(p, k["points"])}
            </ul>
          </article>'''
    gallery = ''
    if c['gallery']:
        slots = []
        for g in c['gallery']:
            aspect = 'aspect-[16/9] md:aspect-[21/9]' if g.get('wide') else 'aspect-[4/3]'
            cap_cls = 'text-sm text-text-body italic px-2 text-center' if g.get('wide') else 'text-sm text-text-body italic px-2'
            box = picture_box(p, g.get('image'), g['icon'], g['label'], aspect, '(min-width: 768px) 50vw, 100vw')
            cap = f'\n            <p class="{cap_cls}">{e(g["caption"])}</p>' if g.get('caption') else ''
            slots.append(f'''
          <div{' class="md:col-span-2"' if g.get('wide') else ''}>
            <div class="mb-4">{box}</div>{cap}
          </div>''')
        gallery = f'''
    <section class="px-6 pb-24">
      <div class="page-container mx-auto max-w-container">
        <h2 class="text-3xl font-bold tracking-tight text-text-heading mb-10">{e(c["gallery_title"])}</h2>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6 lg:gap-8">{"".join(slots)}
        </div>
      </div>
    </section>
'''
    mobile = ''
    M = c.get('mobile') or {}
    if M.get('show'):
        box = picture_box(p, M.get('image'), 'fa-mobile-screen-button', M['label'], 'aspect-[4/5]', '(min-width: 768px) 40vw, 100vw', extra='bg-surface')
        mobile = f'''
    <section class="px-6 pb-24">
      <div class="page-container mx-auto max-w-container">
        <div class="rounded-section bg-surface-muted p-8 lg:p-12">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-10 items-center">
            <div>
              <h2 class="text-3xl font-bold tracking-tight text-text-heading mb-4">{e(M["title"])}</h2>
              <p class="text-base leading-relaxed text-text-body mb-6">{e(M["text"])}</p>
              <ul class="space-y-4 text-sm text-text-body">{check_points(p, M["points"])}
              </ul>
            </div>
            <div class="flex flex-col gap-4">
              {box}
            </div>
          </div>
        </div>
      </div>
    </section>
'''
    features = ''
    FE = c.get('features') or {}
    if FE.get('show') and FE.get('items'):
        items = ''.join(f'''
          <article class="flex flex-col rounded-card bg-surface-muted p-8">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-surface text-lg text-brand mb-6">
              {fa(ic)}
            </div>
            <h3 class="text-xl font-semibold tracking-tight text-text-heading">{e(t)}</h3>
            <p class="mt-3 text-sm leading-relaxed text-text-body">{e(x)}</p>
          </article>''' for ic, t, x in FE['items'])
        features = f'''
    <section class="px-6 pb-24">
      <div class="page-container mx-auto max-w-container">
        <h2 class="text-3xl font-bold tracking-tight text-text-heading mb-10">{e(FE["title"])}</h2>
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">{items}
        </div>
      </div>
    </section>
'''
    results = ''.join(f'''
          <div>
            <p class="text-5xl font-extrabold {"text-emerald-600" if green else "text-text-heading"}">{e(n)}</p>
            <p class="text-sm font-medium text-text-body mt-2">{e(lbl)}</p>
          </div>''' for n, lbl, green in c['results'])
    hero_box = picture_box(p, c.get('hero_image'), 'fa-image', c['hero_label'], 'aspect-[21/9]', '(min-width: 1152px) 1152px, 100vw',
                           big=True, eager=True)
    body = f'''    <section class="page-intro px-6 pt-24 pb-12">
      <div class="page-container mx-auto max-w-5xl">
        <a href="{p.u("portfolio/")}" class="inline-flex items-center gap-2 text-sm font-medium text-text-body hover:text-text-heading transition-colors mb-8">
          <i class="fa-solid fa-arrow-left" aria-hidden="true"></i> {e(T["case"]["back"])}
        </a>
        <div class="flex flex-wrap gap-3 mb-6">{tags}
        </div>
        <h1 class="text-4xl font-extrabold leading-[1.05] tracking-tighter text-text-heading sm:text-5xl lg:text-6xl">{e(c["title"])}</h1>
        <p class="mt-6 text-xl leading-relaxed text-text-body">{e(c["lead"])}</p>
      </div>
    </section>

    <section class="px-6 pb-16">
      <div class="page-container mx-auto max-w-5xl">
        <div class="grid grid-cols-2 md:grid-cols-4 gap-6 md:gap-8 bg-surface-muted rounded-card p-8 lg:p-10">{meta}
        </div>
      </div>
    </section>

    <section class="px-6 pb-16">
      <div class="page-container mx-auto max-w-container">
        {hero_box}
      </div>
    </section>

    <section class="px-6 pb-24">
      <div class="page-container mx-auto max-w-5xl">
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6 lg:gap-8">{card(c["pain"])}{card(c["done"])}
        </div>
      </div>
    </section>
{gallery}{mobile}{features}
    <section class="px-6 py-24">
      <div class="page-container mx-auto max-w-4xl">
        <h2 class="text-3xl font-bold tracking-tight text-text-heading mb-12 text-center">{e(T["case"]["results"])}</h2>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-8 text-center bg-surface-muted rounded-section p-10 lg:p-16">{results}
        </div>
      </div>
    </section>

{dark_cta(c["cta"], T["case"]["cta_button"], cls="px-6 pb-24", inner="page-container ")}
'''
    return render(p, title=c.get('seo_title') or c['title'], desc=c.get('seo_desc') or c['lead'], body=body, active='portfolio',
                  jsonld=(breadcrumbs_ld([('Главная', ''), (C.PORTFOLIO['title'], 'portfolio/'), (c['title'], p.path)]),))


THUMBZONE = '''<svg viewBox="0 0 400 220" fill="none" xmlns="http://www.w3.org/2000/svg" class="w-full max-w-sm h-auto" role="img" aria-label="Схема Thumb Zone: зоны досягаемости большого пальца на экране телефона">
                <rect x="130" y="10" width="140" height="200" rx="20" stroke="#cbd5e1" stroke-width="4" fill="#f8fafc"/>
                <rect x="180" y="20" width="40" height="6" rx="3" fill="#cbd5e1"/>
                <path d="M 134 110 Q 200 90 266 110 L 266 190 Q 200 210 134 190 Z" fill="#10b981" fill-opacity="0.15"/>
                <text x="200" y="160" text-anchor="middle" font-size="12" fill="#047857" font-weight="600">Легко (Easy)</text>
                <text x="200" y="175" text-anchor="middle" font-size="10" fill="#047857">Главные кнопки (CTA)</text>
                <path d="M 134 40 Q 200 20 266 40 L 266 110 Q 200 90 134 110 Z" fill="#f59e0b" fill-opacity="0.15"/>
                <text x="200" y="75" text-anchor="middle" font-size="12" fill="#b45309" font-weight="600">Средне (Stretch)</text>
                <text x="200" y="90" text-anchor="middle" font-size="10" fill="#b45309">Навигация, меню</text>
                <path d="M 134 10 L 266 10 L 266 40 Q 200 20 134 40 Z" fill="#ef4444" fill-opacity="0.15"/>
                <text x="200" y="28" text-anchor="middle" font-size="10" fill="#b91c1c" font-weight="600">Сложно (Hard)</text>
                <path d="M 320 220 Q 300 180 270 150" stroke="#94a3b8" stroke-width="2" stroke-dasharray="4 4" fill="none"/>
                <circle cx="320" cy="220" r="10" fill="#94a3b8"/>
              </svg>'''


def post_block(p, i, kind, val):
    """One block of a post, in the designer's paragraph style."""
    gap = ' class="mt-2"' if i else ''
    if kind == 'h2':
        return f'<p class="{"mt-4 " if i else ""}font-semibold">{e(val)}</p>'
    if kind == 'ul':
        return f'<ul class="{"mt-2 " if i else ""}list-disc space-y-1 pl-5">' + ''.join(f'<li>{md(p, x)}</li>' for x in val) + '</ul>'
    if kind == 'quote':
        return f'<blockquote class="{"mt-2 " if i else ""}border-l-2 border-brand pl-4 italic">{md(p, val)}</blockquote>'
    return f'<p{gap}>{md(p, val)}</p>'


def blog():
    p = Page('blog/')
    B = C.BLOG
    BT = T['blog']
    filters = [f'<button type="button" data-filter="all" aria-pressed="true" class="filter-btn rounded-full bg-dark px-5 py-2 text-sm font-medium text-white transition-colors">{e(B["all"])}</button>']
    cats = dict(B['categories'])
    for key, label in B['categories']:
        filters.append(f'<button type="button" data-filter="{e(key)}" aria-pressed="false" class="filter-btn rounded-full bg-surface-muted px-5 py-2 text-sm font-medium text-text-body hover:bg-slate-200 transition-colors">{e(label)}</button>')
    posts = []
    for a in C.POSTS:
        tone = 'bg-emerald-50 text-emerald-600' if a.get('tone') == 'emerald' else 'bg-brand-light text-brand'
        paras = '\n              '.join(post_block(p, i, k, v) for i, (k, v) in enumerate(a['body']))
        art = ''
        if a.get('image'):
            img = media_img(p, a['image'], '', '(min-width: 768px) 720px, 100vw', 'w-full h-auto rounded-xl')
            if img:
                art = f'\n            <div class="mt-4 w-full overflow-hidden rounded-xl border border-slate-200">{img}</div>'
        elif a.get('illustration') == 'thumbzone':
            art = f'\n            <div class="mt-4 w-full rounded-xl bg-surface p-4 flex flex-col items-center justify-center border border-slate-200">\n              {THUMBZONE}\n            </div>'
        cat = cats.get(a['cat'], a['cat'])
        pid = e(a['id'])
        posts.append(f'''
          <article id="{pid}" data-category="{e(a["cat"])}" data-post="{pid}" class="rounded-card bg-surface-muted p-6 sm:p-8">
            <header class="flex items-center gap-3">
              <div class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full {tone} text-xl font-bold" aria-hidden="true">{e(a["author"][:1])}</div>
              <div>
                <p class="font-semibold text-text-heading">{e(a["author"])} <span class="text-text-body font-normal">• {e(a["role"])}</span></p>
                <p class="text-sm text-text-body"><time datetime="{e(a["date"])}">{date_ru(a["date"])}</time> • #{e(cat.lower())}</p>
              </div>
            </header>

            <div class="mt-4 text-text-heading">
              {paras}
            </div>{art}

            <div class="mt-6 flex items-center gap-6 text-sm font-medium text-text-body">
              <button type="button" class="like-btn flex items-center gap-2 hover:text-red-500 transition-colors" aria-pressed="false" aria-label="{e(BT["like"])}" data-base="{int(a.get("likes", 0))}"><i class="fa-regular fa-heart text-lg" aria-hidden="true"></i> <span>{int(a.get("likes", 0))}</span></button>
              <button type="button" class="dislike-btn flex items-center gap-2 hover:text-text-heading transition-colors" aria-pressed="false" aria-label="{e(BT["dislike"])}" data-base="{int(a.get("dislikes", 0))}"><i class="fa-regular fa-thumbs-down text-lg" aria-hidden="true"></i> <span>{int(a.get("dislikes", 0))}</span></button>
              <button type="button" class="comment-toggle flex items-center gap-2 hover:text-brand transition-colors" aria-expanded="false"><i class="fa-regular fa-comment text-lg" aria-hidden="true"></i> {e(B["comment_toggle"])} <span class="comment-count"></span></button>
            </div>

            <div class="comments-section hidden mt-6 pt-6 border-t border-slate-200">
              <div class="comments-list space-y-3 mb-4" aria-live="polite"></div>
              <form class="comment-form flex gap-2" novalidate>
                <label class="sr-only" for="c-{pid}">{e(B["comment_ph"])}</label>
                <input id="c-{pid}" name="text" type="text" placeholder="{e(B["comment_ph"])}" required maxlength="1000" class="w-full rounded-full border-none bg-surface px-4 py-2 text-sm text-text-heading focus:ring-2 focus:ring-brand outline-none">
                <span class="hp" aria-hidden="true"><input type="text" name="website" tabindex="-1" autocomplete="off"></span>
                <button type="submit" class="rounded-full px-4 py-2 text-sm btn-dark">{e(B["comment_send"])}</button>
              </form>
              <p class="comment-status mt-2 text-sm text-text-body" role="status" aria-live="polite"></p>
            </div>
          </article>''')
    blog_msg = json.dumps({k: BT[k] for k in ('anon', 'pending', 'failed', 'empty', 'off')}, ensure_ascii=False, separators=(',', ':'))
    body = f'''    <section class="page-intro px-6 pt-24 pb-12">
      <div class="page-container mx-auto max-w-3xl">
        <h1 class="text-5xl font-extrabold leading-[1.05] tracking-tighter text-text-heading sm:text-6xl">{e(B["title"])}</h1>
        <p class="mt-6 text-xl leading-relaxed text-text-body">{e(B["lead"])}</p>
      </div>
    </section>

    <section class="px-6 pb-24">
      <div class="page-container mx-auto max-w-3xl">
        <div class="flex flex-wrap gap-2 mb-10" role="group" aria-label="Рубрики">
          {"".join(filters)}
        </div>
        <div id="feed" class="flex flex-col gap-6" data-blog="{e(S.get("blog_endpoint", ""))}" data-blog-msg="{e(blog_msg)}">{"".join(posts)}
        </div>
      </div>
    </section>
'''
    return render(p, title=C.SEO['blog_title'], desc=C.SEO['blog_desc'], body=body, active='blog', extra_js=['blog.js'],
                  jsonld=(breadcrumbs_ld([('Главная', ''), (B['title'], p.path)]),))


def contacts():
    p = Page('contacts/')
    TM = C.TEAM
    people = ''.join(f'''
          <article class="rounded-card bg-surface-muted p-8">
            <div class="flex flex-col items-start gap-4 lg:flex-row lg:items-center">
              <div aria-hidden="true" class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full {"bg-emerald-50 text-emerald-600" if tone == "emerald" else "bg-brand-light text-brand"} text-xl font-bold">{e(name[:1])}</div>
              <div class="min-w-0">
                <h3 class="text-2xl font-semibold tracking-tight text-text-heading">{e(name)}</h3>
                <p class="mt-1 text-sm text-text-body">{e(role)}</p>
              </div>
            </div>
            <p class="mt-6 text-base leading-relaxed text-text-body">{e(text)}</p>
          </article>''' for name, role, text, tone in TM['people'])
    team = f'''
    <section aria-labelledby="team-title" class="py-24">
      <div class="page-container mx-auto max-w-container px-6 lg:px-8">
        <div class="mx-auto max-w-3xl text-center">
          <h2 id="team-title" class="text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(TM["title"])}</h2>
          <p class="mt-5 text-base leading-relaxed text-text-body lg:text-lg">{e(TM["lead"])}</p>
        </div>
        <div class="mt-14 grid grid-cols-1 gap-6 md:grid-cols-3">{people}
        </div>
      </div>
    </section>
''' if TM['people'] else ''
    F = C.FAQ
    qa = ''.join(f'''
          <details class="group h-fit rounded-inner bg-surface-muted px-6 py-5 sm:px-8 sm:py-6 cursor-pointer transition-colors hover:bg-slate-200">
            <summary class="flex list-none items-center justify-between gap-6 text-lg font-semibold leading-snug tracking-tight text-text-heading sm:text-xl [&::-webkit-details-marker]:hidden">
              <span>{e(q)}</span>
              <i class="fa-solid fa-chevron-down shrink-0 text-sm text-text-muted transition-transform duration-300 group-open:rotate-180" aria-hidden="true"></i>
            </summary>
            <p class="mt-4 text-base leading-relaxed text-text-body">{md(p, a)}</p>
          </details>''' for q, a in F['items'])
    faq = f'''
    <section aria-labelledby="faq-title" class="py-24">
      <div class="page-container mx-auto max-w-container px-6 lg:px-8">
        <h2 id="faq-title" class="text-center text-4xl font-bold tracking-tight text-text-heading sm:text-5xl">{e(F["title"])}</h2>
        <div class="mt-12 grid grid-cols-1 gap-4 md:grid-cols-2 md:gap-6">{qa}
        </div>
      </div>
    </section>
''' if F['items'] else ''
    body = contact_block(p, 'h1', 'contact-title', 'contacts', intro_cls='page-intro ') + '\n' + team + faq
    faq_ld = {'@context': 'https://schema.org', '@type': 'FAQPage', 'mainEntity': [
        {'@type': 'Question', 'name': q, 'acceptedAnswer': {'@type': 'Answer', 'text': re.sub(r'\*\*|\[|\]\([^)]*\)', '', a)}} for q, a in F['items']]}
    return render(p, title=C.SEO['contacts_title'], desc=C.SEO['contacts_desc'], body=body, active='contacts',
                  jsonld=(breadcrumbs_ld([('Главная', ''), (C.CONTACTS['title'], p.path)]), faq_ld))


def legal_page(path, doc, seo_title, seo_desc):
    p = Page(path)
    F = T['footer']
    body = f'''    <section class="page-intro legal-section">
      <div class="page-container">
        <p class="legal-kicker">{e(doc["kicker"])}</p>
        <h1>{e(doc["title"])}</h1>
        <article class="legal-copy">{prose(p, doc["body"])}</article>
        <nav class="legal-navigation" aria-label="Правовые документы"><a href="{p.u("privacy/")}">{e(F["privacy"])}</a><a href="{p.u("consent/")}">{e(F["consent"])}</a></nav>
      </div>
    </section>
'''
    return render(p, title=seo_title, desc=seo_desc, body=body, kind='legal', extra_css=['css/legal.css'])


def block_page(p, title, lead, blocks, cover, active, seo_title, seo_desc):
    pic = media_img(p, cover, '', '(min-width: 1152px) 1152px, 100vw', 'h-full w-full object-cover', True) if cover else None
    cover_html = f'\n        <div class="mt-12 overflow-hidden rounded-card">{pic}</div>' if pic else ''
    body = f'''    <section class="page-intro legal-section">
      <div class="page-container">
        <h1>{e(title)}</h1>
        <p class="legal-kicker legal-lead">{e(lead)}</p>{cover_html}
        <article class="legal-copy mt-10">{prose(p, blocks)}</article>
      </div>
    </section>

{dark_cta(C.CTA_HOME, C.CTA_HOME["button"], cls="px-6 pb-24", inner="page-container ")}
'''
    return render(p, title=seo_title or f'{title} — {S["logo"]}', desc=seo_desc or lead, body=body, active=active, kind='legal',
                  extra_css=['css/legal.css'])


def about():
    A = C.ABOUT
    return block_page(Page('about/'), A['title'], A['lead'], A['body'], A.get('cover'), 'about',
                      C.SEO.get('about_title'), C.SEO.get('about_desc'))


RESERVED = {'services', 'portfolio', 'cases', 'blog', 'contacts', 'privacy', 'consent', 'about', '404', 'assets', 'api', 'admin'}


def landing(x):
    if x['slug'] in RESERVED:
        sys.exit(f'Адрес /{x["slug"]}/ занят страницей сайта — выберите другой ключ посадочной страницы')
    return block_page(Page(x['slug'] + '/'), x['title'], x['lead'], x['body'], x.get('cover'), 'l-' + x['slug'],
                      x.get('seo_title'), x.get('seo_desc'))


def not_found():
    # A fixed address so nginx can serve it for any missing page; links are
    # absolute because the page answers at every depth.
    p = Page('404/', absolute=True)
    K = T['p404']
    body = f'''    <section class="page-intro px-6 pt-24 pb-24">
      <div class="page-container mx-auto max-w-4xl">
        <h1 class="text-5xl font-extrabold leading-[1.05] tracking-tighter text-text-heading sm:text-6xl lg:text-7xl">{e(K["title"])}</h1>
        <p class="mt-6 max-w-2xl text-xl leading-relaxed text-text-body">{e(K["lead"])}</p>
        <div class="mt-10 flex flex-col items-stretch gap-3 sm:flex-row sm:items-center">
          <a href="/" class="inline-flex items-center justify-center rounded-full px-8 py-4 font-medium btn-dark">{e(K["home"])}</a>
          <a href="/portfolio/" class="inline-flex items-center justify-center gap-3 rounded-full px-8 py-4 font-medium btn-light">{e(K["link"])} <i class="fa-solid fa-arrow-right text-sm" aria-hidden="true"></i></a>
        </div>
      </div>
    </section>
'''
    return render(p, title=C.SEO['p404_title'], desc=K['lead'], body=body, index=False)


def write_favicon():
    """The default tab icon: the word mark's «a.» on the dark tile."""
    svg = ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="16" fill="#111827"/>'
           '<text x="29" y="44" text-anchor="middle" font-family="Inter,Arial,sans-serif" font-size="40" font-weight="800" fill="#fff">a</text>'
           '<circle cx="47" cy="41" r="4.5" fill="#7457E6"/></svg>\n')
    with open(os.path.join(OUT, 'assets', 'favicon.svg'), 'w', encoding='utf-8') as f:
        f.write(svg)


def write_styles():
    """site.css: the blog's states and the panel's design settings."""
    base = '''/* Generated by build.py. Blog states, then the panel's design settings. */
.liked { color: #ef4444 !important; font-weight: 900; }
.disliked { color: #64748b !important; font-weight: 900; }
.comment-link { color: rgb(var(--c-brand)); text-decoration: underline; }
.comment-link:hover { color: rgb(var(--c-brand-hover)); }
.consent-text a { color: rgb(var(--c-heading)); text-decoration: underline; }
.consent-text a:hover { color: rgb(var(--c-brand)); }
.hp { position: absolute; left: -10000px; width: 1px; height: 1px; overflow: hidden; }
.legal-page .legal-lead { font-size: 20px; line-height: 1.6; max-width: 48rem; }
.legal-copy blockquote { margin: 0 0 20px; padding-left: 20px; border-left: 3px solid rgb(var(--c-brand)); }
'''
    with open(os.path.join(OUT, 'assets', 'css', 'site.css'), 'w', encoding='utf-8') as f:
        f.write(base + design_css())


def remove_stale():
    """Deletes pages this run did not write: a case removed from content.py
    or one whose slug changed. The deletion shows in `git status`, so CI
    fails until it is committed."""
    stale = []
    for root, dirs, files in os.walk(OUT, topdown=True):
        if root == OUT:
            dirs[:] = [d for d in dirs if d != 'assets']
        for name in files:
            path = os.path.normpath(os.path.join(root, name))
            if name.endswith('.html') and path not in WRITTEN:
                stale.append(path)
    for path in sorted(stale):
        os.remove(path)
        print('removed stale page', os.path.relpath(path, OUT))
    for root, dirs, files in os.walk(OUT, topdown=False):
        if root != OUT and not os.listdir(root) and os.path.relpath(root, OUT).split(os.sep)[0] != 'assets':
            os.rmdir(root)


def main():
    write_favicon()
    write_styles()
    VERSIONS['css'] = asset_version(*CSS, 'css/home.css', 'css/legal.css')
    VERSIONS['js'] = asset_version('js/main.js', 'js/blog.js', 'js/process-flow.js')
    urls = [home(), services(), portfolio()]
    urls += [case_page(c) for c in C.CASES]
    urls += [blog(), contacts()]
    if C.ABOUT.get('show'):
        urls.append(about())
    urls += [landing(x) for x in C.LANDINGS if x.get('show', True)]
    urls.append(legal_page('privacy/', C.PRIVACY, C.SEO['privacy_title'], C.SEO['privacy_desc']))
    urls.append(legal_page('consent/', C.CONSENT, C.SEO['consent_title'], C.SEO['consent_desc']))
    not_found()
    remove_stale()
    with open(os.path.join(OUT, 'sitemap.xml'), 'w', encoding='utf-8') as f:
        f.write('<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n')
        for u in urls:
            f.write(f'<url><loc>{e(u)}</loc></url>\n')
        f.write('</urlset>\n')
    with open(os.path.join(OUT, 'robots.txt'), 'w', encoding='utf-8') as f:
        if C.SEO.get('index', True):
            f.write(f'User-agent: *\nDisallow: /api/\nDisallow: /404/\n\nSitemap: {S["url"]}/sitemap.xml\n')
        else:
            f.write('User-agent: *\nDisallow: /\n')
    print(f'{len(urls)} pages in {OUT}')


def export_content(path):
    """Writes everything content.py defines as JSON: the panel's first draft."""
    data = {k: getattr(C, k) for k in dir(C) if k.isupper()}
    with open(path, 'w', encoding='utf-8') as f:
        json.dump(data, f, ensure_ascii=False, indent=1)
        f.write('\n')


if __name__ == '__main__':
    if len(sys.argv) == 3 and sys.argv[1] == '--export-content':
        export_content(sys.argv[2])
    else:
        main()
