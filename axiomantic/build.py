#!/usr/bin/env python3
"""Builds the Axiomantic studio site from content.py into site/.

    python3 axiomantic/build.py

The pages are static HTML beside the hand-written assets in site/assets.
Links between pages are relative, so the folder works from any web server
and any sub-path. Open it through a server (python3 -m http.server -d
axiomantic/site), not as a file, so that "projects/" finds its index.

Nothing is loaded from other servers: the fonts are local, and the map on
the contacts page is fetched only when the visitor asks for it. The output
is deterministic — CI rebuilds it and fails if site/ differs from git.
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

S = C.SITE


def e(s):
    return html.escape(str(s), quote=True)


ICONS = {
    'arrow': '<path d="M5 12h14M13 6l6 6-6 6"/>',
    'arrow-ur': '<path d="M7 17 17 7M8.5 7H17v8.5"/>',
    'check': '<path d="m5 12.5 4.5 4.5L19 7.5"/>',
    'plus': '<path d="M12 5v14M5 12h14"/>',
    'close': '<path d="M6 6l12 12M18 6 6 18"/>',
    'menu': '<path d="M4 8.5h16M4 15.5h16"/>',
    'phone': '<path d="M6.6 3.5h2.6l1.6 4.1-2 1.3a11 11 0 0 0 6.3 6.3l1.3-2 4.1 1.6v2.6a2 2 0 0 1-2.2 2A16.5 16.5 0 0 1 4.6 5.7a2 2 0 0 1 2-2.2Z"/>',
    'chat': '<path d="M5 5.5h14a1.5 1.5 0 0 1 1.5 1.5v8.5A1.5 1.5 0 0 1 19 17h-8.5L6 20.5V17H5a1.5 1.5 0 0 1-1.5-1.5V7A1.5 1.5 0 0 1 5 5.5Z"/><path d="M8 10h8M8 13h5"/>',
    'spark': '<path d="M11 4.5 12.9 10l5.6 2-5.6 2L11 19.5 9.1 14l-5.6-2 5.6-2z"/><path d="M18.5 3v4M16.5 5h4"/>',
    'target': '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><circle cx="12" cy="12" r="1"/>',
    'pen': '<path d="M4.5 19.5 5.6 15 15.8 4.8a2 2 0 0 1 2.8 0l.6.6a2 2 0 0 1 0 2.8L9 18.4z"/><path d="m13.8 6.8 3.4 3.4"/>',
    'code': '<path d="m8.5 7.5-4.5 4.5 4.5 4.5M15.5 7.5l4.5 4.5-4.5 4.5M13.5 5l-3 14"/>',
    'search': '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/>',
    'clock': '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
    'user': '<circle cx="12" cy="8.5" r="3.5"/><path d="M5 19.5a7 7 0 0 1 14 0"/>',
    'mail': '<rect x="3.5" y="5.5" width="17" height="13" rx="2.5"/><path d="m4.5 7.5 7.5 5.5 7.5-5.5"/>',
    'pin': '<path d="M12 21s-6.5-5.6-6.5-11a6.5 6.5 0 0 1 13 0C18.5 15.4 12 21 12 21Z"/><circle cx="12" cy="10" r="2.3"/>',
    'send': '<path d="M20.5 3.5 3.5 10.6l6.6 2.6 2.6 6.6z"/><path d="m10.1 13.2 4.6-4.6"/>',
    'layers': '<path d="m12 3.5 8.5 4.5-8.5 4.5L3.5 8z"/><path d="m3.5 12 8.5 4.5 8.5-4.5M3.5 16l8.5 4.5 8.5-4.5"/>',
    'bolt': '<path d="M13 3 5 13.5h6L10 21l8-10.5h-6z"/>',
    'shield': '<path d="M12 3.5 19 6v5.5c0 4.4-3 7.7-7 9-4-1.3-7-4.6-7-9V6z"/><path d="m8.8 12 2.3 2.3 4.2-4.3"/>',
}


def icon(name, cls='ico'):
    return f'<svg class="{cls}" viewBox="0 0 24 24" aria-hidden="true" focusable="false">{ICONS[name]}</svg>'


def logo_mark(uid):
    return (f'<svg class="logo__mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">'
            f'<defs><linearGradient id="lm-{uid}" x1="0" y1="0" x2="1" y2="1">'
            f'<stop offset="0" stop-color="#4655FF"/><stop offset="1" stop-color="#8B6BFF"/></linearGradient></defs>'
            f'<rect width="32" height="32" rx="10" fill="url(#lm-{uid})"/>'
            f'<path d="M16 8.5 23.5 22.5h-15z" fill="none" stroke="#fff" stroke-width="2.2" stroke-linejoin="round"/>'
            f'<circle cx="16" cy="18.2" r="2.2" fill="#fff"/></svg>')


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


# ---------------------------------------------------------------- frame

def header(p, active):
    links = ''.join(
        f'<a href="{p.u(href)}"{" aria-current=page" if key == active else ""}>{e(label)}</a>'
        for key, label, href in C.NAV)
    return f'''<header class="hdr" data-hdr>
<div class="container hdr__in">
<a class="logo" href="{p.u()}" aria-label="{e(S["name"])} — на главную">{logo_mark("h")}<span>{e(S["name"])}</span></a>
<nav class="nav" aria-label="Основное меню">{links}</nav>
<div class="hdr__act">
<a class="btn btn--primary btn--sm" href="{p.u("contacts/")}" data-order>Заказать</a>
<button class="burger" type="button" aria-expanded="false" aria-controls="menu" data-burger><span></span><span></span><span class="sr">Меню</span></button>
</div>
</div>
<div class="menu" id="menu" hidden>
<nav class="menu__nav" aria-label="Мобильное меню">{links}</nav>
<div class="menu__foot">
<a class="btn btn--primary btn--lg btn--block" href="{p.u("contacts/")}" data-order>Заказать проект</a>
<a class="menu__contact" href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a>
<a class="menu__contact" href="mailto:{e(S["email"])}">{e(S["email"])}</a>
</div>
</div>
</header>'''


def socials(cls='soc'):
    return f'<ul class="{cls}">' + ''.join(
        f'<li><a href="{e(href)}" aria-label="{e(name)}" title="{e(name)}">{e(short)}</a></li>'
        for name, short, href in S['socials']) + '</ul>'


def footer(p):
    nav = ''.join(f'<li><a href="{p.u(href)}">{e(label)}</a></li>' for _, label, href in C.NAV)
    nav += f'<li><a href="{p.u("#tariffs")}">Тарифы</a></li><li><a href="{p.u("#faq")}">Вопросы и ответы</a></li>'
    services = ''.join(
        f'<li><a href="{p.u("#tariffs")}">{e(t["name"])}{" · " + e(t["sub"]) if t["id"] != "landing" else ""}</a></li>'
        for t in C.TARIFFS)
    return f'''<footer class="ftr">
<div class="container">
<div class="ftr__top">
<div class="ftr__brand">
<a class="logo logo--light" href="{p.u()}">{logo_mark("f")}<span>{e(S["name"])}</span></a>
<p>Студия цифровых продуктов. Маркетолог, разработчик и UX/UI-дизайнер в связке с AI — быстро, качественно и технологично.</p>
<a class="btn btn--light" href="{p.u("contacts/")}" data-order>Заказать проект {icon("arrow")}</a>
</div>
<div class="ftr__col"><h2 class="ftr__h">Разделы</h2><ul>{nav}</ul></div>
<div class="ftr__col"><h2 class="ftr__h">Услуги</h2><ul>{services}</ul></div>
<div class="ftr__col"><h2 class="ftr__h">Контакты</h2><ul>
<li><a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></li>
<li><a href="mailto:{e(S["email"])}">{e(S["email"])}</a></li>
<li class="ftr__muted">{e(S["hours"])}</li>
</ul>{socials("soc soc--dark")}</div>
</div>
<p class="ftr__word" aria-hidden="true">{e(S["name"])}</p>
<div class="ftr__bottom">
<span>© {S["year"]} {e(S["name"])}</span>
<a href="{p.u("privacy/")}">Политика обработки персональных данных</a>
<a href="{p.u("consent/")}">Согласие на обработку данных</a>
</div>
</div>
</footer>'''


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
<button class="btn btn--primary btn--lg btn--block lead__submit" type="submit" data-submit>
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


def render(p, *, title, desc, body, active=None, jsonld=(), og_type='website', index=True):
    url = S['url'] + '/' + p.path
    full_title = title if title.startswith(S['name']) else f'{title} — {S["name"]}'
    robots = '' if index else '<meta name="robots" content="noindex">\n'
    endpoint = e(S['endpoint'])
    doc = f'''<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
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
<meta name="twitter:card" content="summary_large_image">
<meta name="theme-color" content="#080B2E">
<meta name="referrer" content="strict-origin-when-cross-origin">
<link rel="icon" href="{p.u("assets/favicon.svg")}" type="image/svg+xml">
<link rel="preload" href="{p.u("assets/fonts/unbounded-cyrillic-wght-normal.woff2")}" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="{p.u("assets/fonts/manrope-cyrillic-wght-normal.woff2")}" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="{p.u("assets/css/main.css")}?v={VERSIONS["css"]}">
<link rel="stylesheet" href="{p.u("assets/css/themes.css")}?v={VERSIONS["css"]}">
<script src="{p.u("assets/js/main.js")}?v={VERSIONS["js"]}" defer></script>
{"".join(ld(j) for j in jsonld)}
</head>
<body data-endpoint="{endpoint}">
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


# -------------------------------------------------------------- pieces

def shead(eyebrow, title_html, lead='', aside='', cls=''):
    lead_html = f'<p class="shead__lead">{e(lead)}</p>' if lead else ''
    aside_html = f'<div class="shead__aside">{aside}</div>' if aside else ''
    return (f'<header class="shead {cls}"><div class="shead__main"><p class="eyebrow">{e(eyebrow)}</p>'
            f'<h2 class="h2">{title_html}</h2>{lead_html}</div>{aside_html}</header>')


def ui(kind, mobile=False):
    """A miniature web page drawn with boxes, for device mockups."""
    if mobile:
        body = {
            'landing': '<i class="ui__img"></i><i class="ui__t w90"></i><i class="ui__t w60"></i><i class="ui__s w80"></i><b class="ui__btn"></b><i class="ui__card"></i>',
            'multi': '<i class="ui__ban"><i class="ui__t w70"></i><i class="ui__t w50"></i></i><i class="ui__s w90"></i><i class="ui__s w70"></i><i class="ui__card"></i><i class="ui__card"></i>',
            'ecom': '<i class="ui__img"></i><i class="ui__t w70"></i><i class="ui__price"></i><b class="ui__btn"></b><span class="ui__grid ui__grid--2"><i></i><i></i></span>',
        }[kind]
        return f'<span class="ui ui--m"><span class="ui__bar"><i></i><i></i></span>{body}</span>'
    nav = '<span class="ui__nav"><i class="ui__logo"></i><i></i><i></i><i></i><b class="ui__btn"></b></span>'
    body = {
        'landing': ('<span class="ui__hero"><span class="ui__txt"><i class="ui__t w90"></i><i class="ui__t w70"></i>'
                    '<i class="ui__s w80"></i><i class="ui__s w60"></i><b class="ui__btn"></b></span><i class="ui__img"></i></span>'
                    '<span class="ui__cards"><i></i><i></i><i></i></span>'),
        'multi': ('<i class="ui__ban"><i class="ui__t w50"></i><i class="ui__t w35"></i><b class="ui__btn"></b></i>'
                  '<span class="ui__cards ui__cards--4"><i></i><i></i><i></i><i></i></span><span class="ui__rows"><i class="ui__s w90"></i><i class="ui__s w70"></i></span>'),
        'ecom': ('<span class="ui__shop"><span class="ui__side"><i class="ui__s w90"></i><i class="ui__s w70"></i><i class="ui__s w80"></i><i class="ui__s w60"></i></span>'
                 '<span class="ui__grid"><i></i><i></i><i></i><i></i><i></i><i></i></span></span>'),
    }[kind]
    return f'<span class="ui">{nav}{body}</span>'


def mockup(kind, cls=''):
    return (f'<div class="mock {cls}" aria-hidden="true"><div class="mock__stage">'
            f'<span class="mock__floor"></span>'
            f'<span class="dev dev--desk"><span class="dev__scr">{ui(kind)}</span></span>'
            f'<span class="dev dev--phone"><span class="dev__scr">{ui(kind, mobile=True)}</span></span>'
            f'</div></div>')


def pages_art(n, extra=''):
    sheets = ''.join(f'<span class="pg pg--{i}"><span class="dev__scr">{ui("multi" if i else "landing")}</span></span>'
                     for i in range(n))
    return (f'<div class="mock mock--pages mock--p{n}" aria-hidden="true"><div class="mock__stage">'
            f'<span class="mock__floor"></span>{sheets}{extra}</div></div>')


def case_card(p, c, cls=''):
    result_n, result_t = c['results'][0]
    return f'''<a class="case t-{c["slug"]} {cls}" href="{p.u("projects/" + c["slug"] + "/")}" data-cat="{c["type"]}">
<div class="case__art">{mockup(c["type"])}</div>
<div class="case__body">
<div class="case__meta"><span class="tag">{e(C.CATEGORIES[c["type"]])}</span><span>{e(c["term"])}</span></div>
<h3 class="case__title">{e(c["title"])}</h3>
<p class="case__res"><b>{e(result_n)}</b> {e(result_t)}</p>
</div>
<span class="case__go">{icon("arrow-ur")}</span>
</a>'''


def cover(a, big=False):
    shapes = {
        'marketing': '<i class="cv__ring cv__ring--1"></i><i class="cv__ring cv__ring--2"></i><i class="cv__ring cv__ring--3"></i><i class="cv__orb"></i>',
        'design': '<i class="cv__sq cv__sq--1"></i><i class="cv__sq cv__sq--2"></i><svg class="cv__curve" viewBox="0 0 200 120"><path d="M10 100C60 100 60 20 110 20S170 70 190 30"/><circle cx="10" cy="100" r="4"/><circle cx="110" cy="20" r="4"/><circle cx="190" cy="30" r="4"/></svg>',
        'dev': '<span class="cv__code"><i class="w60"></i><i class="w80 in"></i><i class="w50 in"></i><i class="w70 in2"></i><i class="w40 in"></i><i class="w30"></i></span>',
        'ai': '<i class="cv__orb cv__orb--ai"></i><i class="cv__ring cv__ring--ai"></i><i class="cv__star"></i>',
    }[a['cat']]
    return f'<div class="cover cover--{a["cat"]}{" cover--big" if big else ""}" aria-hidden="true">{shapes}</div>'


def post_card(p, a, cls=''):
    return f'''<a class="post {cls}" href="{p.u("blog/" + a["slug"] + "/")}" data-cat="{a["cat"]}">
{cover(a)}
<div class="post__body">
<div class="post__meta"><span class="tag tag--soft">{e(C.BLOG_CATEGORIES[a["cat"]])}</span><time datetime="{a["date"]}">{date_ru(a["date"])}</time></div>
<h3 class="post__title">{e(a["title"])}</h3>
<p class="post__desc">{e(a["desc"])}</p>
<span class="post__read">{icon("clock")}{a["read"]} мин чтения</span>
</div>
</a>'''


def cta_band(p):
    return f'''<section class="section section--tight">
<div class="container">
<div class="cta reveal">
<div class="cta__glow" aria-hidden="true"></div>
<div class="cta__orb" aria-hidden="true"></div>
<div class="cta__text">
<p class="eyebrow eyebrow--dark">Новый проект</p>
<h2 class="h2 h2--light">Обсудим ваш сайт?</h2>
<p class="cta__lead">Расскажите о задаче — предложим формат, сроки и стоимость в течение рабочего дня. Базовое SEO уже в цене.</p>
</div>
<div class="cta__act">
<a class="btn btn--light btn--lg" href="{p.u("contacts/")}" data-order data-mode="call">{icon("phone")}Позвонить</a>
<a class="btn btn--glass btn--lg" href="{p.u("contacts/")}" data-order data-mode="write">{icon("chat")}Написать</a>
</div>
</div>
</div>
</section>'''


def page_hero(p, crumbs, title, lead='', meta=''):
    trail = ''.join(f'<a href="{p.u(href)}">{e(name)}</a><span aria-hidden="true">/</span>' for name, href in crumbs[:-1])
    trail += f'<span aria-current="page">{e(crumbs[-1][0])}</span>'
    lead_html = f'<p class="phero__lead">{e(lead)}</p>' if lead else ''
    return f'''<section class="phero">
<div class="phero__glow" aria-hidden="true"></div>
<div class="phero__orb" aria-hidden="true"></div>
<div class="container phero__in">
<nav class="crumbs" aria-label="Навигационная цепочка">{trail}</nav>
{meta}
<h1 class="h1">{e(title)}</h1>
{lead_html}
</div>
</section>'''


def filters(label, options, total_counts):
    btns = [f'<button type="button" class="filter" data-filter="all" aria-pressed="true">Все<sup>{sum(total_counts.values())}</sup></button>']
    for key, name in options.items():
        btns.append(f'<button type="button" class="filter" data-filter="{key}" aria-pressed="false">{e(name)}<sup>{total_counts.get(key, 0)}</sup></button>')
    return f'<div class="filters" role="group" aria-label="{e(label)}" data-filters>{"".join(btns)}</div>'


# --------------------------------------------------------------- pages

def home():
    p = Page('')
    trust = ''.join(f'<li>{icon("check")}{e(t)}</li>' for t in C.TRUST)
    hero = f'''<section class="hero">
<div class="hero__glow" aria-hidden="true"></div>
<div class="container hero__in">
<div class="hero__text">
<p class="eyebrow eyebrow--dark hero__eyebrow">Студия цифровых продуктов</p>
<h1 class="display"><span class="tone">Создаём сайты</span> быстро, качественно и технологично</h1>
<p class="hero__lead">Маркетолог, разработчик и UX/UI-дизайнер работают в связке с AI: нейросети берут на себя рутину, а люди — стратегию, дизайн и качество.</p>
<div class="hero__cta">
<a class="btn btn--primary btn--lg" href="{p.u("contacts/")}" data-order>Обсудить проект {icon("arrow")}</a>
<a class="btn btn--glass btn--lg" href="{p.u("projects/")}">Смотреть проекты</a>
</div>
<ul class="hero__trust">{trust}</ul>
</div>
<div class="hero__art" aria-hidden="true">
<div class="art">
<svg class="art__ring art__ring--back" viewBox="0 0 560 560"><defs><linearGradient id="rg1" x1="0" x2="1"><stop offset="0" stop-color="#fff" stop-opacity="0"/><stop offset=".5" stop-color="#C9CCFF" stop-opacity=".7"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></linearGradient></defs><path d="M20 300a260 86 0 0 1 520 0" fill="none" stroke="url(#rg1)" stroke-width="1.5" transform="rotate(-16 280 280)"/></svg>
<div class="orb"><span class="orb__core"></span><span class="orb__shine"></span></div>
<svg class="art__ring art__ring--front" viewBox="0 0 560 560"><defs><linearGradient id="rg2" x1="0" x2="1"><stop offset="0" stop-color="#8F9BFF" stop-opacity=".1"/><stop offset=".5" stop-color="#fff" stop-opacity=".95"/><stop offset="1" stop-color="#8F9BFF" stop-opacity=".1"/></linearGradient></defs><g transform="rotate(-16 280 280)"><path d="M540 300a260 86 0 0 1-520 0" fill="none" stroke="url(#rg2)" stroke-width="2"/><circle class="art__sat" cx="402" cy="376" r="9"/></g></svg>
<span class="moon"></span>
<span class="cube cube--a"><i></i><i></i><i></i><i></i><i></i><i></i></span>
<span class="cube cube--b"><i></i><i></i><i></i><i></i><i></i><i></i></span>
<div class="glass glass--design"><span class="glass__ico">{icon("pen")}</span><span class="glass__txt"><b>Дизайн</b><small>рисует человек</small></span><span class="glass__ok">{icon("check")}</span></div>
<div class="glass glass--ai"><span class="glass__row"><b>AI-ускорение</b><em>−40%</em></span><span class="bars"><i></i><i></i><i></i><i></i><i></i><i></i><i></i></span><small>времени на рутину</small></div>
<div class="glass glass--seo"><span class="glass__ico glass__ico--sm">{icon("search")}</span><b>SEO включено</b></div>
</div>
</div>
</div>
<div class="container hero__foot">
<span>Маркетолог + Разработчик + UX/UI + AI</span>
<a class="hero__scroll" href="#approach">Наш подход {icon("arrow")}</a>
</div>
</section>'''

    roles = []
    for i, (ic, name, text) in enumerate(C.ROLES):
        op = '<span class="op" aria-hidden="true">+</span>' if i else ''
        roles.append(f'<li class="role{" role--ai" if ic == "spark" else ""} reveal">{op}<span class="role__ico">{icon(ic)}</span>'
                     f'<h3 class="role__name">{e(name)}</h3><p class="role__text">{e(text)}</p></li>')
    roles.append(f'<li class="role role--result reveal"><span class="op op--eq" aria-hidden="true">=</span><span class="role__orb" aria-hidden="true"></span>'
                 f'<h3 class="role__name">Сайт, который продаёт</h3><p class="role__text">Быстро · качественно · технологично</p></li>')
    steps = ''.join(f'''<li class="step reveal">
<span class="step__n">{i + 1:02d}</span>
<div class="step__main"><h3 class="step__title">{e(title)}</h3><p class="step__text">{e(text)}</p>
<span class="tag tag--human">{icon("user")}{e(who)}</span></div>
<div class="step__ai"><span class="step__ai-ico">{icon("spark")}</span><p><b>AI ускоряет:</b> {e(ai)}</p></div>
<span class="step__time">{icon("clock")}{e(days)}</span>
</li>''' for i, (title, who, text, ai, days) in enumerate(C.STEPS))
    approach = f'''<section class="section" id="approach">
<div class="container">
{shead("Наш подход", 'AI ускоряет рутину — <span class="tone">люди делают продукт</span>',
       "Над проектом работают маркетолог, UX/UI-дизайнер и разработчик. AI встроен в каждый этап и забирает повторяющуюся работу — поэтому мы запускаем быстрее и не жертвуем качеством.")}
<ol class="formula" aria-label="Состав команды">{"".join(roles)}</ol>
<ol class="steps">{steps}</ol>
<p class="steps__sum reveal">{icon("bolt")}<span><b>10 рабочих дней</b> — путь лендинга от брифа до запуска</span></p>
</div>
</section>'''

    svc_art = {
        'landing': pages_art(1),
        'multi-base': pages_art(3),
        'multi-custom': pages_art(3, '<span class="node node--1"></span><span class="node node--2"></span><span class="node node--3"></span>'),
    }
    services = ''.join(f'''<article class="svc reveal">
<div class="svc__art svc__art--{sid}">{svc_art[sid]}</div>
<div class="svc__body">
<span class="badge badge--seo">{icon("search")}Базовое SEO включено</span>
<h3 class="svc__title">{e(name)}</h3>
<p class="svc__text">{e(text)}</p>
<ul class="chips">{"".join(f"<li>{e(t)}</li>" for t in tags)}</ul>
<a class="link-arrow" href="#tariffs">Тариф и стоимость {icon("arrow")}</a>
</div>
</article>''' for sid, name, text, tags in C.SERVICES)
    services_sec = f'''<section class="section section--soft" id="services">
<div class="container">
{shead("Услуги", 'Что мы делаем', "Три формата под разные задачи — от продающей страницы до сложного продукта с интеграциями.",
       aside=f'<p class="seo-note">{icon("search")}<span>В каждый проект входит <b>базовое SEO</b>: структура, мета-теги, микроразметка и скорость</span></p>')}
<div class="services">{services}</div>
</div>
</section>'''

    seo_list = ''.join(f'<li>{icon("check")}{e(s)}</li>' for s in C.SEO_ITEMS)
    why = f'''<section class="section" id="why">
<div class="container">
{shead("Почему мы", 'Быстро и качественно — <span class="tone">потому что дизайн делает человек, а рутину берёт AI</span>')}
<div class="bento">
<article class="bc bc--human reveal">
<div class="bc__art" aria-hidden="true">
<svg class="draft" viewBox="0 0 400 260">
<rect class="draft__frame" x="20" y="20" width="360" height="220" rx="18"/>
<rect class="draft__block" x="44" y="48" width="150" height="14" rx="7"/>
<rect class="draft__block draft__block--soft" x="44" y="72" width="110" height="10" rx="5"/>
<rect class="draft__btn" x="44" y="96" width="70" height="22" rx="11"/>
<rect class="draft__img" x="222" y="44" width="134" height="92" rx="14"/>
<rect class="draft__card" x="44" y="156" width="96" height="60" rx="12"/>
<rect class="draft__card" x="152" y="156" width="96" height="60" rx="12"/>
<rect class="draft__card" x="260" y="156" width="96" height="60" rx="12"/>
<path class="draft__pen" d="M60 230C120 120 210 250 300 110S370 60 380 40"/>
<circle class="draft__pt" cx="60" cy="230" r="5"/><circle class="draft__pt" cx="300" cy="110" r="5"/><circle class="draft__pt" cx="380" cy="40" r="5"/>
<path class="draft__handle" d="M250 160 350 60"/>
</svg>
<span class="cursor"><svg viewBox="0 0 24 24"><path d="M5 3l14 8-6 1.5L10 19z"/></svg><b>UI-дизайнер</b></span>
</div>
<h3 class="bc__title">Дизайн рисует человек, а не нейросеть</h3>
<p class="bc__text">AI не генерирует макеты. Каждый экран проектирует UX/UI-дизайнер под вашу задачу и аудиторию — поэтому сайт не похож на шаблон и ведёт к заявке.</p>
</article>
<article class="bc bc--fast reveal">
<span class="bc__ico">{icon("bolt")}</span>
<h3 class="bc__title">Быстро</h3>
<p class="bc__text">AI снимает рутину на каждом этапе — команда тратит время на решения.</p>
<div class="race">
<div class="race__row"><span>Обычный процесс</span><span class="race__bar race__bar--slow"><i></i></span><b>6–8 нед.</b></div>
<div class="race__row race__row--us"><span>Аксиомантик</span><span class="race__bar"><i></i></span><b>2–4 нед.</b></div>
</div>
</article>
<article class="bc bc--quality reveal">
<span class="bc__ico">{icon("shield")}</span>
<h3 class="bc__title">Качество под контролем</h3>
<ul class="ticks ticks--sm"><li>{icon("check")}Ревью дизайна и кода людьми</li><li>{icon("check")}Автотесты форм и вёрстки</li><li>{icon("check")}Проверка на 12 разрешениях</li></ul>
</article>
<article class="bc bc--seo reveal">
<div class="gauge" aria-hidden="true"><svg viewBox="0 0 120 120"><circle class="gauge__bg" cx="60" cy="60" r="50"/><circle class="gauge__val" cx="60" cy="60" r="50"/></svg><b>98</b><small>скорость</small></div>
<h3 class="bc__title">SEO уже в цене</h3>
<ul class="ticks ticks--sm">{seo_list}</ul>
</article>
<article class="bc bc--team reveal">
<div class="avatars" aria-hidden="true"><span>М</span><span>Д</span><span>Р</span><span class="avatars__ai">{icon("spark")}</span></div>
<h3 class="bc__title">Одно окно</h3>
<p class="bc__text">Маркетолог, дизайнер, разработчик и менеджер проекта — в одном чате с вами. Без испорченного телефона.</p>
</article>
</div>
</div>
</section>'''

    stats = ''.join(f'<div class="stat reveal"><b class="stat__n">{e(n)}</b><span class="stat__l">{e(l)}</span><span class="stat__s">{e(s)}</span></div>'
                    for n, l, s in C.STATS)
    space = f'''<section class="section section--tight">
<div class="container">
<div class="space">
<div class="space__sky" aria-hidden="true"><i class="space__beam"></i><i class="space__planet"></i><i class="space__horizon"></i></div>
<h2 class="space__title"><span class="tone">Каждый проект —</span> синергия стратегии, дизайна и технологий</h2>
<div class="space__stats">{stats}</div>
</div>
</div>
</section>'''

    featured = [c for c in C.CASES if c.get('featured')][:4]
    works = f'''<section class="section" id="works">
<div class="container">
{shead("Портфолио", 'Избранные проекты', aside=f'<a class="btn btn--ghost" href="{p.u("projects/")}">Все проекты {icon("arrow")}</a>')}
<div class="works">{"".join(case_card(p, c, "reveal") for c in featured)}</div>
</div>
</section>'''

    tariffs = []
    for t in C.TARIFFS:
        pop = t.get('popular')
        feats = ''.join(f'<li>{icon("check")}{e(f)}</li>' for f in t['features'])
        badge = '<span class="badge badge--pop">Популярный</span>' if pop else ''
        tariffs.append(f'''<article class="tariff{" tariff--pop" if pop else ""} reveal">
<div class="tariff__head"><h3 class="tariff__name">{e(t["name"])}<span>{e(t["sub"])}</span></h3>{badge}</div>
<p class="tariff__price{"" if t["price"][0].isdigit() else " tariff__price--text"}"><b>{e(t["price"])}</b><span>{e(t["price_note"])}</span></p>
<p class="tariff__term">{icon("clock")}{e(t["term"])}</p>
<ul class="ticks">{feats}</ul>
<a class="btn {"btn--light" if pop else "btn--primary"} btn--lg btn--block" href="{p.u("contacts/")}" data-order data-interest="{t["id"]}">Заказать {icon("arrow")}</a>
</article>''')
    tariffs_sec = f'''<section class="section section--soft" id="tariffs">
<div class="container">
{shead("Тарифы", 'Прозрачные цены — <span class="tone">команда и SEO уже внутри</span>')}
<div class="tariffs">{"".join(tariffs)}</div>
<p class="note reveal"><span class="note__ico">{icon("spark")}</span><span>{e(C.TARIFF_NOTE)}</span></p>
</div>
</section>'''

    reviews = ''.join(f'''<figure class="review reveal">
<blockquote class="review__text">«{e(text)}»</blockquote>
<figcaption class="review__who"><span class="avatar">{e(name[0])}</span><span><b>{e(name)}</b><small>{e(role)}</small></span></figcaption>
<a class="link-arrow" href="{p.u("projects/" + slug + "/")}">Смотреть кейс {icon("arrow")}</a>
</figure>''' for name, role, slug, text in C.REVIEWS)
    reviews_sec = f'''<section class="section" id="reviews">
<div class="container">
{shead("Отзывы", 'Что говорят <span class="tone">клиенты</span>')}
<div class="reviews">{reviews}</div>
</div>
</section>'''

    qa = ''.join(f'<details class="qa"><summary class="qa__q"><span>{e(q)}</span><span class="qa__ico">{icon("plus")}</span></summary>'
                 f'<div class="qa__a"><p>{e(a)}</p></div></details>' for q, a in C.FAQ)
    faq = f'''<section class="section section--soft" id="faq">
<div class="container faq">
<div class="faq__head">
<p class="eyebrow">Вопросы и ответы</p>
<h2 class="h2">Частые вопросы</h2>
<p class="shead__lead">Не нашли ответ? Напишите — ответим в течение рабочего дня.</p>
<a class="btn btn--ghost" href="{p.u("contacts/")}" data-order data-mode="write">{icon("chat")}Задать вопрос</a>
</div>
<div class="faq__list">{qa}</div>
</div>
</section>'''

    body = (hero + '<div class="sheet">' + approach + services_sec + why + space + works
            + tariffs_sec + reviews_sec + faq + cta_band(p) + '</div>')
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
    return render(p, title=f'{S["name"]} — создание сайтов быстро, качественно и технологично',
                  desc='Студия «Аксиомантик»: лендинги и многостраничные сайты с индивидуальным дизайном от человека. Маркетолог, разработчик, UX/UI-дизайнер и AI в одной команде. Базовое SEO включено в стоимость.',
                  body=body, active='home', jsonld=(org, faq_ld, offers))


def projects():
    p = Page('projects/')
    counts = {}
    for c in C.CASES:
        counts[c['type']] = counts.get(c['type'], 0) + 1
    grid = ''.join(case_card(p, c) for c in C.CASES)
    body = (page_hero(p, [('Главная', ''), ('Проекты', 'projects/')], 'Проекты',
                      'Лендинги, сайты компаний и интернет-магазины. В каждом кейсе — задача, решение, технологии и результат.')
            + f'''<div class="sheet">
<section class="section section--first">
<div class="container">
{filters("Тип проекта", C.CATEGORIES, counts)}
<div class="works works--3" data-filter-grid>{grid}</div>
<p class="empty" hidden data-empty>В этой категории пока нет проектов.</p>
</div>
</section>
{cta_band(p)}
</div>''')
    return render(p, title='Проекты студии', desc='Портфолио студии «Аксиомантик»: лендинги, многостраничные сайты и интернет-магазины — задачи, решения, технологии и результаты.',
                  body=body, active='projects', jsonld=(breadcrumbs_ld([('Главная', ''), ('Проекты', 'projects/')]),))


def case_page(i, c):
    p = Page(f'projects/{c["slug"]}/')
    nxt = C.CASES[(i + 1) % len(C.CASES)]
    meta = (f'<div class="phero__meta"><span class="tag tag--dark">{e(C.CATEGORIES[c["type"]])}</span>'
            f'<span>{icon("clock")}{e(c["term"])}</span><span>{icon("layers")}{e(c["tariff"])}</span></div>')
    solution = ''.join(f'<li>{e(s)}</li>' for s in c['solution'])
    ai = ''.join(f'<li>{icon("spark")}<span>{e(s)}</span></li>' for s in c['ai'])
    tech = ''.join(f'<li>{e(t)}</li>' for t in c['tech'])
    results = ''.join(f'<div class="kpi"><b>{e(n)}</b><span>{e(t)}</span></div>' for n, t in c['results'])
    body = (page_hero(p, [('Главная', ''), ('Проекты', 'projects/'), (c['title'], '')], c['title'], c['lead'], meta)
            + f'''<div class="sheet">
<section class="section section--first">
<div class="container">
<div class="case-hero t-{c["slug"]}">{mockup(c["type"], "mock--big")}</div>
<div class="case-grid">
<aside class="case-facts">
<dl>
<div><dt>Клиент</dt><dd>{e(c["client"])}</dd></div>
<div><dt>Тип проекта</dt><dd>{e(C.CATEGORIES[c["type"]])}</dd></div>
<div><dt>Срок</dt><dd>{e(c["term"])}</dd></div>
<div><dt>Тариф</dt><dd>{e(c["tariff"])}</dd></div>
</dl>
<h2 class="case-facts__h">Технологии</h2>
<ul class="chips">{tech}</ul>
<a class="btn btn--primary btn--block" href="{p.u("contacts/")}" data-order>Хочу так же {icon("arrow")}</a>
</aside>
<div class="case-body">
<section class="case-part"><p class="eyebrow">Задача</p><p class="case-part__lead">{e(c["task"])}</p></section>
<section class="case-part"><p class="eyebrow">Решение</p><ol class="numlist">{solution}</ol></section>
<section class="case-part case-part--ai"><p class="eyebrow">Где помог AI</p><ul class="ailist">{ai}</ul>
<p class="case-part__note">{icon("pen")}Дизайн и решения — за людьми. AI взял на себя рутину.</p></section>
<section class="case-part"><p class="eyebrow">Результат</p><div class="kpis">{results}</div></section>
</div>
</div>
<a class="next t-{nxt["slug"]}" href="{p.u("projects/" + nxt["slug"] + "/")}">
<span class="next__label">Следующий проект</span>
<span class="next__title">{e(nxt["title"])}</span>
<span class="case__go">{icon("arrow-ur")}</span>
</a>
</div>
</section>
{cta_band(p)}
</div>''')
    return render(p, title=c['title'], desc=c['lead'], body=body, active='projects', og_type='article',
                  jsonld=(breadcrumbs_ld([('Главная', ''), ('Проекты', 'projects/'), (c['title'], p.path)]),))


def blog():
    p = Page('blog/')
    counts = {}
    for a in C.ARTICLES:
        counts[a['cat']] = counts.get(a['cat'], 0) + 1
    cards = ''.join(post_card(p, a, 'post--wide' if a.get('featured') else '') for a in C.ARTICLES)
    body = (page_hero(p, [('Главная', ''), ('Блог', 'blog/')], 'Блог',
                      'Пишем о маркетинге, дизайне и разработке — и о том, как AI меняет работу студии.')
            + f'''<div class="sheet">
<section class="section section--first">
<div class="container">
{filters("Категория", C.BLOG_CATEGORIES, counts)}
<div class="posts" data-filter-grid>{cards}</div>
<p class="empty" hidden data-empty>В этой категории пока нет статей.</p>
</div>
</section>
{cta_band(p)}
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
    meta = (f'<div class="phero__meta"><span class="tag tag--dark">{e(C.BLOG_CATEGORIES[a["cat"]])}</span>'
            f'<span><time datetime="{a["date"]}">{date_ru(a["date"])}</time></span><span>{icon("clock")}{a["read"]} мин чтения</span></div>')
    body = (page_hero(p, [('Главная', ''), ('Блог', 'blog/'), (a['title'], '')], a['title'], a['desc'], meta)
            + f'''<div class="sheet">
<section class="section section--first">
<div class="container">
<article class="article">
{cover(a, big=True)}
<div class="prose">{"".join(blocks)}</div>
<aside class="article__cta">
<p><b>Нужен сайт, который работает на бизнес?</b> Расскажите о задаче — предложим формат и сроки.</p>
<a class="btn btn--primary" href="{p.u("contacts/")}" data-order>Обсудить проект {icon("arrow")}</a>
</aside>
</article>
</div>
</section>
<section class="section section--soft">
<div class="container">
{shead("Блог", "Читайте также", aside=f'<a class="btn btn--ghost" href="{p.u("blog/")}">Все статьи {icon("arrow")}</a>')}
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
<div class="container contacts">
<div class="card card--form">
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
<button class="btn btn--primary btn--lg lead__submit" type="submit" data-submit><span data-submit-label>Отправить заявку</span>{icon("send")}</button>
<p class="lead__status" role="status" aria-live="polite"></p>
</div>
{done_block()}
</form>
</div>
<div class="contacts__side">
<div class="card card--info">
<ul class="info">
<li><span class="info__ico">{icon("phone")}</span><span><small>Телефон</small><a href="tel:{e(S["phone_href"])}">{e(S["phone"])}</a></span></li>
<li><span class="info__ico">{icon("mail")}</span><span><small>Email</small><a href="mailto:{e(S["email"])}">{e(S["email"])}</a></span></li>
<li><span class="info__ico">{icon("clock")}</span><span><small>Часы работы</small>{e(S["hours"])}</span></li>
<li><span class="info__ico">{icon("pin")}</span><span><small>Офис</small>{e(S["address"])}</span></li>
</ul>
<div class="info__soc"><small>Мы в соцсетях</small>{socials()}</div>
</div>
<div class="map" data-map="{e(map_src)}">
<div class="map__ph" aria-hidden="true"><i class="map__road map__road--1"></i><i class="map__road map__road--2"></i><i class="map__road map__road--3"></i><i class="map__river"></i><span class="map__pin">{icon("pin")}</span></div>
<div class="map__act">
<button type="button" class="btn btn--light btn--sm" data-map-load>Показать карту</button>
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


def legal(path, title, lead, sections):
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
            + f'''<div class="sheet"><section class="section section--first"><div class="container">
<article class="article"><div class="prose prose--legal">{"".join(blocks)}</div></article>
</div></section></div>''')
    return render(p, title=title, desc=lead, body=body)


OPERATOR = '[Наименование оператора: ИП ФИО или ООО «…», ИНН, ОГРН/ОГРНИП, адрес]'

PRIVACY = [
    ('1. Общие положения', [
        f'Политика определяет порядок обработки персональных данных посетителей сайта {S["url"]} и меры по их защите. Оператор — {OPERATOR}, email для обращений: {S["email"]}.',
        'Политика составлена в соответствии с Федеральным законом от 27.07.2006 № 152-ФЗ «О персональных данных».',
    ]),
    ('2. Какие данные мы обрабатываем', [
        ['Имя', 'Номер телефона', 'Адрес электронной почты или имя пользователя в мессенджере', 'Текст обращения и сведения о проекте, которые вы сообщаете сами', 'IP-адрес — только для защиты формы от злоупотреблений'],
        'Сайт не использует cookie для аналитики и рекламы. Если сервисы аналитики будут подключены, политика будет дополнена до их включения.',
    ]),
    ('3. Цели обработки', [
        ['Ответить на заявку и связаться с вами выбранным способом', 'Подготовить предложение и заключить договор', 'Исполнить договор'],
    ]),
    ('4. Правовые основания', [
        'Согласие субъекта персональных данных (п. 1 ч. 1 ст. 6 Закона № 152-ФЗ), а при заключении и исполнении договора — п. 5 ч. 1 ст. 6 Закона № 152-ФЗ.',
    ]),
    ('5. Как мы обрабатываем данные', [
        'Заявки записываются в базу данных на сервере, расположенном на территории Российской Федерации. Доступ к ним есть только у сотрудников, которые отвечают на заявки.',
        '[Если уведомления о заявках пересылаются в Telegram или другой зарубежный сервис — указать это здесь как трансграничную передачу и уведомить Роскомнадзор до её начала.]',
        'Данные хранятся до достижения целей обработки, но не дольше [срок] после последнего обращения, если договор не заключён, и затем удаляются.',
    ]),
    ('6. Ваши права', [
        'Вы можете запросить сведения об обработке ваших данных, потребовать их уточнения, блокирования или удаления, а также отозвать согласие, написав на ' + S['email'] + '. Ответим в срок, установленный законом.',
    ]),
    ('7. Защита данных', [
        'Данные передаются по защищённому соединению (HTTPS). Хранилище заявок закрыто от публичного доступа, ключи и пароли хранятся только на сервере.',
    ]),
]

CONSENT = [
    ('Согласие', [
        f'Отправляя форму на сайте {S["url"]}, я свободно, своей волей и в своём интересе даю согласие {OPERATOR} на обработку моих персональных данных: имени, номера телефона, адреса электронной почты или имени пользователя в мессенджере, а также сведений, которые я указал в сообщении.',
        'Цель обработки: ответ на мою заявку, подготовка коммерческого предложения, заключение и исполнение договора.',
        'Действия с данными: сбор, запись, систематизация, накопление, хранение, уточнение, использование, удаление, уничтожение — с использованием средств автоматизации и без них.',
        'Согласие действует до достижения целей обработки или до его отзыва. Отозвать согласие можно, написав на ' + S['email'] + '.',
    ]),
]


def not_found():
    # A fixed address so nginx can serve it for any missing page; links are
    # absolute because the page answers at every depth.
    p = Page('404/', absolute=True)
    body = f'''<section class="phero phero--404">
<div class="phero__glow" aria-hidden="true"></div>
<div class="phero__orb" aria-hidden="true"></div>
<div class="container phero__in">
<p class="eyebrow eyebrow--dark">Ошибка 404</p>
<h1 class="h1">Такой страницы нет</h1>
<p class="phero__lead">Возможно, её переместили. Начните с главной или посмотрите наши проекты.</p>
<div class="hero__cta"><a class="btn btn--primary btn--lg" href="/">На главную {icon("arrow")}</a><a class="btn btn--glass btn--lg" href="/projects/">Проекты</a></div>
</div>
</section>'''
    return render(p, title='Страница не найдена', desc='Страница не найдена.', body=body, index=False)


def write_themes():
    rules = [f'.t-{c["slug"]}{{--c1:{c["colors"][0]};--c2:{c["colors"][1]};--c3:{c["colors"][2]}}}' for c in C.CASES]
    path = os.path.join(OUT, 'assets', 'css', 'themes.css')
    with open(path, 'w', encoding='utf-8') as f:
        f.write('/* Generated by build.py from content.py: colours of each case. */\n' + '\n'.join(rules) + '\n')


def main():
    write_themes()
    VERSIONS['css'] = asset_version('css/main.css', 'css/themes.css')
    VERSIONS['js'] = asset_version('js/main.js')
    urls = [home(), projects()]
    urls += [case_page(i, c) for i, c in enumerate(C.CASES)]
    urls.append(blog())
    urls += [article(a) for a in C.ARTICLES]
    urls.append(contacts())
    urls.append(legal('privacy/', 'Политика обработки персональных данных',
                      'Как студия обрабатывает и защищает данные, которые вы оставляете на сайте.', PRIVACY))
    urls.append(legal('consent/', 'Согласие на обработку персональных данных',
                      'Текст согласия, которое вы даёте, отправляя форму на сайте.', CONSENT))
    not_found()
    with open(os.path.join(OUT, 'sitemap.xml'), 'w', encoding='utf-8') as f:
        f.write('<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n')
        for u in urls:
            f.write(f'<url><loc>{e(u)}</loc></url>\n')
        f.write('</urlset>\n')
    with open(os.path.join(OUT, 'robots.txt'), 'w', encoding='utf-8') as f:
        f.write(f'User-agent: *\nDisallow: /api/\nDisallow: /404/\n\nSitemap: {S["url"]}/sitemap.xml\n')
    print(f'{len(urls)} pages in {OUT}')


if __name__ == '__main__':
    main()
