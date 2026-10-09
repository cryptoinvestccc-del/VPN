#!/usr/bin/env python3
"""Screens for the case mockups: a small website for each case, in the
case's colours, at desktop (1440x900) and phone (390x844) size.

    python axiomantic/render/screens/make.py        # writes .cache/screens/*.html
    node axiomantic/render/screens/shoot.mjs        # turns them into PNG textures

They are textures on rendered laptops and phones, seen at a few hundred
pixels: big headings, clear blocks and one strong colour read better than
detail. No photos and no real brand names — every product is fictional.
"""
import html
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, '..', '.cache', 'screens')
sys.path.insert(0, os.path.join(HERE, '..', '..'))
sys.dont_write_bytecode = True
import content as C  # noqa: E402

FONT = os.path.abspath(os.path.join(HERE, '..', '..', 'site', 'assets', 'fonts', 'inter-cyrillic-opsz-normal.woff2'))
FONT_LAT = os.path.abspath(os.path.join(HERE, '..', '..', 'site', 'assets', 'fonts', 'inter-latin-opsz-normal.woff2'))

# What each fictional product's site says and shows.
SITES = {
    'english-school': {
        'brand': 'talk·lab', 'nav': ['Курсы', 'Преподаватели', 'Цены'], 'cta': 'Пробный урок',
        'h1': 'Английский для взрослых за 12 недель', 'sub': 'Живые уроки в мини-группах и разговорный клуб каждый вечер.',
        'art': 'bubbles', 'cards': [('12', 'недель до B1'), ('6', 'человек в группе'), ('24/7', 'разговорный клуб')],
    },
    'run-app': {
        'brand': 'stride', 'nav': ['Планы', 'Тренеры', 'Сообщество'], 'cta': 'Скачать',
        'h1': 'Бегай по плану, а не наугад', 'sub': 'Персональный тренер в телефоне: от первых 5 км до марафона.',
        'art': 'track', 'cards': [('5 км', 'первый забег'), ('10 км', 'за 8 недель'), ('42', 'марафон')],
    },
    'arch-bureau': {
        'brand': 'ФОРМА', 'nav': ['Проекты', 'Бюро', 'Процесс'], 'cta': 'Обсудить дом',
        'h1': 'Архитектура частных домов', 'sub': 'Проектируем дома, в которых хочется жить, — от участка до интерьера.',
        'art': 'house', 'cards': [('48', 'домов построено'), ('14', 'лет практики'), ('3', 'премии')],
    },
    'dental-clinic': {
        'brand': 'дента+', 'nav': ['Услуги', 'Врачи', 'Цены'], 'cta': 'Записаться',
        'h1': 'Стоматология без страха', 'sub': 'Онлайн-запись к нужному врачу, прозрачные цены и лечение без боли.',
        'art': 'shield', 'cards': [('15 мин', 'запись онлайн'), ('4', 'клиники'), ('0 ₽', 'консультация')],
    },
    'ceramics-shop': {
        'brand': 'глина', 'nav': ['Каталог', 'Коллекции', 'Мастерская'], 'cta': 'Корзина',
        'h1': 'Керамика ручной работы', 'sub': 'Чашки, тарелки и вазы из маленькой мастерской.',
        'art': 'cups', 'shop': True,
        'products': [('Чашка «Утро»', 1900), ('Ваза «Холм»', 3400), ('Тарелка «Лист»', 1500),
                     ('Набор «Дом»', 6200), ('Пиала «Туман»', 1200), ('Кувшин «Сад»', 4100)],
    },
    'sport-nutrition': {
        'brand': 'PEAK', 'nav': ['Каталог', 'Подбор', 'Доставка'], 'cta': 'Корзина',
        'h1': 'Питание под вашу цель', 'sub': 'Протеин, изотоники и витамины — подберём за минуту.',
        'art': 'jars', 'shop': True,
        'products': [('Протеин «Старт»', 2490), ('Изотоник «Темп»', 890), ('Аминокислоты «Ритм»', 1590),
                     ('Гейнер «Масса»', 2990), ('Витамин D3', 690), ('Шейкер', 590)],
    },
}


def art(kind, c1, c2, c3):
    """A simple illustration for the hero block, drawn in SVG."""
    if kind == 'bubbles':
        return (f'<svg viewBox="0 0 400 300"><rect x="40" y="40" width="230" height="110" rx="40" fill="{c1}"/>'
                f'<rect x="130" y="170" width="230" height="90" rx="36" fill="{c2}"/>'
                f'<rect x="80" y="80" width="140" height="14" rx="7" fill="#fff" opacity=".9"/><rect x="80" y="106" width="90" height="14" rx="7" fill="#fff" opacity=".6"/>'
                f'<rect x="170" y="200" width="150" height="12" rx="6" fill="{c1}" opacity=".7"/><rect x="170" y="222" width="90" height="12" rx="6" fill="{c1}" opacity=".45"/></svg>')
    if kind == 'track':
        return (f'<svg viewBox="0 0 400 300"><path d="M40 250 C120 250 110 70 200 70 S300 220 360 60" fill="none" stroke="{c1}" stroke-width="22" stroke-linecap="round"/>'
                f'<path d="M40 250 C120 250 110 70 200 70 S300 220 360 60" fill="none" stroke="#fff" stroke-width="3" stroke-dasharray="10 12"/>'
                f'<circle cx="360" cy="60" r="22" fill="{c2}" stroke="{c1}" stroke-width="8"/><circle cx="40" cy="250" r="14" fill="{c1}"/></svg>')
    if kind == 'house':
        return (f'<svg viewBox="0 0 400 300"><rect x="0" y="0" width="400" height="300" fill="{c2}" opacity=".55"/>'
                f'<polygon points="70,170 200,70 330,170" fill="{c1}"/><rect x="95" y="165" width="210" height="110" fill="{c1}" opacity=".85"/>'
                f'<rect x="125" y="190" width="70" height="85" fill="{c3}" opacity=".9"/><rect x="215" y="190" width="60" height="45" fill="{c3}" opacity=".9"/>'
                f'<rect x="0" y="270" width="400" height="30" fill="{c1}" opacity=".35"/></svg>')
    if kind == 'shield':
        return (f'<svg viewBox="0 0 400 300"><circle cx="200" cy="150" r="120" fill="{c2}" opacity=".6"/>'
                f'<path d="M200 50 L290 85 V150 C290 205 250 240 200 258 C150 240 110 205 110 150 V85 Z" fill="{c1}"/>'
                f'<path d="M160 150 L190 180 L245 120" fill="none" stroke="#fff" stroke-width="16" stroke-linecap="round" stroke-linejoin="round"/></svg>')
    if kind == 'cups':
        return (f'<svg viewBox="0 0 400 300"><rect width="400" height="300" fill="{c2}" opacity=".5"/>'
                f'<path d="M90 110 h120 v80 a60 60 0 0 1 -120 0 z" fill="{c1}"/><path d="M210 130 a28 28 0 0 1 0 56" fill="none" stroke="{c1}" stroke-width="14"/>'
                f'<path d="M250 150 h90 v60 a45 45 0 0 1 -90 0 z" fill="#fff"/><ellipse cx="200" cy="262" rx="160" ry="10" fill="{c1}" opacity=".25"/></svg>')
    if kind == 'jars':
        return (f'<svg viewBox="0 0 400 300"><circle cx="200" cy="150" r="130" fill="{c2}" opacity=".55"/>'
                f'<rect x="100" y="90" width="100" height="150" rx="22" fill="{c1}"/><rect x="108" y="70" width="84" height="28" rx="8" fill="#1A1A2E"/>'
                f'<rect x="215" y="120" width="85" height="120" rx="20" fill="#fff"/><rect x="222" y="104" width="71" height="22" rx="7" fill="{c1}"/>'
                f'<rect x="118" y="140" width="64" height="40" rx="8" fill="#fff" opacity=".9"/></svg>')
    return ''


def product(i, c1, c2, name, price):
    shades = [c1, c2, '#1A1A2E', c1, c2, '#ffffff']
    shape = ('<path d="M30 30 h60 v40 a30 30 0 0 1 -60 0 z" fill="{f}"/>' if i % 2 == 0
             else '<rect x="35" y="22" width="50" height="70" rx="14" fill="{f}"/>').format(f=shades[i % len(shades)])
    return (f'<div class="prod"><div class="prod__img"><svg viewBox="0 0 120 110">{shape}</svg></div>'
            f'<b>{html.escape(name)}</b><span>{price:,} ₽</span></div>'.replace(',', ' '))


CSS = """
@font-face{font-family:Inter;font-weight:100 900;src:url(file://%FONT%) format('woff2');unicode-range:U+0400-045F}
@font-face{font-family:Inter;font-weight:100 900;src:url(file://%FONTLAT%) format('woff2');unicode-range:U+0000-00FF,U+2000-206F,U+20BD}
*{box-sizing:border-box;margin:0}
body{font-family:Inter,sans-serif;color:#14142B;background:%C3%;width:%W%px;height:%H%px;overflow:hidden}
.nav{display:flex;align-items:center;gap:36px;padding:28px 64px;font-size:17px;font-weight:500}
.brand{font-weight:800;font-size:26px;letter-spacing:-.03em;margin-right:auto;color:%C1%}
.nav a{color:#14142B;opacity:.7}
.btn{display:inline-flex;align-items:center;height:52px;padding:0 28px;border-radius:999px;background:%C1%;color:#fff;font-weight:600;font-size:17px}
.hero{display:grid;grid-template-columns:1.05fr .95fr;gap:48px;align-items:center;padding:40px 64px 48px}
h1{font-size:72px;line-height:1.02;letter-spacing:-.045em;font-weight:700}
.sub{margin:24px 0 36px;font-size:21px;line-height:1.5;opacity:.7;max-width:520px}
.art{border-radius:36px;overflow:hidden;background:#fff;aspect-ratio:4/3}
.art svg{width:100%;height:100%;display:block}
.cards{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;padding:0 64px}
.card{background:#fff;border-radius:24px;padding:28px 30px}
.card b{display:block;font-size:44px;letter-spacing:-.04em;color:%C1%}
.card span{font-size:17px;opacity:.65}
.shop{display:grid;grid-template-columns:repeat(6,1fr);gap:16px;padding:0 64px}
.prod{background:#fff;border-radius:20px;padding:12px 12px 16px;font-size:14px}
.prod__img{background:%C3%;border-radius:14px;aspect-ratio:1;margin-bottom:10px}
.prod__img svg{width:100%;height:100%}
.prod b{display:block;font-weight:600}.prod span{color:%C1%;font-weight:700}
/* phone */
.m .nav{padding:22px 22px;gap:0}.m .nav a{display:none}.m .brand{font-size:24px}
.m .burger{width:28px;height:18px;border-top:3px solid #14142B;border-bottom:3px solid #14142B;position:relative}
.m .burger::after{content:"";position:absolute;left:0;right:0;top:5px;border-top:3px solid #14142B}
.m .hero{display:block;padding:14px 22px 24px}
.m h1{font-size:42px}.m .sub{font-size:17px;margin:16px 0 22px}
.m .btn{height:50px;width:100%;justify-content:center}
.m .art{margin-top:24px;border-radius:26px}
.m .cards{grid-template-columns:1fr 1fr;padding:0 22px;gap:12px}
.m .card{padding:18px}.m .card b{font-size:30px}.m .card span{font-size:14px}
.m .card:nth-child(3){display:none}
.m .shop{grid-template-columns:1fr 1fr;padding:0 22px;gap:12px}
.m .prod:nth-child(n+3){display:none}
"""


def page(slug, colors, mobile):
    s = SITES[slug]
    c1, c2, c3 = colors
    w, h = (390, 844) if mobile else (1440, 900)
    css = (CSS.replace('%FONT%', FONT).replace('%FONTLAT%', FONT_LAT).replace('%C1%', c1).replace('%C2%', c2)
           .replace('%C3%', c3).replace('%W%', str(w)).replace('%H%', str(h)))
    e = html.escape
    nav = ''.join(f'<a>{e(n)}</a>' for n in s['nav'])
    right = '<span class="burger"></span>' if mobile else f'<span class="btn">{e(s["cta"])}</span>'
    if s.get('shop'):
        below = '<div class="shop">' + ''.join(product(i, c1, c2, n, p) for i, (n, p) in enumerate(s['products'])) + '</div>'
    else:
        below = '<div class="cards">' + ''.join(f'<div class="card"><b>{e(n)}</b><span>{e(t)}</span></div>' for n, t in s['cards']) + '</div>'
    return (f'<!doctype html><html lang="ru"><head><meta charset="utf-8"><style>{css}</style></head>'
            f'<body class="{"m" if mobile else "d"}"><div class="nav"><span class="brand">{e(s["brand"])}</span>{nav}{right}</div>'
            f'<div class="hero"><div><h1>{e(s["h1"])}</h1><p class="sub">{e(s["sub"])}</p><span class="btn">{e(s["cta"] if not s.get("shop") else "Смотреть каталог")}</span></div>'
            f'<div class="art">{art(s["art"], c1, c2, c3)}</div></div>{below}</body></html>')


def main():
    os.makedirs(OUT, exist_ok=True)
    for c in C.CASES:
        for mobile in (False, True):
            name = f"{c['slug']}-{'phone' if mobile else 'desktop'}.html"
            with open(os.path.join(OUT, name), 'w', encoding='utf-8') as f:
                f.write(page(c['slug'], c['colors'], mobile))
    print('screens in', OUT)


if __name__ == '__main__':
    main()
