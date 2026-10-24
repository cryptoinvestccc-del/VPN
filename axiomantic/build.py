#!/usr/bin/env python3
"""Builds the Axiomantic studio site from content.py into site/.

    python3 axiomantic/build.py

The pages are static HTML beside the hand-written assets in site/assets.
Pictures come from the Blender renders in render/ via
site/assets/img/manifest.json, which gives every <img> its exact width
and height. Links between pages are relative, so the folder works from
any web server and any sub-path; open it through a server
(python3 -m http.server -d axiomantic/site), not as a file.

Nothing is loaded from other servers: fonts, scripts and pictures are
local, and the map on the contacts page is fetched only on request. The
output is deterministic — CI rebuilds it and fails if site/ differs from
git, or if a page refers to a picture that has not been rendered.
"""
import hashlib
import html
import json
import os
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
import legal  # noqa: E402

S = C.SITE
T = C.TEXTS
IMG = C.IMAGES
MANIFEST = {}
MISSING = set()


def e(s):
    return html.escape(str(s), quote=True)


ICONS = {
    'arrow': '<path d="M5 12h14M13 6l6 6-6 6"/>',
    'arrow-ur': '<path d="M7 17 17 7M8.5 7H17v8.5"/>',
    'arrow-down': '<path d="M12 5v14M6 13l6 6 6-6"/>',
    'check': '<path d="m5 12.5 4.5 4.5L19 7.5"/>',
    'plus': '<path d="M12 5v14M5 12h14"/>',
    'close': '<path d="M6 6l12 12M18 6 6 18"/>',
    'phone': '<path d="M6.6 3.5h2.6l1.6 4.1-2 1.3a11 11 0 0 0 6.3 6.3l1.3-2 4.1 1.6v2.6a2 2 0 0 1-2.2 2A16.5 16.5 0 0 1 4.6 5.7a2 2 0 0 1 2-2.2Z"/>',
    'chat': '<path d="M5 5.5h14a1.5 1.5 0 0 1 1.5 1.5v8.5A1.5 1.5 0 0 1 19 17h-8.5L6 20.5V17H5a1.5 1.5 0 0 1-1.5-1.5V7A1.5 1.5 0 0 1 5 5.5Z"/><path d="M8 10h8M8 13h5"/>',
    'spark': '<path d="M11 4.5 12.9 10l5.6 2-5.6 2L11 19.5 9.1 14l-5.6-2 5.6-2z"/><path d="M18.5 3v4M16.5 5h4"/>',
    'search': '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/>',
    'clock': '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
    'user': '<circle cx="12" cy="8.5" r="3.5"/><path d="M5 19.5a7 7 0 0 1 14 0"/>',
    'mail': '<rect x="3.5" y="5.5" width="17" height="13" rx="2.5"/><path d="m4.5 7.5 7.5 5.5 7.5-5.5"/>',
    'pin': '<path d="M12 21s-6.5-5.6-6.5-11a6.5 6.5 0 0 1 13 0C18.5 15.4 12 21 12 21Z"/><circle cx="12" cy="10" r="2.3"/>',
    'send': '<path d="M20.5 3.5 3.5 10.6l6.6 2.6 2.6 6.6z"/><path d="m10.1 13.2 4.6-4.6"/>',
    'layers': '<path d="m12 3.5 8.5 4.5-8.5 4.5L3.5 8z"/><path d="m3.5 12 8.5 4.5 8.5-4.5M3.5 16l8.5 4.5 8.5-4.5"/>',
    'diamond': '<path d="M12 3.5 20.5 12 12 20.5 3.5 12z"/>',
    'asterisk': '<path d="M12 3v18M4.2 7.5l15.6 9M4.2 16.5l15.6-9"/>',
    'command': '<path d="M9 6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3z"/>',
    'orbit': '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/>',
    'pause': '<path d="M9 6v12M15 6v12"/>',
    'play': '<path d="M8 5.5v13l10.5-6.5z"/>',
}


def icon(name, cls='ico'):
    return f'<svg class="{cls}" viewBox="0 0 24 24" aria-hidden="true" focusable="false">{ICONS[name]}</svg>'


# The logo mark: the brand font's «а» (Manrope ExtraBold, font units, y up)
# and its dot. The wordmark next to it is live text in the same font.
A_PATH = ('M440 -30Q324 -30 243.5 14.5Q163 59 121.5 133.5Q80 208 80 298Q80 373 103.0 435.0Q126 497 177.5 544.5Q229 592 316 624'
          'Q376 646 459.0 663.0Q542 680 647.0 695.5Q752 711 878 730L780 676Q780 772 734.0 817.0Q688 862 580 862Q520 862 455.0 833.0'
          'Q390 804 364 730L118 808Q159 942 272.0 1026.0Q385 1110 580 1110Q723 1110 834.0 1066.0Q945 1022 1002 914Q1034 854 1040.0 794.0'
          'Q1046 734 1046 660V0H808V222L842 176Q763 67 671.5 18.5Q580 -30 440 -30ZM498 184Q573 184 624.5 210.5Q676 237 706.5 271.0'
          'Q737 305 748 328Q769 372 772.5 430.5Q776 489 776 528L856 508Q735 488 660.0 474.5Q585 461 539.0 450.0Q493 439 458 426'
          'Q418 410 393.5 391.5Q369 373 357.5 351.0Q346 329 346 302Q346 265 364.5 238.5Q383 212 417.0 198.0Q451 184 498 184Z')
LOGO_MARK = (f'<svg class="logo__mark" viewBox="40 -1150 1430 1210" aria-hidden="true" focusable="false">'
             f'<path transform="scale(1 -1)" fill="currentColor" d="{A_PATH}"/>'
             f'<circle class="logo__dot" cx="1300" cy="-122" r="132"/></svg>')

MONTHS = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа',
          'сентября', 'октября', 'ноября', 'декабря']


def date_ru(iso):
    y, m, d = iso.split('-')
    return f'{int(d)} {MONTHS[int(m) - 1]} {y}'


class Page:
    """A page at a folder path such as '' or 'projects/english-school/'."""

    def __init__(self, path, absolute=False):
        self.path = path
        self.depth = path.count('/')
        self.absolute = absolute

    def u(self, target=''):
        if self.absolute:
            return '/' + target
        return '../' * self.depth + target or './'


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


def media_picture(p, mid, alt, sizes, cls, eager):
    m = media_entry(mid)
    if not m or not m.get('jpg'):
        return None
    srcset = ', '.join(f'{p.u("assets/media/" + name)} {w}w' for w, name in m['jpg'])
    load = 'loading="eager" fetchpriority="high"' if eager else 'loading="lazy"'
    alt_attr = f'alt="{e(alt)}"' if alt else 'alt="" aria-hidden="true"'
    return (f'<picture class="{cls}"><img src="{p.u("assets/media/" + m["jpg"][-1][1])}" srcset="{srcset}" sizes="{sizes}" '
            f'width="{m["w"]}" height="{m["h"]}" {alt_attr} {load} decoding="async"></picture>')


def slot(p, aid, alt='', sizes='100vw', cls='', eager=False):
    """A picture of the design: the one uploaded in the panel for this place
    (IMAGES), else the 3D render."""
    if IMG.get(aid):
        out = media_picture(p, IMG[aid], alt, sizes, cls, eager)
        if out:
            return out
    return picture(p, aid, alt=alt, sizes=sizes, cls=cls, eager=eager)


def post_picture(p, a, sizes='100vw', eager=False):
    if a.get('cover'):
        out = media_picture(p, a['cover'], '', sizes, '', eager)
        if out:
            return out
    return slot(p, 'cover-' + a['cat'], sizes=sizes, eager=eager)


def favicon_links(p):
    m = media_entry(IMG.get('favicon'))
    if m and m.get('png'):
        icons = dict((w, name) for w, name in m['png'])
        small = icons.get(32) or m['png'][0][1]
        big = icons.get(180) or m['png'][-1][1]
        return (f'<link rel="icon" href="{p.u("assets/media/" + small)}" type="image/png" sizes="32x32">\n'
                f'<link rel="apple-touch-icon" href="{p.u("assets/media/" + big)}">')
    return (f'<link rel="icon" href="{p.u("assets/favicon.svg")}" type="image/svg+xml">\n'
            f'<link rel="apple-touch-icon" href="{p.u("assets/apple-touch-icon.png")}">')


def favicon_path():
    m = media_entry(IMG.get('favicon'))
    if m and m.get('png'):
        return 'assets/media/' + m['png'][-1][1]
    return 'assets/favicon.svg'


def og_image():
    """Address, width and height of the link-preview picture."""
    m = media_entry(IMG.get('og'))
    if m and m.get('jpg'):
        w, name = m['jpg'][-1]
        return f'{S["url"]}/assets/media/{name}', w, round(m['h'] * w / m['w'])
    return f'{S["url"]}/assets/og.jpg', 1200, 630


def home_label():
    return next((label for key, label, _ in C.NAV if key == 'home'), 'Главная')


def prose_blocks(body):
    out = []
    for kind, val in body:
        if kind == 'p':
            out.append(f'<p>{e(val)}</p>')
        elif kind == 'h2':
            out.append(f'<h2>{e(val)}</h2>')
        elif kind == 'ul':
            out.append('<ul>' + ''.join(f'<li>{e(x)}</li>' for x in val) + '</ul>')
        elif kind == 'quote':
            out.append(f'<blockquote><p>{e(val)}</p></blockquote>')
    return ''.join(out)


def case_picture(p, c, alt='', sizes='100vw', eager=False):
    """A case's cover: the picture uploaded in the panel, else the 3D render."""
    if c.get('cover'):
        out = media_picture(p, c['cover'], alt, sizes, '', eager)
        if out:
            return out
    return picture(p, 'case-' + c['slug'], alt=alt, sizes=sizes, eager=eager)


def picture(p, aid, alt='', sizes='100vw', cls='', eager=False):
    """<picture> with AVIF and WebP srcsets from the render manifest; the
    <img> carries the real width/height so nothing jumps while it loads."""
    m = MANIFEST.get(aid)
    if not m:
        MISSING.add(aid)
        return f'<div class="ph {cls}" data-missing="{e(aid)}" aria-hidden="true"></div>'

    def srcset(fmt):
        return ', '.join(f'{p.u(src)} {w}w' for w, src in m[fmt])

    load = 'loading="eager" fetchpriority="high"' if eager else 'loading="lazy"'
    alt_attr = f'alt="{e(alt)}"' if alt else 'alt="" aria-hidden="true"'
    return (f'<picture class="{cls}"><source type="image/avif" srcset="{srcset("avif")}" sizes="{sizes}">'
            f'<source type="image/webp" srcset="{srcset("webp")}" sizes="{sizes}">'
            f'<img src="{p.u(m["webp"][-1][1])}" width="{m["w"]}" height="{m["h"]}" {alt_attr} {load} decoding="async"></picture>')


def words(text, cls='w'):
    """Wraps each word in a mask span so motion.js can slide words up.
    Plain text for screen readers and for pages without scripts."""
    return ' '.join(f'<span class="{cls}"><span>{e(t)}</span></span>' for t in text.split())


# ---------------------------------------------------------------- frame

def logo(p, cls=''):
    m = media_entry(IMG.get('logo'))
    if m and m.get('jpg'):
        w, name = m['jpg'][0]
        return (f'<a class="logo {cls}" href="{p.u()}"><img class="logo__img" src="{p.u("assets/media/" + name)}" '
                f'width="{w}" height="{round(m["h"] * w / m["w"])}" alt="{e(S["name"])} — на главную"></a>')
    return (f'<a class="logo {cls}" href="{p.u()}" aria-label="{e(S["name"])} — на главную">{LOGO_MARK}'
            f'<span class="logo__word">аксиомантик</span></a>')


def nav_items():
    """The menu: NAV, then «О студии» and landing pages marked for it."""
    items = list(C.NAV)
    if C.ABOUT.get('show') and C.ABOUT.get('nav'):
        items.append(('about', C.ABOUT['title'], 'about/'))
    for x in C.LANDINGS:
        if x.get('show', True) and x.get('nav'):
            items.append(('l-' + x['slug'], x.get('menu') or x['title'], x['slug'] + '/'))
    return items


def nav_links(p, active):
    """The section of the page is marked: "page" on the page itself, "true"
    on its parent section (a case or an article), so a screen reader hears
    one current page — the breadcrumb's."""
    def cur(key, href):
        if key != active:
            return ''
        return ' aria-current="page"' if href == p.path else ' aria-current="true"'
    return ''.join(f'<a href="{p.u(href)}"{cur(key, href)}>{e(label)}</a>' for key, label, href in nav_items())


def header(p, active):
    links = nav_links(p, active)
    return f'''<header class="hdr" data-hdr>
<div class="wrap hdr__in">
{logo(p)}
<nav class="nav" aria-label="Основное меню">{links}</nav>
<div class="hdr__act">
<a class="btn btn--glass hdr__cta" href="{p.u("contacts/")}" data-order>{e(T["header"]["cta"])}</a>
<button class="burger" type="button" aria-expanded="false" aria-controls="menu" data-burger><span></span><span></span><span class="sr">Меню</span></button>
</div>
</div>
<div class="menu" id="menu" hidden>
<nav class="menu__nav" aria-label="Мобильное меню">{links}</nav>
<div class="menu__foot">
<a class="btn btn--white btn--lg btn--block" href="{p.u("contacts/")}" data-order>{e(T["header"]["menu_cta"])}</a>
<a class="menu__contact" href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a>
<a class="menu__contact" href="mailto:{e(S["email"])}">{e(S["email"])}</a>
</div>
</div>
</header>'''


def socials(cls='soc'):
    return f'<ul class="{cls}">' + ''.join(
        # the name starts with the visible letters (WCAG 2.5.3, label in name)
        f'<li><a href="{e(href)}" aria-label="{e(short)} — {e(name)}" title="{e(name)}">{e(short)}</a></li>'
        for name, short, href in S['socials']) + '</ul>'


def footer(p):
    nav = ''.join(f'<li><a href="{p.u(href)}">{e(label)}</a></li>' for _, label, href in nav_items())
    F = T['footer']
    nav += f'<li><a href="{p.u("#tariffs")}">{e(F["tariffs"])}</a></li><li><a href="{p.u("#faq")}">{e(F["faq"])}</a></li>'
    services = ''.join(
        f'<li><a href="{p.u("#tariffs")}">{e(t["name"])}{" · " + e(t["sub"]) if t["id"] != "landing" else ""}</a></li>'
        for t in C.TARIFFS)
    return f'''<footer class="ftr">
<div class="wrap">
<div class="ftr__top">
<div class="ftr__brand">
{logo(p, "logo--footer")}
<p>{e(C.HERO["caption"])} {e(F["about"])}</p>
<a class="btn btn--white" href="{p.u("contacts/")}" data-order>{e(F["button"])} {icon("arrow")}</a>
</div>
<div class="ftr__col"><h2 class="ftr__h">{e(F["nav"])}</h2><ul>{nav}</ul></div>
<div class="ftr__col"><h2 class="ftr__h">{e(F["services"])}</h2><ul>{services}</ul></div>
<div class="ftr__col"><h2 class="ftr__h">{e(F["contacts"])}</h2><ul>
<li><a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></li>
<li><a href="mailto:{e(S["email"])}">{e(S["email"])}</a></li>
<li class="ftr__muted">{e(S["hours"])}</li>
</ul>{socials("soc soc--dark")}</div>
</div>
<p class="ftr__word" aria-hidden="true">аксиомантик</p>
<div class="ftr__bottom">
<span>© {S["year"]} {e(S["name"])}</span>
<a href="{p.u("privacy/")}">{e(F["privacy"])}</a>
<a href="{p.u("consent/")}">{e(F["consent"])}</a>
</div>
</div>
</footer>'''


# ---------------------------------------------------------------- forms

def picks(name, options, checked=None):
    out = []
    for value, label in options:
        chk = ' checked' if value == checked else ''
        out.append(f'<label class="pick"><input type="radio" name="{name}" value="{e(value)}"{chk}>'
                   f'<span>{e(label)}</span></label>')
    return f'<div class="picks">{"".join(out)}</div>'


def field(uid, name, label, *, kind='input', type_='text', required=True, placeholder='',
          autocomplete='', inputmode='', maxlength=120, extra='', optional_note=False):
    fid = f'{uid}-{name}'
    req = ' required' if required else ''
    attrs = f' id="{fid}" name="{name}"{req} maxlength="{maxlength}" aria-describedby="{fid}-err"'
    if placeholder:
        attrs += f' placeholder="{e(placeholder)}"'
    if autocomplete:
        attrs += f' autocomplete="{autocomplete}"'
    if inputmode:
        attrs += f' inputmode="{inputmode}"'
    attrs += extra
    opt = f' <span class="field__opt">{e(T["consent"]["optional"])}</span>' if optional_note else ''
    if kind == 'textarea':
        control = f'<textarea rows="3"{attrs}></textarea>'
    else:
        control = f'<input type="{type_}"{attrs}>'
    return (f'<div class="field"><label class="field__label" for="{fid}">{e(label)}{opt}</label>{control}'
            f'<p class="field__err" id="{fid}-err"></p></div>')


def consent(p, uid):
    K = T['consent']
    return (f'<label class="consent"><input type="checkbox" name="consent" required aria-describedby="{uid}-consent-err">'
            f'<span>{e(K["before"])} <a href="{p.u("consent/")}" target="_blank">{e(K["link"])}</a> '
            f'{e(K["middle"])} <a href="{p.u("privacy/")}" target="_blank">{e(K["policy"])}</a></span></label>'
            f'<p class="field__err" id="{uid}-consent-err"></p>')


# The lead forms are sent by main.js (fetch to SITE["endpoint"]). Their
# method is "dialog", so without the script a submit sends nothing: a POST to
# the static page would answer 405/501 and drop what the visitor typed. The
# <noscript> note says how to reach us instead.
def nojs_note():
    return (f'<noscript><p class="lead__status">{e(T["errors"]["nojs"])} '
            f'<a href="mailto:{e(S["email"])}">{e(S["email"])}</a>, {e(T["errors"]["nojs_call"])} '
            f'<a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a>.</p></noscript>')


HONEYPOT = ('<div class="hp" aria-hidden="true"><label>Не заполняйте это поле '
            '<input type="text" name="website" tabindex="-1" autocomplete="off"></label></div>')


def done_block():
    D = T['done']
    return (f'<div class="lead__done" hidden tabindex="-1">'
            f'<span class="lead__done-ico">{icon("check")}</span>'
            f'<h3 class="lead__done-title">{e(D["title"])}</h3><p class="lead__done-text" data-done-text></p>'
            f'<button type="button" class="btn btn--ghost" data-again>{e(D["again"])}</button></div>')


def project_form(p, uid, source):
    """The full lead form: on the contacts page and, when switched on, at
    the bottom of the home page. Both post as the contacts form."""
    K = T['contacts']
    return f'''<form class="lead" method="dialog" data-lead data-source="{source}" novalidate>
<input type="hidden" name="mode" value="form">
<div class="lead__form">
<div class="fields fields--2">
{field(uid, "name", K["name"], placeholder=K["name_ph"], autocomplete="name", maxlength=80)}
{field(uid, "contact", K["contact"], placeholder=K["contact_ph"], autocomplete="email", maxlength=120)}
</div>
<div class="fields">
<fieldset class="field"><legend class="field__label">{e(K["budget"])}</legend>{picks("budget", C.BUDGETS, "unknown")}</fieldset>
{field(uid, "message", K["message"], kind="textarea", placeholder=K["message_ph"], maxlength=3000)}
{HONEYPOT}
{consent(p, uid)}
</div>
<button class="btn btn--dark btn--lg lead__submit" type="submit" data-submit><span data-submit-label>{e(K["submit"])}</span>{icon("send")}</button>
<p class="lead__status" role="status" aria-live="polite"></p>
{nojs_note()}
</div>
{done_block()}
</form>'''


def body_data():
    """What main.js needs besides markup: its messages, and the counter."""
    msg = {'errors': T['errors'], 'done': T['done'],
           'submit_call': T['modal']['submit_call'], 'submit_write': T['modal']['submit_write']}
    out = f" data-msg='{e(json.dumps(msg, ensure_ascii=False, separators=(',', ':')))}'"
    if C.CONNECT.get('metrika'):
        out += f' data-metrika="{e(C.CONNECT["metrika"])}"'
        if C.CONNECT.get('webvisor'):
            out += ' data-webvisor'
    return out


def cookie_banner(p):
    """Shown only with a counter: Metrika starts after «Принять»."""
    K = C.CONNECT
    if not K.get('metrika'):
        return ''
    return (f'<div class="cookie" role="region" aria-label="cookie" data-cookie hidden><p>{e(K["cookie_text"])} '
            f'<a href="{p.u("privacy/")}">{e(K["cookie_more"])}</a></p><div class="cookie__act">'
            f'<button type="button" class="btn btn--dark btn--sm" data-cookie-yes>{e(K["cookie_accept"])}</button>'
            f'<button type="button" class="btn btn--outline btn--sm" data-cookie-no>{e(K["cookie_decline"])}</button></div></div>')


def modal(p):
    uid = 'm'
    channels = ''.join(
        f'<label class="pick"><input type="radio" name="channel" value="{v}"{" checked" if i == 0 else ""} '
        f'data-label="{e(lbl)}" data-placeholder="{e(ph)}" data-type="{t}" data-autocomplete="{ac}">'
        f'<span>{e(name)}</span></label>'
        for i, (v, name, lbl, ph, t, ac) in enumerate(C.CHANNELS))
    first = C.CHANNELS[0]
    M = T['modal']
    return f'''<dialog class="modal" id="order" aria-labelledby="order-title">
<button class="modal__close" type="button" data-close aria-label="Закрыть">{icon("close")}</button>
<div class="modal__box">
<form class="lead" method="dialog" data-lead data-source="modal" novalidate>
<div class="lead__form">
<div class="modal__head">
<h2 class="modal__title" id="order-title">{e(M["title"])}</h2>
<p class="modal__sub">{e(M["sub"].replace("{hours}", S["hours"]))}</p>
</div>
<fieldset class="switch">
<legend class="sr">Как с вами связаться</legend>
<label class="switch__opt"><input type="radio" name="mode" value="call" checked>
<span class="switch__tile"><span class="switch__ico">{icon("phone")}</span><span class="switch__txt"><b>{e(M["call"])}</b><small>{e(M["call_sub"])}</small></span></span></label>
<label class="switch__opt"><input type="radio" name="mode" value="write">
<span class="switch__tile"><span class="switch__ico">{icon("chat")}</span><span class="switch__txt"><b>{e(M["write"])}</b><small>{e(M["write_sub"])}</small></span></span></label>
</fieldset>
<div class="fields">
{field(uid, "name", M["name"], placeholder=M["name_ph"], autocomplete="name", maxlength=80)}
<fieldset class="fields__group" data-mode-fields="call">
{field(uid, "phone", M["phone"], type_="tel", placeholder="+7 (900) 000-00-00", autocomplete="tel", inputmode="tel", maxlength=24, extra=" data-phone")}
<fieldset class="field"><legend class="field__label">{e(M["when"])}</legend>{picks("when", C.CALL_TIMES, "hour")}</fieldset>
</fieldset>
<fieldset class="fields__group" data-mode-fields="write" hidden disabled>
<fieldset class="field"><legend class="field__label">{e(M["where"])}</legend><div class="picks" data-channels>{channels}</div></fieldset>
{field(uid, "contact", first[2], placeholder=first[3], autocomplete=first[5], extra=" data-contact")}
{field(uid, "message", M["message"], kind="textarea", required=False, placeholder=M["message_ph"], maxlength=3000, optional_note=True)}
</fieldset>
<fieldset class="field"><legend class="field__label">{e(M["interest"])}</legend>{picks("interest", C.INTERESTS, "unknown")}</fieldset>
{HONEYPOT}
{consent(p, uid)}
</div>
<button class="btn btn--dark btn--lg btn--block lead__submit" type="submit" data-submit>
<span data-submit-label>{e(M["submit_call"])}</span>{icon("arrow")}</button>
<p class="lead__status" role="status" aria-live="polite"></p>
</div>
{done_block()}
</form>
</div>
</dialog>'''


def demo_badge():
    if not S.get('demo'):
        return ''
    return (f'<p class="demo" role="note" data-demo><b>{e(T["demo"]["b"])}</b><span>{e(T["demo"]["text"])}</span>'
            f'<button class="demo__x" type="button" aria-label="Скрыть пометку" data-demo-close>{icon("close")}</button></p>')


def ld(obj):
    return ('<script type="application/ld+json">'
            + json.dumps(obj, ensure_ascii=False, separators=(',', ':')).replace('</', '<\\/')
            + '</script>')


def breadcrumbs_ld(items):
    return {'@context': 'https://schema.org', '@type': 'BreadcrumbList', 'itemListElement': [
        {'@type': 'ListItem', 'position': i + 1, 'name': name, 'item': S['url'] + '/' + path}
        for i, (name, path) in enumerate(items)]}


VERSIONS = {}
WRITTEN = set()  # index.html files this run wrote; anything else is stale


def render(p, *, title, desc, body, active=None, jsonld=(), og_type='website', index=True, preload=''):
    url = S['url'] + '/' + p.path
    full_title = title if title.startswith(S['name']) else f'{title} — {S["name"]}'
    robots = '' if index and C.SEO.get('index', True) else '<meta name="robots" content="noindex">\n'
    og_src, og_w, og_h = og_image()
    verify = ''
    if C.SEO.get('yandex_verification'):
        verify += f'<meta name="yandex-verification" content="{e(C.SEO["yandex_verification"])}">\n'
    if C.SEO.get('google_verification'):
        verify += f'<meta name="google-site-verification" content="{e(C.SEO["google_verification"])}">\n'
    js = lambda name: f'<script src="{p.u("assets/js/" + name)}?v={VERSIONS["js"]}" defer></script>'  # noqa: E731
    doc = f'''<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>{e(full_title)}</title>
<meta name="description" content="{e(desc)}">
{robots}<link rel="canonical" href="{e(url)}">
<meta property="og:type" content="{og_type}">
<meta property="og:site_name" content="{e(S["name"])}">
<meta property="og:locale" content="ru_RU">
<meta property="og:title" content="{e(full_title)}">
<meta property="og:description" content="{e(desc)}">
<meta property="og:url" content="{e(url)}">
<meta property="og:image" content="{e(og_src)}">
<meta property="og:image:width" content="{og_w}">
<meta property="og:image:height" content="{og_h}">
<meta name="twitter:card" content="summary_large_image">
<meta name="theme-color" content="{e(C.SEO.get("theme_color") or "#0B0E33")}">
<meta name="color-scheme" content="light">
<meta name="referrer" content="strict-origin-when-cross-origin">
{verify}{favicon_links(p)}
<link rel="preload" href="{p.u("assets/fonts/inter-cyrillic-opsz-normal.woff2")}" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="{p.u("assets/fonts/inter-latin-opsz-normal.woff2")}" as="font" type="font/woff2" crossorigin>
{preload}<link rel="stylesheet" href="{p.u("assets/css/main.css")}?v={VERSIONS["css"]}">
<script src="{p.u("assets/js/boot.js")}?v={VERSIONS["js"]}"></script>
{js("main.js")}
{js("motion.js")}
{"".join(ld(j) for j in jsonld)}
</head>
<body data-endpoint="{e(S["endpoint"])}"{body_data()}>
<a class="skip" href="#main">Перейти к содержанию</a>
{header(p, active)}
<main id="main">
{body}
</main>
{footer(p)}
{modal(p)}
{demo_badge()}{cookie_banner(p)}
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


# --------------------------------------------------------------- pieces

def shead(eyebrow, title, lead='', aside='', tone='', cls='', light=False):
    """Section heading: eyebrow, a two-tone title (words slide in), a lead."""
    t = words(title)
    if tone:
        t += ' <span class="tone">' + words(tone) + '</span>'
    lead_html = f'<p class="shead__lead" data-reveal>{e(lead)}</p>' if lead else ''
    aside_html = f'<div class="shead__aside" data-reveal>{aside}</div>' if aside else ''
    return (f'<header class="shead {cls}"><div class="shead__main"><p class="eyebrow{" eyebrow--dark" if light else ""}" data-reveal>{e(eyebrow)}</p>'
            f'<h2 class="h2{" h2--light" if light else ""}" data-split>{t}</h2>{lead_html}</div>{aside_html}</header>')


# `sizes` follow main.css. The content box of .wrap is 100vw minus a 5.6vw
# gutter each side (88.8vw) until it reaches 1600px at 1824px; in
# .section--soft it is 91.2vw and 1640px. Grids: 2 columns from 768px,
# 3 (or the 4-column bento) from 1024px (1280px for the bento); gap 24px.
# A size a little over the box only costs bytes; one under it gets a
# picture smaller than the box, so they round up.

def case_card(p, c, cls='', sizes='(min-width: 1824px) 788px, (min-width: 768px) 44.4vw, 88.8vw', h=3, eager=False):
    """h: heading level (2 where the cards sit right under the page's H1);
    eager: the first card of a list is on screen at once, so it is not lazy."""
    result_n, result_t = c['results'][0]
    return f'''<a class="case t-{c["slug"]} {cls}" href="{p.u("projects/" + c["slug"] + "/")}" data-cat="{c["type"]}" data-reveal>
<div class="case__media">{case_picture(p, c, sizes=sizes, eager=eager)}</div>
<div class="case__body">
<div class="case__meta"><span class="tag">{e(C.CATEGORIES[c["type"]])}</span><span>{e(c["term"])}</span></div>
<h{h} class="case__title">{e(c["title"])}</h{h}>
<p class="case__res"><b>{e(result_n)}</b> {e(result_t)}</p>
</div>
<span class="go">{icon("arrow-ur")}</span>
</a>'''


def post_card(p, a, cls='', sizes='(min-width: 1824px) 531px, (min-width: 1024px) 30.4vw, (min-width: 768px) 45.6vw, 92vw',
              h=3, eager=False):
    """The default sizes are for "Читайте также" under an article (.section--soft)."""
    return f'''<a class="post {cls}" href="{p.u("blog/" + a["slug"] + "/")}" data-cat="{a["cat"]}" data-reveal>
<div class="post__media">{post_picture(p, a, sizes=sizes, eager=eager)}</div>
<div class="post__body">
<div class="post__meta"><span class="tag">{e(C.BLOG_CATEGORIES[a["cat"]])}</span><time datetime="{a["date"]}">{date_ru(a["date"])}</time></div>
<h{h} class="post__title">{e(a["title"])}</h{h}>
<p class="post__desc">{e(a["desc"])}</p>
<span class="post__read">{icon("clock")}{a["read"]} {e(T["blog"]["read"])}</span>
</div>
</a>'''


def cta_scene(p):
    K = T['cta']
    return f'''<section class="scene scene--cta" aria-labelledby="cta-title">
<div class="wrap cta">
<div class="cta__text">
<p class="eyebrow eyebrow--dark" data-reveal>{icon("diamond")}{e(K["eyebrow"])}</p>
<h2 class="h2 h2--light" id="cta-title" data-split>{words(K["title"])} <span class="tone">{words(K["tone"])}</span></h2>
<p class="cta__lead" data-reveal>{e(K["lead"])}</p>
<div class="cta__act" data-reveal>
<a class="btn btn--white btn--lg" href="{p.u("contacts/")}" data-order data-mode="call">{icon("phone")}{e(K["call"])}</a>
<a class="btn btn--glass btn--lg" href="{p.u("contacts/")}" data-order data-mode="write">{icon("chat")}{e(K["write"])}</a>
</div>
</div>
<div class="cta__art" data-parallax>{slot(p, "hero", sizes="(min-width: 1280px) 460px, (min-width: 1024px) 40vw, (min-width: 508px) 360px, 72vw")}</div>
</div>
</section>'''


def page_hero(p, crumbs, title, lead='', meta=''):
    trail = ''.join(f'<a href="{p.u(href)}">{e(name)}</a><span aria-hidden="true">/</span>' for name, href in crumbs[:-1])
    trail += f'<span aria-current="page">{e(crumbs[-1][0])}</span>'
    lead_html = f'<p class="phero__lead" data-intro="2">{e(lead)}</p>' if lead else ''
    return f'''<section class="phero">
<div class="wrap phero__in">
<nav class="crumbs" aria-label="Навигационная цепочка" data-intro="1">{trail}</nav>
{meta}
<h1 class="h1" data-intro-split>{words(title)}</h1>
{lead_html}
</div>
</section>'''


def filters(label, options, total_counts, all_label):
    btns = [f'<button type="button" class="filter" data-filter="all" aria-pressed="true">{e(all_label)}<sup>{sum(total_counts.values())}</sup></button>']
    for key, name in options.items():
        btns.append(f'<button type="button" class="filter" data-filter="{key}" aria-pressed="false">{e(name)}<sup>{total_counts.get(key, 0)}</sup></button>')
    return f'<div class="filters" role="group" aria-label="{e(label)}" data-filters>{"".join(btns)}</div>'


def hero_media(p):
    """The 3D «а»: poster picture first (it is the LCP), the loop on top of it
    once it can play. motion.js picks the size and never plays it for
    reduced motion or Save-Data."""
    poster = slot(p, 'hero', alt=T['hero']['art_alt'], sizes='(min-width: 1024px) 46vw, 92vw',
                  cls='hero__poster', eager=True)
    if IMG.get('hero') or not IMG.get('hero_video', True):
        return f'<div class="hero__media">{poster}</div>'
    v = lambda n: p.u(f'assets/video/hero-loop-{n}')  # noqa: E731
    return (f'<div class="hero__media" data-hero-media>{poster}'
            f'<video class="hero__video" muted playsinline loop preload="none" aria-hidden="true" tabindex="-1" '
            f'data-webm="{v("960.webm")}" data-mp4="{v("960.mp4")}" data-webm-sm="{v("640.webm")}" data-mp4-sm="{v("640.mp4")}"></video>'
            f'</div>')


def problems_sec(p):
    K = C.PROBLEMS
    if not K.get('show'):
        return ''
    cards = ''.join(f'<li class="plain-card" data-reveal><span class="plain-card__n">{i + 1:02d}</span>'
                    f'<h3 class="plain-card__t">{e(t)}</h3><p class="plain-card__x">{e(x)}</p></li>'
                    for i, (t, x) in enumerate(K['items']))
    return (f'<section class="section" id="problems"><div class="wrap">{shead(K["eyebrow"], K["title"], tone=K["tone"])}'
            f'<ul class="plain-grid">{cards}</ul></div></section>')


def terms_sec(p):
    K = C.TERMS
    if not K.get('show'):
        return ''
    cards = ''.join(f'<li class="plain-card plain-card--soft" data-reveal><span class="plain-card__ico">{icon("check")}</span>'
                    f'<h3 class="plain-card__t">{e(t)}</h3><p class="plain-card__x">{e(x)}</p></li>'
                    for t, x in K['items'])
    return (f'<section class="section" id="terms"><div class="wrap">{shead(K["eyebrow"], K["title"], tone=K["tone"])}'
            f'<ul class="plain-grid">{cards}</ul></div></section>')


def compare_sec(p):
    K = C.COMPARE
    if not K.get('show'):
        return ''
    rows = ''.join(f'<tr><th scope="row">{e(a)}</th><td>{e(b)}</td><td class="cmp__us">{e(c)}</td></tr>' for a, b, c in K['rows'])
    return (f'<section class="section section--soft" id="compare"><div class="wrap">{shead(K["eyebrow"], K["title"], tone=K["tone"])}'
            f'<div class="cmp" data-reveal><table><thead><tr><td></td><th scope="col">{e(K["left"])}</th>'
            f'<th scope="col" class="cmp__us">{e(K["right"])}</th></tr></thead><tbody>{rows}</tbody></table></div></div></section>')


def home_form(p):
    K = C.HOME_FORM
    if not K.get('show'):
        return ''
    return (f'<section class="section" id="order-form"><div class="wrap"><div class="card card--form card--wide" data-reveal>'
            f'<h2 class="card__title">{e(K["title"])}</h2><p class="card__sub">{e(K["sub"])}</p>'
            f'{project_form(p, "h", "contacts")}</div></div></section>')


def block_page(p, title, lead, body, cover, active, seo_title, seo_desc, crumbs):
    cover_html = ''
    if cover:
        pic = media_picture(p, cover, '', '(min-width: 960px) 880px, 100vw', '', True)
        if pic:
            cover_html = f'<div class="article__cover" data-reveal>{pic}</div>'
    page = (page_hero(p, crumbs, title, lead)
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
<article class="article">
{cover_html}<div class="prose">{prose_blocks(body)}</div>
<aside class="article__cta" data-reveal>
<p><b>{e(T["blog"]["cta_b"])}</b> {e(T["blog"]["cta"])}</p>
<a class="btn btn--dark" href="{p.u("contacts/")}" data-order>{e(T["blog"]["cta_button"])} {icon("arrow")}</a>
</aside>
</article>
</div>
</section>
{cta_scene(p)}
</div>''')
    return render(p, title=seo_title or title, desc=seo_desc or lead, body=page, active=active,
                  jsonld=(breadcrumbs_ld([(name, path) for name, path in crumbs[:-1]] + [(title, p.path)]),))


def about():
    A = C.ABOUT
    p = Page('about/')
    return block_page(p, A['title'], A['lead'], A['body'], A.get('cover'), 'about',
                      C.SEO.get('about_title'), C.SEO.get('about_desc'), [(home_label(), ''), (A['title'], '')])


RESERVED = {'projects', 'blog', 'contacts', 'privacy', 'consent', 'about', '404', 'assets', 'api', 'admin'}


def landing(x):
    if x['slug'] in RESERVED:
        sys.exit(f'Адрес /{x["slug"]}/ занят страницей сайта — выберите другой ключ посадочной страницы')
    p = Page(x['slug'] + '/')
    return block_page(p, x['title'], x['lead'], x['body'], x.get('cover'), 'l-' + x['slug'],
                      x.get('seo_title'), x.get('seo_desc'), [(home_label(), ''), (x['title'], '')])


def design_css():
    """The panel's colours and sizes over the designer's tokens. Nothing is
    written while every value is empty."""
    D = C.DESIGN
    tokens = []
    rules = []
    num = lambda k: D.get(k) if isinstance(D.get(k), (int, float)) and not isinstance(D.get(k), bool) else None  # noqa: E731
    fmt = lambda v: ('%.3f' % v).rstrip('0').rstrip('.')  # noqa: E731
    if num('section') is not None:
        n = num('section')
        tokens.append(f'--section:clamp({fmt(n / 2)}px,{fmt(n / 16)}vw,{fmt(n)}px)')
    if num('wrap') is not None:
        tokens.append(f'--wrap:{fmt(num("wrap"))}px')
    if num('radius') is not None:
        n = num('radius')
        tokens.append(f'--r-xl:clamp({fmt(n * .6)}px,3.2vw,{fmt(n)}px);--r-l:{fmt(n * 2 / 3)}px;--r-m:{fmt(n / 2)}px;--r-s:{fmt(n / 3)}px')
    for key, var in (('accent', '--indigo'), ('accent_dark', '--lilac'), ('ink', '--ink'), ('muted', '--muted'),
                     ('bg', '--bg'), ('bg_soft', '--bg-2'), ('line', '--line')):
        if D.get(key):
            tokens.append(f'{var}:{D[key]}')
    if D.get('dark'):
        tokens.append(f'--hero-bg:{D["dark"]};--scene-bg:{D["dark"]};--navy-900:{D["dark"]}')
    if num('hero_pad') is not None:
        n = num('hero_pad')
        rules.append(f'.phero{{padding-top:calc(var(--hdr) + {fmt(n)}px)}}')
    if num('font_size') is not None:
        rules.append(f'body{{font-size:{fmt(num("font_size"))}px}}')
    if num('heading_ls') is not None:
        rules.append(f'.display,.h1,.h2{{letter-spacing:{fmt(num("heading_ls"))}em}}')
    if not tokens and not rules:
        return ''
    out = '\n/* panel: design settings (generated) */\n'
    if tokens:
        out += ':root{' + ';'.join(tokens) + '}\n'
    return out + '\n'.join(rules) + ('\n' if rules else '')


# ---------------------------------------------------------------- pages

def home():
    p = Page('')
    H = C.HERO
    feats = ''.join(f'<li>{icon(ic)}<span>{e(t)}</span></li>' for ic, t in C.FEATURES)
    hero = f'''<section class="hero">
<div class="wrap hero__in">
<div class="hero__text">
<p class="eyebrow eyebrow--dark" data-intro="1">{icon("diamond")}{e(H["eyebrow"])}</p>
<h1 class="display"><span class="line"><span>{e(H["lines"][0])}</span></span> <span class="line line--accent"><span>{e(H["lines"][1])}</span></span></h1>
<p class="hero__lead" data-intro="2">{e(H["lead"])}</p>
<div class="hero__cta" data-intro="3">
<a class="btn btn--white btn--xl" href="{p.u("contacts/")}" data-order>{e(T["hero"]["cta"])}</a>
<a class="link-under" href="{p.u("projects/")}">{e(T["hero"]["link"])}</a>
</div>
<p class="hero__caption" data-intro="4">{e(H["caption"])}</p>
</div>
<div class="hero__art" data-intro-art>
{hero_media(p)}
<p class="hero__art-caption">{e(H["art_caption"])}</p>
<button class="hero__pause" type="button" aria-label="Остановить анимацию" aria-pressed="false" hidden data-video-toggle>{icon("pause", "ico ico--pause")}{icon("play", "ico ico--play")}</button>
</div>
</div>
<div class="wrap"><ul class="hero__feats">{feats}</ul></div>
</section>'''

    lead_a, lead_b = C.STATEMENT
    statement = f'''<section class="section statement" aria-label="{e(S["name"])}" data-statement-section>
<div class="wrap">
<p class="statement__text" data-statement><span class="statement__a">{e(lead_a)}</span> <span class="statement__b">{words(lead_b, "sw")}</span></p>
<ul class="stats">{"".join(f'<li class="stat" data-reveal><b class="stat__n" data-count>{e(n)}</b><span class="stat__l">{e(l)}</span><span class="stat__s">{e(s)}</span></li>' for n, l, s in C.STATS)}</ul>
</div>
</section>'''

    A = T['approach']
    roles = ''.join(f'<li{" class=is-ai" if ic == "spark" else ""}>{e(name)}</li>' for ic, name, _ in C.ROLES)
    steps = ''.join(f'''<li class="astep" data-step>
<span class="astep__n">{i + 1:02d}</span>
<h3 class="astep__title">{e(title)}</h3>
<p class="astep__text">{e(text)}</p>
<dl class="astep__meta">
<div><dt>{icon("user")}{e(A["who"])}</dt><dd>{e(who)}</dd></div>
<div><dt>{icon("spark")}{e(A["ai"])}</dt><dd>{e(ai)}</dd></div>
<div><dt>{icon("clock")}{e(A["term"])}</dt><dd>{e(days)}</dd></div>
</dl>
</li>''' for i, (title, who, text, ai, days) in enumerate(C.STEPS))
    approach = f'''<section class="scene scene--approach" id="approach" aria-labelledby="approach-title">
<div class="wrap approach">
<div class="approach__side">
<div class="approach__sticky">
<p class="eyebrow eyebrow--dark" data-reveal>{icon("diamond")}{e(A["eyebrow"])}</p>
<h2 class="h2 h2--light" id="approach-title" data-split>{words(A["title"])} <span class="tone">{words(A["tone"])}</span></h2>
<ul class="formula" aria-label="Команда проекта" data-reveal>{roles}</ul>
<p class="approach__count" aria-hidden="true">{e(A["step"])} <b data-step-now>01</b> из {len(C.STEPS):02d}</p>
<div class="approach__art" data-parallax>{slot(p, "approach", sizes="(min-width: 1200px) 500px, (min-width: 1024px) 42vw, (min-width: 473px) 420px, 88.8vw")}</div>
</div>
</div>
<ol class="approach__steps">{steps}</ol>
</div>
<div class="wrap"><p class="approach__total" data-reveal>{icon("clock")}<span><b>{e(A["total_b"])}</b> {e(A["total"])}</span></p></div>
</section>'''

    V = T['services']
    svc_img = {'landing': 'svc-landing', 'multi-base': 'svc-multi', 'multi-custom': 'svc-custom'}
    services = ''.join(f'''<article class="svc" data-reveal>
<div class="svc__media">{slot(p, svc_img[sid], sizes="(min-width: 1824px) 517px, (min-width: 1024px) 29.6vw, (min-width: 768px) 44.4vw, 88.8vw")}</div>
<div class="svc__body">
<span class="badge">{icon("search")}{e(V["badge"])}</span>
<h3 class="svc__title">{e(name)}</h3>
<p class="svc__text">{e(text)}</p>
<ul class="chips">{"".join(f"<li>{e(t)}</li>" for t in tags)}</ul>
<a class="link-arrow" href="#tariffs">{e(V["link"])} {icon("arrow")}</a>
</div>
</article>''' for sid, name, text, tags in C.SERVICES)
    services_sec = f'''<section class="section" id="services">
<div class="wrap">
{shead(V["eyebrow"], V["title"], tone=V["tone"], lead=V["lead"])}
<div class="svc-grid">{services}</div>
</div>
</section>'''

    BENTO_SIZES = '(min-width: 1824px) 404px, (min-width: 1280px) 22.8vw, (min-width: 768px) 45.6vw, 92vw'
    W = T['why']
    seo_list = ''.join(f'<li>{icon("check")}{e(s)}</li>' for s in C.SEO_ITEMS)
    why = f'''<section class="section section--soft" id="why">
<div class="wrap">
{shead(W["eyebrow"], W["title"], tone=W["tone"])}
<div class="bento">
<article class="bc bc--human" data-reveal>
<div class="bc__media">{slot(p, "why-human", sizes="(min-width: 1824px) 808px, (min-width: 768px) 45.6vw, 92vw")}</div>
<div class="bc__body"><h3 class="bc__title">{e(W["human_t"])}</h3>
<p class="bc__text">{e(W["human"])}</p></div>
</article>
<article class="bc bc--fast" data-reveal>
<div class="bc__media">{slot(p, "why-fast", sizes=BENTO_SIZES)}</div>
<div class="bc__body"><h3 class="bc__title">{e(W["fast_t"])}</h3>
<p class="bc__text">{e(W["fast"])}</p></div>
</article>
<article class="bc bc--seo" data-reveal>
<div class="bc__media">{slot(p, "why-seo", sizes=BENTO_SIZES)}</div>
<div class="bc__body"><h3 class="bc__title">{e(W["seo_t"])}</h3><ul class="ticks">{seo_list}</ul></div>
</article>
<article class="bc bc--team" data-reveal>
<div class="bc__media">{slot(p, "why-team", sizes=BENTO_SIZES)}</div>
<div class="bc__body"><h3 class="bc__title">{e(W["team_t"])}</h3>
<p class="bc__text">{e(W["team"])}</p></div>
</article>
</div>
</div>
</section>'''

    featured = [c for c in C.CASES if c.get('featured')][:4]
    works = f'''<section class="section" id="works">
<div class="wrap">
{shead(T["works"]["eyebrow"], T["works"]["title"], tone=T["works"]["tone"], aside=f'<a class="btn btn--outline" href="{p.u("projects/")}">{e(T["works"]["button"])} {icon("arrow")}</a>')}
<div class="works">{"".join(case_card(p, c) for c in featured)}</div>
</div>
</section>'''

    tariffs = []
    for t in C.TARIFFS:
        pop = t.get('popular')
        feats_t = ''.join(f'<li>{icon("check")}{e(f)}</li>' for f in t['features'])
        badge = f'<span class="badge badge--pop">{e(T["tariffs"]["popular"])}</span>' if pop else ''
        price_cls = '' if t['price'][0].isdigit() else ' tariff__price--text'
        tariffs.append(f'''<article class="tariff{" tariff--pop" if pop else ""}" data-reveal>
<div class="tariff__head"><h3 class="tariff__name">{e(t["name"])}<span>{e(t["sub"])}</span></h3>{badge}</div>
<p class="tariff__price{price_cls}"><b>{e(t["price"])}</b><span>{e(t["price_note"])}</span></p>
<p class="tariff__term">{icon("clock")}{e(t["term"])}</p>
<ul class="ticks">{feats_t}</ul>
<a class="btn {"btn--white" if pop else "btn--dark"} btn--lg btn--block" href="{p.u("contacts/")}" data-order data-interest="{t["id"]}">{e(T["tariffs"]["button"])} {icon("arrow")}</a>
</article>''')
    tariffs_sec = f'''<section class="section" id="tariffs">
<div class="wrap">
{shead(T["tariffs"]["eyebrow"], T["tariffs"]["title"], tone=T["tariffs"]["tone"])}
<div class="tariffs">{"".join(tariffs)}</div>
<p class="note" data-reveal><span class="note__ico">{icon("spark")}</span><span>{e(C.TARIFF_NOTE)}</span></p>
</div>
</section>'''

    reviews = ''.join(f'''<div class="review" data-reveal>
<blockquote class="review__text">«{e(text)}»</blockquote>
<p class="review__who"><span class="avatar" aria-hidden="true">{e(name[0])}</span><span><b>{e(name)}</b><small>{e(role)}</small></span></p>
<a class="link-arrow" href="{p.u("projects/" + slug + "/")}">{e(T["reviews"]["link"])} {icon("arrow")}</a>
</div>''' for name, role, slug, text in C.REVIEWS)
    reviews_sec = f'''<section class="section section--soft" id="reviews">
<div class="wrap">
{shead(T["reviews"]["eyebrow"], T["reviews"]["title"], tone=T["reviews"]["tone"])}
<div class="reviews">{reviews}</div>
</div>
</section>'''

    qa = ''.join(f'<details class="qa"><summary class="qa__q"><span>{e(q)}</span><span class="qa__ico">{icon("plus")}</span></summary>'
                 f'<div class="qa__a"><p>{e(a)}</p></div></details>' for q, a in C.FAQ)
    faq = f'''<section class="section" id="faq">
<div class="wrap faq">
<div class="faq__head">
<p class="eyebrow" data-reveal>{e(T["faq"]["eyebrow"])}</p>
<h2 class="h2" data-split>{words(T["faq"]["title"])}</h2>
<p class="shead__lead" data-reveal>{e(T["faq"]["lead"])}</p>
<a class="btn btn--outline" href="{p.u("contacts/")}" data-order data-mode="write" data-reveal>{icon("chat")}{e(T["faq"]["button"])}</a>
</div>
<div class="faq__list" data-reveal>{qa}</div>
</div>
</section>'''

    body = (hero + '<div class="sheet">' + statement + problems_sec(p) + approach + services_sec + why + compare_sec(p)
            + works + tariffs_sec + reviews_sec + terms_sec(p) + faq + home_form(p) + cta_scene(p) + '</div>')
    org = {
        '@context': 'https://schema.org', '@type': 'Organization', 'name': S['name'],
        'alternateName': S['name_lat'], 'url': S['url'], 'logo': S['url'] + '/' + favicon_path(),
        'email': S['email'], 'telephone': S['phone_href'],
        'description': C.SEO['org_desc'],
    }
    faq_ld = {'@context': 'https://schema.org', '@type': 'FAQPage', 'mainEntity': [
        {'@type': 'Question', 'name': q, 'acceptedAnswer': {'@type': 'Answer', 'text': a}} for q, a in C.FAQ]}
    offers = {'@context': 'https://schema.org', '@type': 'ItemList', 'name': 'Тарифы', 'itemListElement': [
        {'@type': 'ListItem', 'position': i + 1, 'item': {
            '@type': 'Service', 'name': t['name'] + ('' if t['id'] == 'landing' else ' · ' + t['sub']),
            'provider': {'@type': 'Organization', 'name': S['name']}}}
        for i, t in enumerate(C.TARIFFS)]}
    hero_img = None if IMG.get('hero') else MANIFEST.get('hero')
    preload = ''
    if hero_img:
        srcset = ', '.join(f'{p.u(src)} {w}w' for w, src in hero_img['avif'])
        preload = (f'<link rel="preload" as="image" type="image/avif" imagesrcset="{srcset}" '
                   f'imagesizes="(min-width: 1024px) 46vw, 92vw" fetchpriority="high">\n')
    return render(p, title=C.SEO['home_title'], desc=C.SEO['home_desc'],
                  body=body, active='home', jsonld=(org, faq_ld, offers), preload=preload)


def projects():
    p = Page('projects/')
    counts = {}
    for c in C.CASES:
        counts[c['type']] = counts.get(c['type'], 0) + 1
    grid = ''.join(case_card(p, c, sizes='(min-width: 1024px) 33vw, (min-width: 768px) 50vw, 100vw', h=2, eager=i == 0)
                   for i, c in enumerate(C.CASES))
    P = T['projects']
    body = (page_hero(p, [(home_label(), ''), (P['title'], 'projects/')], P['title'], P['lead'])
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
{filters(P["filter"], C.CATEGORIES, counts, P["all"])}
<div class="works works--3" data-filter-grid>{grid}</div>
<p class="empty" hidden data-empty>{e(P["empty"])}</p>
</div>
</section>
{cta_scene(p)}
</div>''')
    return render(p, title=C.SEO['projects_title'], desc=C.SEO['projects_desc'],
                  body=body, active='projects', jsonld=(breadcrumbs_ld([(home_label(), ''), (P['title'], 'projects/')]),))


def case_page(i, c):
    p = Page(f'projects/{c["slug"]}/')
    nxt = C.CASES[(i + 1) % len(C.CASES)]
    K = T['case']
    P = T['projects']
    meta = (f'<div class="phero__meta" data-intro="1"><span class="tag tag--dark">{e(C.CATEGORIES[c["type"]])}</span>'
            f'<span>{icon("clock")}{e(c["term"])}</span><span>{icon("layers")}{e(c["tariff"])}</span></div>')
    solution = ''.join(f'<li>{e(s)}</li>' for s in c['solution'])
    ai = ''.join(f'<li>{icon("spark")}<span>{e(s)}</span></li>' for s in c['ai'])
    tech = ''.join(f'<li>{e(t)}</li>' for t in c['tech'])
    results = ''.join(f'<div class="kpi" data-reveal><b data-count>{e(n)}</b><span>{e(t)}</span></div>' for n, t in c['results'])
    body = (page_hero(p, [(home_label(), ''), (P['title'], 'projects/'), (c['title'], '')], c['title'], c['lead'], meta)
            + f'''<div class="sheet">
<div class="section section--first">
<div class="wrap">
<div class="case-hero t-{c["slug"]}" data-reveal>{case_picture(p, c, alt=K["cover_alt"], sizes="(min-width: 1824px) 1600px, 88.8vw", eager=True)}</div>
<div class="case-grid">
<aside class="case-facts" data-reveal>
<dl>
<div><dt>{e(K["client"])}</dt><dd>{e(c["client"])}</dd></div>
<div><dt>{e(K["type"])}</dt><dd>{e(C.CATEGORIES[c["type"]])}</dd></div>
<div><dt>{e(K["term"])}</dt><dd>{e(c["term"])}</dd></div>
<div><dt>{e(K["tariff"])}</dt><dd>{e(c["tariff"])}</dd></div>
</dl>
<h2 class="case-facts__h">{e(K["tech"])}</h2>
<ul class="chips">{tech}</ul>
<a class="btn btn--dark btn--block" href="{p.u("contacts/")}" data-order>{e(K["button"])} {icon("arrow")}</a>
</aside>
<div class="case-body">
<section class="case-part" data-reveal><h2 class="eyebrow">{e(K["task"])}</h2><p class="case-part__lead">{e(c["task"])}</p></section>
<section class="case-part" data-reveal><h2 class="eyebrow">{e(K["solution"])}</h2><ol class="numlist">{solution}</ol></section>
<section class="case-part case-part--ai" data-reveal><h2 class="eyebrow">{e(K["ai"])}</h2><ul class="ailist">{ai}</ul>
<p class="case-part__note">{icon("user")}{e(K["ai_note"])}</p></section>
<section class="case-part"><h2 class="eyebrow" data-reveal>{e(K["results"])}</h2><div class="kpis">{results}</div></section>
</div>
</div>
<a class="next t-{nxt["slug"]}" href="{p.u("projects/" + nxt["slug"] + "/")}" data-reveal>
<span class="next__label">{e(K["next"])}</span>
<span class="next__title">{e(nxt["title"])}</span>
<span class="go">{icon("arrow-ur")}</span>
</a>
</div>
</div>
{cta_scene(p)}
</div>''')
    return render(p, title=c['title'], desc=c['lead'], body=body, active='projects', og_type='article',
                  jsonld=(breadcrumbs_ld([(home_label(), ''), (P['title'], 'projects/'), (c['title'], p.path)]),))


def blog():
    p = Page('blog/')
    counts = {}
    for a in C.ARTICLES:
        counts[a['cat']] = counts.get(a['cat'], 0) + 1
    cards = ''.join(post_card(p, a, 'post--wide' if a.get('featured') else '',
                              sizes='(min-width: 1024px) 50vw, 100vw' if a.get('featured') else '(min-width: 1024px) 33vw, (min-width: 768px) 50vw, 100vw',
                              h=2, eager=i == 0)
                    for i, a in enumerate(C.ARTICLES))
    B = T['blog']
    body = (page_hero(p, [(home_label(), ''), (B['title'], 'blog/')], B['title'], B['lead'])
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
{filters(B["filter"], C.BLOG_CATEGORIES, counts, T["projects"]["all"])}
<div class="posts" data-filter-grid>{cards}</div>
<p class="empty" hidden data-empty>{e(B["empty"])}</p>
</div>
</section>
{cta_scene(p)}
</div>''')
    return render(p, title=C.SEO['blog_title'], desc=C.SEO['blog_desc'],
                  body=body, active='blog', jsonld=(breadcrumbs_ld([(home_label(), ''), (B['title'], 'blog/')]),))


def article(a):
    p = Page(f'blog/{a["slug"]}/')
    B = T['blog']
    blocks = prose_blocks(a['body'])
    same = [x for x in C.ARTICLES if x['cat'] == a['cat'] and x is not a]
    rest = [x for x in C.ARTICLES if x['cat'] != a['cat']]
    related = (same + rest)[:3]
    meta = (f'<div class="phero__meta" data-intro="1"><span class="tag tag--dark">{e(C.BLOG_CATEGORIES[a["cat"]])}</span>'
            f'<span><time datetime="{a["date"]}">{date_ru(a["date"])}</time></span><span>{icon("clock")}{a["read"]} {e(B["read"])}</span></div>')
    body = (page_hero(p, [(home_label(), ''), (B['title'], 'blog/'), (a['title'], '')], a['title'], a['desc'], meta)
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
<article class="article">
<div class="article__cover" data-reveal>{post_picture(p, a, sizes="(min-width: 960px) 880px, 100vw", eager=True)}</div>
<div class="prose">{blocks}</div>
<aside class="article__cta" data-reveal>
<p><b>{e(B["cta_b"])}</b> {e(B["cta"])}</p>
<a class="btn btn--dark" href="{p.u("contacts/")}" data-order>{e(B["cta_button"])} {icon("arrow")}</a>
</aside>
</article>
</div>
</section>
<section class="section section--soft">
<div class="wrap">
{shead(B["related_eyebrow"], B["related"], tone=B["related_tone"], aside=f'<a class="btn btn--outline" href="{p.u("blog/")}">{e(B["all"])} {icon("arrow")}</a>')}
<div class="posts">{"".join(post_card(p, x) for x in related)}</div>
</div>
</section>
</div>''')
    posting = {
        '@context': 'https://schema.org', '@type': 'BlogPosting', 'headline': a['title'], 'description': a['desc'],
        'datePublished': a['date'], 'inLanguage': 'ru', 'mainEntityOfPage': S['url'] + '/' + p.path,
        'author': {'@type': 'Organization', 'name': S['name']},
        'publisher': {'@type': 'Organization', 'name': S['name']},
    }
    return render(p, title=a['title'], desc=a['desc'], body=body, active='blog', og_type='article',
                  jsonld=(breadcrumbs_ld([(home_label(), ''), (B['title'], 'blog/'), (a['title'], p.path)]), posting))


def contacts():
    p = Page('contacts/')
    uid = 'c'
    map_src = f'https://yandex.ru/map-widget/v1/?ll={S["map_ll"]}&z={S["map_z"]}&pt={S["map_ll"]},pm2blm'
    map_link = f'https://yandex.ru/maps/?ll={S["map_ll"]}&z={S["map_z"]}&pt={S["map_ll"]}'
    K = T['contacts']
    body = (page_hero(p, [(home_label(), ''), (K['title'], 'contacts/')], K['title'], K['lead'])
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap contacts">
<div class="card card--form" data-reveal>
<h2 class="card__title">{e(K["form_title"])}</h2>
<p class="card__sub">{e(K["form_sub"])}</p>
{project_form(p, uid, "contacts")}
</div>
<div class="contacts__side">
<div class="card card--info" data-reveal>
<ul class="info">
<li><span class="info__ico">{icon("phone")}</span><span><small>{e(K["phone"])}</small><a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></span></li>
<li><span class="info__ico">{icon("mail")}</span><span><small>{e(K["email"])}</small><a href="mailto:{e(S["email"])}">{e(S["email"])}</a></span></li>
<li><span class="info__ico">{icon("clock")}</span><span><small>{e(K["hours"])}</small>{e(S["hours"])}</span></li>
<li><span class="info__ico">{icon("pin")}</span><span><small>{e(K["office"])}</small>{e(S["address"])}</span></li>
</ul>
<div class="info__soc"><small>{e(K["socials"])}</small>{socials()}</div>
</div>
<div class="map" data-map="{e(map_src)}" data-map-title="{e(K["map_title"])}" data-reveal>
<div class="map__ph" aria-hidden="true"><i class="map__road map__road--1"></i><i class="map__road map__road--2"></i><i class="map__road map__road--3"></i><i class="map__river"></i><span class="map__pin">{icon("pin")}</span></div>
<div class="map__act">
<button type="button" class="btn btn--dark btn--sm" data-map-load>{e(K["map_button"])}</button>
<a class="map__ext" href="{e(map_link)}" target="_blank" rel="noopener">{e(K["map_link"])} {icon("arrow-ur")}</a>
</div>
<p class="map__note">{e(K["map_note"])}</p>
</div>
</div>
</div>
</section>
</div>''')
    return render(p, title=C.SEO['contacts_title'], desc=C.SEO['contacts_desc'],
                  body=body, active='contacts', jsonld=(breadcrumbs_ld([(home_label(), ''), (K['title'], 'contacts/')]),))


def legal_page(path, title, lead, sections):
    p = Page(path)
    blocks = []
    for h, items in sections:
        blocks.append(f'<h2>{e(h)}</h2>')
        for it in items:
            if isinstance(it, list):
                blocks.append('<ul>' + ''.join(f'<li>{e(x)}</li>' for x in it) + '</ul>')
            else:
                blocks.append(f'<p>{e(it)}</p>')
    body = (page_hero(p, [(home_label(), ''), (title, '')], title, lead)
            + f'''<div class="sheet"><section class="section section--first"><div class="wrap">
<article class="article"><div class="prose prose--legal">{"".join(blocks)}</div></article>
</div></section></div>''')
    return render(p, title=title, desc=lead, body=body)


def not_found():
    # A fixed address so nginx can serve it for any missing page; links are
    # absolute because the page answers at every depth.
    p = Page('404/', absolute=True)
    K = T['p404']
    body = f'''<section class="phero phero--404">
<div class="wrap phero__in p404">
<div>
<p class="eyebrow eyebrow--dark" data-intro="1">{icon("diamond")}{e(K["eyebrow"])}</p>
<h1 class="h1" data-intro-split>{words(K["title"])}</h1>
<p class="phero__lead" data-intro="2">{e(K["lead"])}</p>
<div class="hero__cta" data-intro="3"><a class="btn btn--white btn--lg" href="/">{e(K["home"])} {icon("arrow")}</a><a class="link-under" href="/projects/">{e(K["link"])}</a></div>
</div>
<div class="p404__art" data-intro-art><div class="hero__media">{slot(p, "hero", sizes="(min-width: 1024px) 40vw, 80vw")}</div></div>
</div>
</section>'''
    return render(p, title=C.SEO['p404_title'], desc=C.SEO['p404_title'] + '.', body=body, index=False)


def write_favicon():
    """The tab icon is the logo mark on a navy tile, from the same glyph path."""
    svg = ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">'
           '<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">'
           '<stop offset="0" stop-color="#2A3196"/><stop offset="1" stop-color="#0B0E33"/></linearGradient></defs>'
           '<rect width="64" height="64" rx="16" fill="url(#g)"/>'
           '<g transform="translate(32 33.5) scale(.034) translate(-756 540)">'
           f'<path transform="scale(1 -1)" fill="#fff" d="{A_PATH}"/>'
           '<circle cx="1300" cy="-122" r="132" fill="#A9ACF7"/></g></svg>\n')
    with open(os.path.join(OUT, 'assets', 'favicon.svg'), 'w', encoding='utf-8') as f:
        f.write(svg)


def write_themes():
    rules = [f'.t-{c["slug"]}{{--c1:{c["colors"][0]};--c2:{c["colors"][1]};--c3:{c["colors"][2]}}}' for c in C.CASES]
    path = os.path.join(OUT, 'assets', 'css', 'themes.css')
    with open(path, 'w', encoding='utf-8') as f:
        f.write('/* Generated by build.py from content.py: colours of each case. */\n' + '\n'.join(rules) + '\n')


def remove_stale():
    """Deletes pages this run did not write: a case or an article removed
    from content.py, or one whose slug changed. Without this the old page
    would stay in git and keep being deployed, and CI's check that site/ is
    exactly what the sources give would pass. The deletion shows in
    `git status`, so CI fails until it is committed."""
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
    # folders left empty by the deletions (deepest first)
    for root, dirs, files in os.walk(OUT, topdown=False):
        if root != OUT and not os.listdir(root) and os.path.relpath(root, OUT).split(os.sep)[0] != 'assets':
            os.rmdir(root)


def main():
    global MANIFEST
    path = os.path.join(OUT, 'assets', 'img', 'manifest.json')
    if os.path.exists(path):
        with open(path, encoding='utf-8') as f:
            MANIFEST = json.load(f)
    write_favicon()
    write_themes()
    # one stylesheet: the case colours are appended so pages make one request
    with open(os.path.join(OUT, 'assets', 'css', 'themes.css'), encoding='utf-8') as f:
        themes = f.read()
    css_path = os.path.join(OUT, 'assets', 'css', 'main.css')
    with open(css_path, encoding='utf-8') as f:
        css = f.read()
    marker = '/* --- case colours (generated) --- */\n'
    css = css.split(marker)[0].rstrip('\n') + '\n\n' + marker + themes + design_css()
    with open(css_path, 'w', encoding='utf-8') as f:
        f.write(css)
    os.remove(os.path.join(OUT, 'assets', 'css', 'themes.css'))
    VERSIONS['css'] = asset_version('css/main.css')
    VERSIONS['js'] = asset_version('js/boot.js', 'js/main.js', 'js/motion.js', 'js/vendor/gsap.min.js', 'js/vendor/ScrollTrigger.min.js')
    urls = [home(), projects()]
    urls += [case_page(i, c) for i, c in enumerate(C.CASES)]
    urls.append(blog())
    urls += [article(a) for a in C.ARTICLES]
    urls.append(contacts())
    L = T['legal']
    if C.ABOUT.get('show'):
        urls.append(about())
    urls += [landing(x) for x in C.LANDINGS if x.get('show', True)]
    urls.append(legal_page('privacy/', L['privacy_title'], L['privacy_lead'], legal.PRIVACY))
    urls.append(legal_page('consent/', L['consent_title'], L['consent_lead'], legal.CONSENT))
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
    if MISSING:
        print('pictures not rendered yet:', ', '.join(sorted(MISSING)))
        if not os.environ.get('AXM_ALLOW_MISSING'):
            sys.exit(1)


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
