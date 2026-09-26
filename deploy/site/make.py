#!/usr/bin/env python3
"""Builds the public pages from the same texts the app shows.

Google Play wants the privacy policy at a public address, and it has to
say the same thing as the one inside the app, so the pages are generated
from android/app/res/raw*/ rather than written twice. No fonts, scripts
or counters are loaded from anywhere: the policy says there is no
analytics, and the page it lives on should not be the exception.

    python3 deploy/site/make.py      # writes deploy/site/out/
"""
import html, os, re

HERE = os.path.dirname(os.path.abspath(__file__))
RES = os.path.join(HERE, '..', '..', 'android', 'app', 'res')
OUT = os.path.join(HERE, 'out')

STYLE = """
:root{color-scheme:dark;--bg:#050506;--ink:#e9ebef;--dim:#9a9ea8;--line:#23252b;--card:#0d0e11}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif}
main{max-width:760px;margin:0 auto;padding:40px 20px 64px}
.brand{font:italic 900 28px/1 Arial Black,Arial,sans-serif;letter-spacing:.02em;color:#050506;-webkit-text-stroke:1.6px #e6e8ec;paint-order:stroke fill;text-decoration:none}
nav{display:flex;flex-wrap:wrap;gap:8px 18px;margin:18px 0 34px;font-size:14px}
nav a{color:var(--dim);text-decoration:none;border-bottom:1px solid var(--line)}
nav a:hover,nav a.on{color:var(--ink);border-color:var(--ink)}
h1{font-size:30px;line-height:1.2;margin:0 0 6px}
.ver{color:var(--dim);font-size:14px;margin:0 0 28px}
h2{font-size:18px;margin:30px 0 8px}
p{margin:0 0 12px}
ul{margin:0 0 12px;padding-left:22px}
.lead{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:16px 18px;margin:0 0 8px}
.lang{float:right;font-size:14px}
.lang a{color:var(--dim)}
footer{margin-top:48px;padding-top:18px;border-top:1px solid var(--line);color:var(--dim);font-size:13px}
a{color:#cfd3db}
"""

def page(lang, title, body, active, other_href):
    t = {'ru': {'home': 'Главная', 'privacy': 'Конфиденциальность', 'terms': 'Условия', 'other': 'English', 'who': 'ИП Задорожный В. В., ИНН 380806643503'},
         'en': {'home': 'Home', 'privacy': 'Privacy', 'terms': 'Terms', 'other': 'Русский', 'who': 'IP Zadorozhny V. V., INN 380806643503'}}[lang]
    pre = '' if lang == 'ru' else 'en/'
    root = '/' 
    links = [('home', root + pre), ('privacy', root + pre + 'privacy.html'), ('terms', root + pre + 'terms.html')]
    nav = ''.join('<a href="%s"%s>%s</a>' % (href, ' class="on"' if k == active else '', t[k]) for k, href in links)
    return ('<!doctype html>\n<html lang="%s"><head><meta charset="utf-8">'
            '<meta name="viewport" content="width=device-width,initial-scale=1">'
            '<meta name="referrer" content="no-referrer">'
            '<title>%s</title><style>%s</style></head><body><main>'
            '<span class="lang"><a href="%s">%s</a></span>'
            '<a class="brand" href="%s">BESY VPN</a><nav>%s</nav>%s'
            '<footer>BESY VPN · %s · <a href="mailto:workmail196@yahoo.com">workmail196@yahoo.com</a></footer>'
            '</main></body></html>\n') % (lang, html.escape(title), STYLE, other_href, t['other'], root + pre, nav, body, t['who'])

def from_text(path):
    """First line: title. Second: version. A line "N. …" is a heading, "• …" a list item, the rest paragraphs."""
    lines = [l.rstrip() for l in open(path, encoding='utf-8').read().strip().split('\n')]
    title, version = lines[0], lines[1]
    out, items = [], []
    def flush():
        if items:
            out.append('<ul>' + ''.join('<li>%s</li>' % linkify(i) for i in items) + '</ul>')
            items.clear()
    first = True
    body = [l for l in lines[2:] if l.strip()]
    for n, l in enumerate(body):
        if l.startswith('• '):
            items.append(l[2:]); continue
        flush()
        # a numbered line is a heading only when text follows it; the
        # terms are a numbered list of whole sentences
        nxt = body[n + 1] if n + 1 < len(body) else ''
        if re.match(r'^\d+\. ', l) and len(l) < 70 and nxt and not re.match(r'^\d+\. ', nxt):
            out.append('<h2>%s</h2>' % html.escape(l))
        else:
            cls = ' class="lead"' if l.startswith('Коротко') or l.startswith('In short') else ''
            out.append('<p%s>%s</p>' % (cls, linkify(l)))
        first = False
    flush()
    return title, '<h1>%s</h1><p class="ver">%s</p>%s' % (html.escape(title), html.escape(version), ''.join(out))

def linkify(s):
    s = html.escape(s)
    return re.sub(r'([\w.+-]+@[\w-]+\.[\w.]+)', r'<a href="mailto:\1">\1</a>', s)

HOME = {
 'ru': ('BESY VPN', '<h1>BESY VPN</h1><p class="ver">Бесплатный VPN для Android на протоколе AmneziaWG.</p>'
        '<p class="lead">Без аккаунтов и рекламы. Одна кнопка — и соединение защищено. Ключ шифрования создаётся на телефоне и не покидает его.</p>'
        '<h2>Документы</h2><ul><li><a href="/privacy.html">Политика конфиденциальности</a></li><li><a href="/terms.html">Условия использования</a></li></ul>'
        '<h2>Удаление данных</h2><p>В приложении: Настройки → «Удалить ключ и данные». Ключ удаляется с телефона и с сервера сразу. Вопросы: <a href="mailto:workmail196@yahoo.com">workmail196@yahoo.com</a></p>'),
 'en': ('BESY VPN', '<h1>BESY VPN</h1><p class="ver">A free VPN for Android on the AmneziaWG protocol.</p>'
        '<p class="lead">No accounts, no ads. One button and the connection is protected. The encryption key is created on the phone and never leaves it.</p>'
        '<h2>Documents</h2><ul><li><a href="/en/privacy.html">Privacy policy</a></li><li><a href="/en/terms.html">Terms of use</a></li></ul>'
        '<h2>Deleting your data</h2><p>In the app: Settings → "Delete key and data". The key is removed from the phone and the server at once. Questions: <a href="mailto:workmail196@yahoo.com">workmail196@yahoo.com</a></p>'),
}

def main():
    for lang, raw, pre in (('ru', 'raw', ''), ('en', 'raw-en', 'en/')):
        os.makedirs(os.path.join(OUT, pre), exist_ok=True)
        other = lambda name: '/' + ('en/' if lang == 'ru' else '') + name
        title, body = HOME[lang]
        open(os.path.join(OUT, pre, 'index.html'), 'w', encoding='utf-8').write(page(lang, title, body, 'home', other('')))
        for name in ('privacy', 'terms'):
            title, body = from_text(os.path.join(RES, raw, name + '.txt'))
            open(os.path.join(OUT, pre, name + '.html'), 'w', encoding='utf-8').write(page(lang, title, body, name, other(name + '.html')))
    print('pages in', OUT)

if __name__ == '__main__':
    main()
