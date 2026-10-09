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
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, 'site')
sys.path.insert(0, HERE)
sys.dont_write_bytecode = True  # no __pycache__ beside the sources
import content as C  # noqa: E402
import legal  # noqa: E402

S = C.SITE
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
    return (f'<a class="logo {cls}" href="{p.u()}" aria-label="{e(S["name"])} — на главную">{LOGO_MARK}'
            f'<span class="logo__word">аксиомантик</span></a>')


def nav_links(p, active):
    return ''.join(
        f'<a href="{p.u(href)}"{" aria-current=page" if key == active else ""}>{e(label)}</a>'
        for key, label, href in C.NAV)


def header(p, active):
    links = nav_links(p, active)
    return f'''<header class="hdr" data-hdr>
<div class="wrap hdr__in">
{logo(p)}
<nav class="nav" aria-label="Основное меню">{links}</nav>
<div class="hdr__act">
<a class="btn btn--glass hdr__cta" href="{p.u("contacts/")}" data-order>Заказать</a>
<button class="burger" type="button" aria-expanded="false" aria-controls="menu" data-burger><span></span><span></span><span class="sr">Меню</span></button>
</div>
</div>
<div class="menu" id="menu" hidden>
<nav class="menu__nav" aria-label="Мобильное меню">{links}</nav>
<div class="menu__foot">
<a class="btn btn--white btn--lg btn--block" href="{p.u("contacts/")}" data-order>Обсудить проект</a>
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
    nav = ''.join(f'<li><a href="{p.u(href)}">{e(label)}</a></li>' for _, label, href in C.NAV)
    nav += f'<li><a href="{p.u("#tariffs")}">Тарифы</a></li><li><a href="{p.u("#faq")}">Вопросы и ответы</a></li>'
    services = ''.join(
        f'<li><a href="{p.u("#tariffs")}">{e(t["name"])}{" · " + e(t["sub"]) if t["id"] != "landing" else ""}</a></li>'
        for t in C.TARIFFS)
    return f'''<footer class="ftr">
<div class="wrap">
<div class="ftr__top">
<div class="ftr__brand">
{logo(p, "logo--footer")}
<p>{e(C.HERO["caption"])} Маркетолог, разработчик и UX/UI-дизайнер в связке с AI.</p>
<a class="btn btn--white" href="{p.u("contacts/")}" data-order>Обсудить проект {icon("arrow")}</a>
</div>
<div class="ftr__col"><h2 class="ftr__h">Разделы</h2><ul>{nav}</ul></div>
<div class="ftr__col"><h2 class="ftr__h">Услуги</h2><ul>{services}</ul></div>
<div class="ftr__col"><h2 class="ftr__h">Контакты</h2><ul>
<li><a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></li>
<li><a href="mailto:{e(S["email"])}">{e(S["email"])}</a></li>
<li class="ftr__muted">{e(S["hours"])}</li>
</ul>{socials("soc soc--dark")}</div>
</div>
<p class="ftr__word" aria-hidden="true">аксиомантик</p>
<div class="ftr__bottom">
<span>© {S["year"]} {e(S["name"])}</span>
<a href="{p.u("privacy/")}">Политика обработки персональных данных</a>
<a href="{p.u("consent/")}">Согласие на обработку данных</a>
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
    opt = ' <span class="field__opt">необязательно</span>' if optional_note else ''
    if kind == 'textarea':
        control = f'<textarea rows="3"{attrs}></textarea>'
    else:
        control = f'<input type="{type_}"{attrs}>'
    return (f'<div class="field"><label class="field__label" for="{fid}">{e(label)}{opt}</label>{control}'
            f'<p class="field__err" id="{fid}-err"></p></div>')


def consent(p, uid):
    return (f'<label class="consent"><input type="checkbox" name="consent" required aria-describedby="{uid}-consent-err">'
            f'<span>Даю <a href="{p.u("consent/")}" target="_blank">согласие на обработку персональных данных</a> '
            f'на условиях <a href="{p.u("privacy/")}" target="_blank">политики</a></span></label>'
            f'<p class="field__err" id="{uid}-consent-err"></p>')


HONEYPOT = ('<div class="hp" aria-hidden="true"><label>Не заполняйте это поле '
            '<input type="text" name="website" tabindex="-1" autocomplete="off"></label></div>')


def done_block(title='Заявка отправлена'):
    return (f'<div class="lead__done" hidden tabindex="-1">'
            f'<span class="lead__done-ico">{icon("check")}</span>'
            f'<h3 class="lead__done-title">{e(title)}</h3><p class="lead__done-text" data-done-text></p>'
            f'<button type="button" class="btn btn--ghost" data-again>Отправить ещё одну</button></div>')


def modal(p):
    uid = 'm'
    channels = ''.join(
        f'<label class="pick"><input type="radio" name="channel" value="{v}"{" checked" if i == 0 else ""} '
        f'data-label="{e(lbl)}" data-placeholder="{e(ph)}" data-type="{t}" data-autocomplete="{ac}">'
        f'<span>{e(name)}</span></label>'
        for i, (v, name, lbl, ph, t, ac) in enumerate(C.CHANNELS))
    first = C.CHANNELS[0]
    return f'''<dialog class="modal" id="order" aria-labelledby="order-title">
<button class="modal__close" type="button" data-close aria-label="Закрыть">{icon("close")}</button>
<div class="modal__box">
<form class="lead" method="post" data-lead data-source="modal" novalidate>
<div class="lead__form">
<div class="modal__head">
<h2 class="modal__title" id="order-title">Обсудим проект</h2>
<p class="modal__sub">Выберите, как удобнее связаться. Ответим в рабочее время: {e(S["hours"])}.</p>
</div>
<fieldset class="switch">
<legend class="sr">Как с вами связаться</legend>
<label class="switch__opt"><input type="radio" name="mode" value="call" checked>
<span class="switch__tile"><span class="switch__ico">{icon("phone")}</span><span class="switch__txt"><b>Позвонить</b><small>Перезвоним в удобное время</small></span></span></label>
<label class="switch__opt"><input type="radio" name="mode" value="write">
<span class="switch__tile"><span class="switch__ico">{icon("chat")}</span><span class="switch__txt"><b>Написать</b><small>Ответим в мессенджере или на почту</small></span></span></label>
</fieldset>
<div class="fields">
{field(uid, "name", "Как к вам обращаться", placeholder="Имя", autocomplete="name", maxlength=80)}
<fieldset class="fields__group" data-mode-fields="call">
{field(uid, "phone", "Телефон", type_="tel", placeholder="+7 (900) 000-00-00", autocomplete="tel", inputmode="tel", maxlength=24, extra=" data-phone")}
<fieldset class="field"><legend class="field__label">Когда удобно</legend>{picks("when", C.CALL_TIMES, "hour")}</fieldset>
</fieldset>
<fieldset class="fields__group" data-mode-fields="write" hidden disabled>
<fieldset class="field"><legend class="field__label">Куда ответить</legend><div class="picks" data-channels>{channels}</div></fieldset>
{field(uid, "contact", first[2], placeholder=first[3], autocomplete=first[5], extra=" data-contact")}
{field(uid, "message", "Коротко о задаче", kind="textarea", required=False, placeholder="Например: лендинг для запуска курса, нужен к марту", maxlength=3000, optional_note=True)}
</fieldset>
<fieldset class="field"><legend class="field__label">Что интересует</legend>{picks("interest", C.INTERESTS, "unknown")}</fieldset>
{HONEYPOT}
{consent(p, uid)}
</div>
<button class="btn btn--dark btn--lg btn--block lead__submit" type="submit" data-submit>
<span data-submit-label>Жду звонка</span>{icon("arrow")}</button>
<p class="lead__status" role="status" aria-live="polite"></p>
</div>
{done_block()}
</form>
</div>
</dialog>'''


def demo_badge():
    if not S.get('demo'):
        return ''
    return ('<p class="demo" role="note" data-demo><b>Демо</b><span>Цены, кейсы, отзывы, цифры и контакты — '
            'примеры. Заменить перед запуском.</span>'
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


def render(p, *, title, desc, body, active=None, jsonld=(), og_type='website', index=True, preload=''):
    url = S['url'] + '/' + p.path
    full_title = title if title.startswith(S['name']) else f'{title} — {S["name"]}'
    robots = '' if index else '<meta name="robots" content="noindex">\n'
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
<meta property="og:image" content="{e(S["url"])}/assets/og.jpg">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta name="twitter:card" content="summary_large_image">
<meta name="theme-color" content="#0B0E33">
<meta name="color-scheme" content="light">
<meta name="referrer" content="strict-origin-when-cross-origin">
<link rel="icon" href="{p.u("assets/favicon.svg")}" type="image/svg+xml">
<link rel="apple-touch-icon" href="{p.u("assets/apple-touch-icon.png")}">
<link rel="preload" href="{p.u("assets/fonts/inter-cyrillic-opsz-normal.woff2")}" as="font" type="font/woff2" crossorigin>
{preload}<link rel="stylesheet" href="{p.u("assets/css/main.css")}?v={VERSIONS["css"]}">
<script src="{p.u("assets/js/boot.js")}?v={VERSIONS["js"]}"></script>
{js("main.js")}
{js("motion.js")}
{"".join(ld(j) for j in jsonld)}
</head>
<body data-endpoint="{e(S["endpoint"])}">
<a class="skip" href="#main">Перейти к содержанию</a>
{header(p, active)}
<main id="main">
{body}
</main>
{footer(p)}
{modal(p)}
{demo_badge()}
</body>
</html>
'''
    out_dir = os.path.join(OUT, p.path)
    os.makedirs(out_dir, exist_ok=True)
    with open(os.path.join(out_dir, 'index.html'), 'w', encoding='utf-8') as f:
        f.write(doc)
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


def case_card(p, c, cls='', sizes='(min-width: 1024px) 50vw, 100vw'):
    result_n, result_t = c['results'][0]
    return f'''<a class="case t-{c["slug"]} {cls}" href="{p.u("projects/" + c["slug"] + "/")}" data-cat="{c["type"]}" data-reveal>
<div class="case__media">{picture(p, "case-" + c["slug"], sizes=sizes)}</div>
<div class="case__body">
<div class="case__meta"><span class="tag">{e(C.CATEGORIES[c["type"]])}</span><span>{e(c["term"])}</span></div>
<h3 class="case__title">{e(c["title"])}</h3>
<p class="case__res"><b>{e(result_n)}</b> {e(result_t)}</p>
</div>
<span class="go">{icon("arrow-ur")}</span>
</a>'''


def post_card(p, a, cls='', sizes='(min-width: 1024px) 33vw, 100vw'):
    return f'''<a class="post {cls}" href="{p.u("blog/" + a["slug"] + "/")}" data-cat="{a["cat"]}" data-reveal>
<div class="post__media">{picture(p, "cover-" + a["cat"], sizes=sizes)}</div>
<div class="post__body">
<div class="post__meta"><span class="tag">{e(C.BLOG_CATEGORIES[a["cat"]])}</span><time datetime="{a["date"]}">{date_ru(a["date"])}</time></div>
<h3 class="post__title">{e(a["title"])}</h3>
<p class="post__desc">{e(a["desc"])}</p>
<span class="post__read">{icon("clock")}{a["read"]} мин чтения</span>
</div>
</a>'''


def cta_scene(p):
    return f'''<section class="scene scene--cta" aria-labelledby="cta-title">
<div class="wrap cta">
<div class="cta__text">
<p class="eyebrow eyebrow--dark" data-reveal>{icon("diamond")}Новый проект</p>
<h2 class="h2 h2--light" id="cta-title" data-split>{words("Обсудим ваш")} <span class="tone">{words("сайт?")}</span></h2>
<p class="cta__lead" data-reveal>Расскажите о задаче — предложим формат, сроки и стоимость в течение рабочего дня. Базовое SEO уже в цене.</p>
<div class="cta__act" data-reveal>
<a class="btn btn--white btn--lg" href="{p.u("contacts/")}" data-order data-mode="call">{icon("phone")}Позвонить</a>
<a class="btn btn--glass btn--lg" href="{p.u("contacts/")}" data-order data-mode="write">{icon("chat")}Написать</a>
</div>
</div>
<div class="cta__art" data-parallax>{picture(p, "hero", sizes="(min-width: 1024px) 40vw, 80vw")}</div>
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


def filters(label, options, total_counts):
    btns = [f'<button type="button" class="filter" data-filter="all" aria-pressed="true">Все<sup>{sum(total_counts.values())}</sup></button>']
    for key, name in options.items():
        btns.append(f'<button type="button" class="filter" data-filter="{key}" aria-pressed="false">{e(name)}<sup>{total_counts.get(key, 0)}</sup></button>')
    return f'<div class="filters" role="group" aria-label="{e(label)}" data-filters>{"".join(btns)}</div>'


def hero_media(p):
    """The 3D «а»: poster picture first (it is the LCP), the loop on top of it
    once it can play. motion.js picks the size and never plays it for
    reduced motion or Save-Data."""
    poster = picture(p, 'hero', alt='Объёмная буква «а» из логотипа студии', sizes='(min-width: 1024px) 46vw, 92vw',
                     cls='hero__poster', eager=True)
    v = lambda n: p.u(f'assets/video/hero-loop-{n}')  # noqa: E731
    return (f'<div class="hero__media" data-hero-media>{poster}'
            f'<video class="hero__video" muted playsinline loop preload="none" aria-hidden="true" tabindex="-1" '
            f'data-webm="{v("960.webm")}" data-mp4="{v("960.mp4")}" data-webm-sm="{v("640.webm")}" data-mp4-sm="{v("640.mp4")}"></video>'
            f'</div>')


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
<a class="btn btn--white btn--xl" href="{p.u("contacts/")}" data-order>Обсудить проект</a>
<a class="link-under" href="{p.u("projects/")}">Смотреть проекты</a>
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
    statement = f'''<section class="section statement" aria-label="О студии" data-statement-section>
<div class="wrap">
<p class="statement__text" data-statement><span class="statement__a">{e(lead_a)}</span> <span class="statement__b">{words(lead_b, "sw")}</span></p>
<ul class="stats">{"".join(f'<li class="stat" data-reveal><b class="stat__n" data-count>{e(n)}</b><span class="stat__l">{e(l)}</span><span class="stat__s">{e(s)}</span></li>' for n, l, s in C.STATS)}</ul>
</div>
</section>'''

    roles = ''.join(f'<li{" class=is-ai" if ic == "spark" else ""}>{e(name)}</li>' for ic, name, _ in C.ROLES)
    steps = ''.join(f'''<li class="astep" data-step>
<span class="astep__n">{i + 1:02d}</span>
<h3 class="astep__title">{e(title)}</h3>
<p class="astep__text">{e(text)}</p>
<dl class="astep__meta">
<div><dt>{icon("user")}Делает</dt><dd>{e(who)}</dd></div>
<div><dt>{icon("spark")}AI ускоряет</dt><dd>{e(ai)}</dd></div>
<div><dt>{icon("clock")}Срок</dt><dd>{e(days)}</dd></div>
</dl>
</li>''' for i, (title, who, text, ai, days) in enumerate(C.STEPS))
    approach = f'''<section class="scene scene--approach" id="approach" aria-labelledby="approach-title">
<div class="wrap approach">
<div class="approach__side">
<div class="approach__sticky">
<p class="eyebrow eyebrow--dark" data-reveal>{icon("diamond")}Наш подход</p>
<h2 class="h2 h2--light" id="approach-title" data-split>{words("AI ускоряет рутину.")} <span class="tone">{words("Люди делают продукт.")}</span></h2>
<ul class="formula" aria-label="Команда проекта" data-reveal>{roles}</ul>
<p class="approach__count" aria-hidden="true">Этап <b data-step-now>01</b> из {len(C.STEPS):02d}</p>
<div class="approach__art" data-parallax>{picture(p, "approach", sizes="(min-width: 1024px) 40vw, 90vw")}</div>
</div>
</div>
<ol class="approach__steps">{steps}</ol>
</div>
<div class="wrap"><p class="approach__total" data-reveal>{icon("clock")}<span><b>10 рабочих дней</b> — путь лендинга от брифа до запуска</span></p></div>
</section>'''

    svc_img = {'landing': 'svc-landing', 'multi-base': 'svc-multi', 'multi-custom': 'svc-custom'}
    services = ''.join(f'''<article class="svc" data-reveal>
<div class="svc__media">{picture(p, svc_img[sid], sizes="(min-width: 1024px) 33vw, 100vw")}</div>
<div class="svc__body">
<span class="badge">{icon("search")}Базовое SEO включено</span>
<h3 class="svc__title">{e(name)}</h3>
<p class="svc__text">{e(text)}</p>
<ul class="chips">{"".join(f"<li>{e(t)}</li>" for t in tags)}</ul>
<a class="link-arrow" href="#tariffs">Тариф и стоимость {icon("arrow")}</a>
</div>
</article>''' for sid, name, text, tags in C.SERVICES)
    services_sec = f'''<section class="section" id="services">
<div class="wrap">
{shead("Услуги", "Что мы делаем", tone="— от страницы до продукта", lead="Три формата под разные задачи. В каждый уже входит работа маркетолога, UX/UI-дизайнера и разработчика, а также базовое SEO.")}
<div class="svc-grid">{services}</div>
</div>
</section>'''

    seo_list = ''.join(f'<li>{icon("check")}{e(s)}</li>' for s in C.SEO_ITEMS)
    why = f'''<section class="section section--soft" id="why">
<div class="wrap">
{shead("Почему мы", "Быстро и качественно —", tone="потому что дизайн делает человек, а рутину берёт AI")}
<div class="bento">
<article class="bc bc--human" data-reveal>
<div class="bc__media">{picture(p, "why-human", sizes="(min-width: 1024px) 40vw, 100vw")}</div>
<div class="bc__body"><h3 class="bc__title">Дизайн рисует человек, а не нейросеть</h3>
<p class="bc__text">AI не генерирует макеты. Каждый экран проектирует UX/UI-дизайнер под вашу задачу и аудиторию — поэтому сайт не похож на шаблон и ведёт к заявке.</p></div>
</article>
<article class="bc bc--fast" data-reveal>
<div class="bc__media">{picture(p, "why-fast", sizes="(min-width: 1024px) 25vw, 100vw")}</div>
<div class="bc__body"><h3 class="bc__title">Быстро</h3>
<p class="bc__text">AI снимает рутину на каждом этапе — лендинг от 10 рабочих дней вместо 4–6 недель.</p></div>
</article>
<article class="bc bc--seo" data-reveal>
<div class="bc__media">{picture(p, "why-seo", sizes="(min-width: 1024px) 25vw, 100vw")}</div>
<div class="bc__body"><h3 class="bc__title">SEO уже в цене</h3><ul class="ticks">{seo_list}</ul></div>
</article>
<article class="bc bc--team" data-reveal>
<div class="bc__media">{picture(p, "why-team", sizes="(min-width: 1024px) 25vw, 100vw")}</div>
<div class="bc__body"><h3 class="bc__title">Одно окно</h3>
<p class="bc__text">Маркетолог, дизайнер, разработчик и менеджер проекта — в одном чате с вами. Ревью дизайна и кода людьми, автотесты форм и вёрстки.</p></div>
</article>
</div>
</div>
</section>'''

    featured = [c for c in C.CASES if c.get('featured')][:4]
    works = f'''<section class="section" id="works">
<div class="wrap">
{shead("Портфолио", "Избранные", tone="проекты", aside=f'<a class="btn btn--outline" href="{p.u("projects/")}">Все проекты {icon("arrow")}</a>')}
<div class="works">{"".join(case_card(p, c) for c in featured)}</div>
</div>
</section>'''

    tariffs = []
    for t in C.TARIFFS:
        pop = t.get('popular')
        feats_t = ''.join(f'<li>{icon("check")}{e(f)}</li>' for f in t['features'])
        badge = '<span class="badge badge--pop">Популярный</span>' if pop else ''
        price_cls = '' if t['price'][0].isdigit() else ' tariff__price--text'
        tariffs.append(f'''<article class="tariff{" tariff--pop" if pop else ""}" data-reveal>
<div class="tariff__head"><h3 class="tariff__name">{e(t["name"])}<span>{e(t["sub"])}</span></h3>{badge}</div>
<p class="tariff__price{price_cls}"><b>{e(t["price"])}</b><span>{e(t["price_note"])}</span></p>
<p class="tariff__term">{icon("clock")}{e(t["term"])}</p>
<ul class="ticks">{feats_t}</ul>
<a class="btn {"btn--white" if pop else "btn--dark"} btn--lg btn--block" href="{p.u("contacts/")}" data-order data-interest="{t["id"]}">Заказать {icon("arrow")}</a>
</article>''')
    tariffs_sec = f'''<section class="section" id="tariffs">
<div class="wrap">
{shead("Тарифы", "Прозрачные цены —", tone="команда и SEO уже внутри")}
<div class="tariffs">{"".join(tariffs)}</div>
<p class="note" data-reveal><span class="note__ico">{icon("spark")}</span><span>{e(C.TARIFF_NOTE)}</span></p>
</div>
</section>'''

    reviews = ''.join(f'''<figure class="review" data-reveal>
<blockquote class="review__text">«{e(text)}»</blockquote>
<figcaption class="review__who"><span class="avatar">{e(name[0])}</span><span><b>{e(name)}</b><small>{e(role)}</small></span></figcaption>
<a class="link-arrow" href="{p.u("projects/" + slug + "/")}">Смотреть кейс {icon("arrow")}</a>
</figure>''' for name, role, slug, text in C.REVIEWS)
    reviews_sec = f'''<section class="section section--soft" id="reviews">
<div class="wrap">
{shead("Отзывы", "Что говорят", tone="клиенты")}
<div class="reviews">{reviews}</div>
</div>
</section>'''

    qa = ''.join(f'<details class="qa"><summary class="qa__q"><span>{e(q)}</span><span class="qa__ico">{icon("plus")}</span></summary>'
                 f'<div class="qa__a"><p>{e(a)}</p></div></details>' for q, a in C.FAQ)
    faq = f'''<section class="section" id="faq">
<div class="wrap faq">
<div class="faq__head">
<p class="eyebrow" data-reveal>Вопросы и ответы</p>
<h2 class="h2" data-split>{words("Частые вопросы")}</h2>
<p class="shead__lead" data-reveal>Не нашли ответ? Напишите — ответим в течение рабочего дня.</p>
<a class="btn btn--outline" href="{p.u("contacts/")}" data-order data-mode="write" data-reveal>{icon("chat")}Задать вопрос</a>
</div>
<div class="faq__list" data-reveal>{qa}</div>
</div>
</section>'''

    body = (hero + '<div class="sheet">' + statement + approach + services_sec + why + works
            + tariffs_sec + reviews_sec + faq + cta_scene(p) + '</div>')
    org = {
        '@context': 'https://schema.org', '@type': 'Organization', 'name': S['name'],
        'alternateName': S['name_lat'], 'url': S['url'], 'logo': S['url'] + '/assets/favicon.svg',
        'email': S['email'], 'telephone': S['phone_href'],
        'description': 'Студия цифровых продуктов: лендинги, многостраничные сайты и интернет-магазины с базовым SEO.',
    }
    faq_ld = {'@context': 'https://schema.org', '@type': 'FAQPage', 'mainEntity': [
        {'@type': 'Question', 'name': q, 'acceptedAnswer': {'@type': 'Answer', 'text': a}} for q, a in C.FAQ]}
    offers = {'@context': 'https://schema.org', '@type': 'ItemList', 'name': 'Тарифы', 'itemListElement': [
        {'@type': 'ListItem', 'position': i + 1, 'item': {
            '@type': 'Service', 'name': t['name'] + ('' if t['id'] == 'landing' else ' · ' + t['sub']),
            'provider': {'@type': 'Organization', 'name': S['name']}}}
        for i, t in enumerate(C.TARIFFS)]}
    hero_img = MANIFEST.get('hero')
    preload = ''
    if hero_img:
        srcset = ', '.join(f'{p.u(src)} {w}w' for w, src in hero_img['avif'])
        preload = (f'<link rel="preload" as="image" type="image/avif" imagesrcset="{srcset}" '
                   f'imagesizes="(min-width: 1024px) 46vw, 92vw" fetchpriority="high">\n')
    return render(p, title=f'{S["name"]} — сайты, которые двигают бизнес',
                  desc='Студия «Аксиомантик»: лендинги и многостраничные сайты с индивидуальным дизайном от человека. Маркетолог, разработчик, UX/UI-дизайнер и AI в одной команде. Базовое SEO включено в стоимость.',
                  body=body, active='home', jsonld=(org, faq_ld, offers), preload=preload)


def projects():
    p = Page('projects/')
    counts = {}
    for c in C.CASES:
        counts[c['type']] = counts.get(c['type'], 0) + 1
    grid = ''.join(case_card(p, c, sizes='(min-width: 1024px) 33vw, (min-width: 768px) 50vw, 100vw') for c in C.CASES)
    body = (page_hero(p, [('Главная', ''), ('Проекты', 'projects/')], 'Проекты',
                      'Лендинги, сайты компаний и интернет-магазины. В каждом кейсе — задача, решение, технологии и результат.')
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
{filters("Тип проекта", C.CATEGORIES, counts)}
<div class="works works--3" data-filter-grid>{grid}</div>
<p class="empty" hidden data-empty>В этой категории пока нет проектов.</p>
</div>
</section>
{cta_scene(p)}
</div>''')
    return render(p, title='Проекты студии', desc='Портфолио студии «Аксиомантик»: лендинги, многостраничные сайты и интернет-магазины — задачи, решения, технологии и результаты.',
                  body=body, active='projects', jsonld=(breadcrumbs_ld([('Главная', ''), ('Проекты', 'projects/')]),))


def case_page(i, c):
    p = Page(f'projects/{c["slug"]}/')
    nxt = C.CASES[(i + 1) % len(C.CASES)]
    meta = (f'<div class="phero__meta" data-intro="1"><span class="tag tag--dark">{e(C.CATEGORIES[c["type"]])}</span>'
            f'<span>{icon("clock")}{e(c["term"])}</span><span>{icon("layers")}{e(c["tariff"])}</span></div>')
    solution = ''.join(f'<li>{e(s)}</li>' for s in c['solution'])
    ai = ''.join(f'<li>{icon("spark")}<span>{e(s)}</span></li>' for s in c['ai'])
    tech = ''.join(f'<li>{e(t)}</li>' for t in c['tech'])
    results = ''.join(f'<div class="kpi" data-reveal><b data-count>{e(n)}</b><span>{e(t)}</span></div>' for n, t in c['results'])
    body = (page_hero(p, [('Главная', ''), ('Проекты', 'projects/'), (c['title'], '')], c['title'], c['lead'], meta)
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
<div class="case-hero t-{c["slug"]}" data-reveal>{picture(p, "case-" + c["slug"], alt="Макет сайта на ноутбуке и телефоне", sizes="(min-width: 1280px) 1200px, 100vw", eager=True)}</div>
<div class="case-grid">
<aside class="case-facts" data-reveal>
<dl>
<div><dt>Клиент</dt><dd>{e(c["client"])}</dd></div>
<div><dt>Тип проекта</dt><dd>{e(C.CATEGORIES[c["type"]])}</dd></div>
<div><dt>Срок</dt><dd>{e(c["term"])}</dd></div>
<div><dt>Тариф</dt><dd>{e(c["tariff"])}</dd></div>
</dl>
<h2 class="case-facts__h">Технологии</h2>
<ul class="chips">{tech}</ul>
<a class="btn btn--dark btn--block" href="{p.u("contacts/")}" data-order>Хочу так же {icon("arrow")}</a>
</aside>
<div class="case-body">
<section class="case-part" data-reveal><p class="eyebrow">Задача</p><p class="case-part__lead">{e(c["task"])}</p></section>
<section class="case-part" data-reveal><p class="eyebrow">Решение</p><ol class="numlist">{solution}</ol></section>
<section class="case-part case-part--ai" data-reveal><p class="eyebrow">Где помог AI</p><ul class="ailist">{ai}</ul>
<p class="case-part__note">{icon("user")}Дизайн и решения — за людьми. AI взял на себя рутину.</p></section>
<section class="case-part"><p class="eyebrow" data-reveal>Результат</p><div class="kpis">{results}</div></section>
</div>
</div>
<a class="next t-{nxt["slug"]}" href="{p.u("projects/" + nxt["slug"] + "/")}" data-reveal>
<span class="next__label">Следующий проект</span>
<span class="next__title">{e(nxt["title"])}</span>
<span class="go">{icon("arrow-ur")}</span>
</a>
</div>
</section>
{cta_scene(p)}
</div>''')
    return render(p, title=c['title'], desc=c['lead'], body=body, active='projects', og_type='article',
                  jsonld=(breadcrumbs_ld([('Главная', ''), ('Проекты', 'projects/'), (c['title'], p.path)]),))


def blog():
    p = Page('blog/')
    counts = {}
    for a in C.ARTICLES:
        counts[a['cat']] = counts.get(a['cat'], 0) + 1
    cards = ''.join(post_card(p, a, 'post--wide' if a.get('featured') else '',
                              sizes='(min-width: 1024px) 50vw, 100vw' if a.get('featured') else '(min-width: 1024px) 33vw, (min-width: 768px) 50vw, 100vw')
                    for a in C.ARTICLES)
    body = (page_hero(p, [('Главная', ''), ('Блог', 'blog/')], 'Блог',
                      'Пишем о маркетинге, дизайне и разработке — и о том, как AI меняет работу студии.')
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
{filters("Категория", C.BLOG_CATEGORIES, counts)}
<div class="posts" data-filter-grid>{cards}</div>
<p class="empty" hidden data-empty>В этой категории пока нет статей.</p>
</div>
</section>
{cta_scene(p)}
</div>''')
    return render(p, title='Блог', desc='Статьи студии «Аксиомантик» о маркетинге, дизайне, разработке сайтов и AI в дизайне.',
                  body=body, active='blog', jsonld=(breadcrumbs_ld([('Главная', ''), ('Блог', 'blog/')]),))


def article(a):
    p = Page(f'blog/{a["slug"]}/')
    blocks = []
    for kind, val in a['body']:
        if kind == 'p':
            blocks.append(f'<p>{e(val)}</p>')
        elif kind == 'h2':
            blocks.append(f'<h2>{e(val)}</h2>')
        elif kind == 'ul':
            blocks.append('<ul>' + ''.join(f'<li>{e(x)}</li>' for x in val) + '</ul>')
        elif kind == 'quote':
            blocks.append(f'<blockquote><p>{e(val)}</p></blockquote>')
    same = [x for x in C.ARTICLES if x['cat'] == a['cat'] and x is not a]
    rest = [x for x in C.ARTICLES if x['cat'] != a['cat']]
    related = (same + rest)[:3]
    meta = (f'<div class="phero__meta" data-intro="1"><span class="tag tag--dark">{e(C.BLOG_CATEGORIES[a["cat"]])}</span>'
            f'<span><time datetime="{a["date"]}">{date_ru(a["date"])}</time></span><span>{icon("clock")}{a["read"]} мин чтения</span></div>')
    body = (page_hero(p, [('Главная', ''), ('Блог', 'blog/'), (a['title'], '')], a['title'], a['desc'], meta)
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap">
<article class="article">
<div class="article__cover" data-reveal>{picture(p, "cover-" + a["cat"], sizes="(min-width: 960px) 880px, 100vw", eager=True)}</div>
<div class="prose">{"".join(blocks)}</div>
<aside class="article__cta" data-reveal>
<p><b>Нужен сайт, который работает на бизнес?</b> Расскажите о задаче — предложим формат и сроки.</p>
<a class="btn btn--dark" href="{p.u("contacts/")}" data-order>Обсудить проект {icon("arrow")}</a>
</aside>
</article>
</div>
</section>
<section class="section section--soft">
<div class="wrap">
{shead("Блог", "Читайте", tone="также", aside=f'<a class="btn btn--outline" href="{p.u("blog/")}">Все статьи {icon("arrow")}</a>')}
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
                  jsonld=(breadcrumbs_ld([('Главная', ''), ('Блог', 'blog/'), (a['title'], p.path)]), posting))


def contacts():
    p = Page('contacts/')
    uid = 'c'
    map_src = f'https://yandex.ru/map-widget/v1/?ll={S["map_ll"]}&z={S["map_z"]}&pt={S["map_ll"]},pm2blm'
    map_link = f'https://yandex.ru/maps/?ll={S["map_ll"]}&z={S["map_z"]}&pt={S["map_ll"]}'
    body = (page_hero(p, [('Главная', ''), ('Контакты', 'contacts/')], 'Контакты',
                      'Расскажите о проекте — ответим в течение рабочего дня и предложим формат, сроки и стоимость.')
            + f'''<div class="sheet">
<section class="section section--first">
<div class="wrap contacts">
<div class="card card--form" data-reveal>
<h2 class="card__title">Заявка на проект</h2>
<p class="card__sub">Заполните форму — маркетолог изучит задачу и свяжется с вами.</p>
<form class="lead" method="post" data-lead data-source="contacts" novalidate>
<input type="hidden" name="mode" value="form">
<div class="lead__form">
<div class="fields fields--2">
{field(uid, "name", "Имя", placeholder="Как к вам обращаться", autocomplete="name", maxlength=80)}
{field(uid, "contact", "Email или телефон", placeholder="name@company.ru или +7…", autocomplete="email", maxlength=120)}
</div>
<div class="fields">
<fieldset class="field"><legend class="field__label">Бюджет</legend>{picks("budget", C.BUDGETS, "unknown")}</fieldset>
{field(uid, "message", "Описание задачи", kind="textarea", placeholder="Что за проект, какие сроки, есть ли сайт сейчас", maxlength=3000)}
{HONEYPOT}
{consent(p, uid)}
</div>
<button class="btn btn--dark btn--lg lead__submit" type="submit" data-submit><span data-submit-label>Отправить заявку</span>{icon("send")}</button>
<p class="lead__status" role="status" aria-live="polite"></p>
</div>
{done_block()}
</form>
</div>
<div class="contacts__side">
<div class="card card--info" data-reveal>
<ul class="info">
<li><span class="info__ico">{icon("phone")}</span><span><small>Телефон</small><a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></span></li>
<li><span class="info__ico">{icon("mail")}</span><span><small>Email</small><a href="mailto:{e(S["email"])}">{e(S["email"])}</a></span></li>
<li><span class="info__ico">{icon("clock")}</span><span><small>Часы работы</small>{e(S["hours"])}</span></li>
<li><span class="info__ico">{icon("pin")}</span><span><small>Офис</small>{e(S["address"])}</span></li>
</ul>
<div class="info__soc"><small>Мы в соцсетях</small>{socials()}</div>
</div>
<div class="map" data-map="{e(map_src)}" data-reveal>
<div class="map__ph" aria-hidden="true"><i class="map__road map__road--1"></i><i class="map__road map__road--2"></i><i class="map__road map__road--3"></i><i class="map__river"></i><span class="map__pin">{icon("pin")}</span></div>
<div class="map__act">
<button type="button" class="btn btn--dark btn--sm" data-map-load>Показать карту</button>
<a class="map__ext" href="{e(map_link)}" target="_blank" rel="noopener">Открыть в Яндекс Картах {icon("arrow-ur")}</a>
</div>
<p class="map__note">Карта загружается с серверов Яндекса только после нажатия.</p>
</div>
</div>
</div>
</section>
</div>''')
    return render(p, title='Контакты', desc='Свяжитесь со студией «Аксиомантик»: заявка на сайт, телефон, email и соцсети. Ответим в течение рабочего дня.',
                  body=body, active='contacts', jsonld=(breadcrumbs_ld([('Главная', ''), ('Контакты', 'contacts/')]),))


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
    body = (page_hero(p, [('Главная', ''), (title, '')], title, lead)
            + f'''<div class="sheet"><section class="section section--first"><div class="wrap">
<article class="article"><div class="prose prose--legal">{"".join(blocks)}</div></article>
</div></section></div>''')
    return render(p, title=title, desc=lead, body=body)


def not_found():
    # A fixed address so nginx can serve it for any missing page; links are
    # absolute because the page answers at every depth.
    p = Page('404/', absolute=True)
    body = f'''<section class="phero phero--404">
<div class="wrap phero__in p404">
<div>
<p class="eyebrow eyebrow--dark" data-intro="1">{icon("diamond")}Ошибка 404</p>
<h1 class="h1" data-intro-split>{words("Такой страницы нет")}</h1>
<p class="phero__lead" data-intro="2">Возможно, её переместили. Начните с главной или посмотрите наши проекты.</p>
<div class="hero__cta" data-intro="3"><a class="btn btn--white btn--lg" href="/">На главную {icon("arrow")}</a><a class="link-under" href="/projects/">Проекты</a></div>
</div>
<div class="p404__art" data-intro-art><div class="hero__media">{picture(p, "hero", sizes="(min-width: 1024px) 40vw, 80vw")}</div></div>
</div>
</section>'''
    return render(p, title='Страница не найдена', desc='Страница не найдена.', body=body, index=False)


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
    css = css.split(marker)[0].rstrip('\n') + '\n\n' + marker + themes
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
    urls.append(legal_page('privacy/', 'Политика обработки персональных данных',
                           'Как студия обрабатывает и защищает данные, которые вы оставляете на сайте.', legal.PRIVACY))
    urls.append(legal_page('consent/', 'Согласие на обработку персональных данных',
                           'Текст согласия, которое вы даёте, отправляя форму на сайте.', legal.CONSENT))
    not_found()
    with open(os.path.join(OUT, 'sitemap.xml'), 'w', encoding='utf-8') as f:
        f.write('<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n')
        for u in urls:
            f.write(f'<url><loc>{e(u)}</loc></url>\n')
        f.write('</urlset>\n')
    with open(os.path.join(OUT, 'robots.txt'), 'w', encoding='utf-8') as f:
        f.write(f'User-agent: *\nDisallow: /api/\nDisallow: /404/\n\nSitemap: {S["url"]}/sitemap.xml\n')
    print(f'{len(urls)} pages in {OUT}')
    if MISSING:
        print('pictures not rendered yet:', ', '.join(sorted(MISSING)))
        if not os.environ.get('AXM_ALLOW_MISSING'):
            sys.exit(1)


if __name__ == '__main__':
    main()
