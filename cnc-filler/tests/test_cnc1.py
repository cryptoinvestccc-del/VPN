"""Офлайн-тесты парсера cnc1.ru. Синтетическая разметка — заменить реальной сохранённой карточкой (tests/fixtures/cnc1_card.html)."""
import sys
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src" / "cncfiller"))
from cnc1_parser import find_urls, parse_card, cards_to_rows, product_urls_from_sitemaps  # noqa: E402

URLS = ["https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_rastochnye/s_sckcr_s_sckcl/s12m_sckcr06_rezets_tokarnyy_derzhavka_rastochnoy_po_metallu/",
        "https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/",
        "https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_rastochnye/s_sckcr_s_sckcl/s12m_sckcr06h_rezets/"]

def test_find_by_slug():
    assert find_urls("S12M-SCKCR06", URLS) == [URLS[0]]
    assert find_urls("mcknl 2020 k12", URLS) == [URLS[1]]
    assert find_urls("S12M-SCKCR", URLS) == []

def test_parse_card():
    c = parse_card((ROOT / "tests/fixtures/cnc1_card_synthetic.html").read_text(encoding="utf-8"), URLS[1])
    assert c["article"] == "MCKNL2020K12" and c["code"] == "00059015" and c["brand"] == "Sant"
    assert c["props"]["e, мм"] == "28" and c["props"]["Форма пластины"] == "C (ромб 80°)"
    h, rows = cards_to_rows([c])
    assert rows[0]["S, мм"] is None and "L, мм" in h

def test_sitemap():
    pages = {"https://cnc1.ru/sitemap.xml": "<loc>https://cnc1.ru/sitemap-iblock-536.part1.xml</loc><loc>https://cnc1.ru/sitemap-files.xml</loc>",
             "https://cnc1.ru/sitemap-iblock-536.part1.xml": "<loc>" + URLS[1] + "</loc><loc>https://cnc1.ru/catalog/stanki/</loc>"}
    assert product_urls_from_sitemaps(pages.__getitem__) == [URLS[1]]

MENU = '<a href="/catalog/frezy_korpusnye/frezy_diskovye/pt01/">меню</a><a href="/catalog/stanki/">меню</a>'

def test_fetch_by_code(tmp_path, monkeypatch):
    import cnc1_parser as cp
    monkeypatch.setattr(cp, "CODES_CACHE", tmp_path / "codes.json")
    monkeypatch.setattr(cp, "_nav", [])
    card = (ROOT / "tests/fixtures/cnc1_card_synthetic.html").read_text(encoding="utf-8")
    # как на живом сайте: код ищется с ведущими нулями; ссылки меню (есть и на пустом поиске) не открываются —
    # их нет в pages, обращение к ним уронило бы тест
    pages = {"https://cnc1.ru/catalog/?q=zzqx00000": MENU,
             "https://cnc1.ru/catalog/?q=00059015": MENU + '<a href="/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/">x</a>',
             URLS[1]: card}
    got = cp.fetch("00059015", get=pages.__getitem__, urls=URLS)
    assert [c["code"] for c in got] == ["00059015"]
    # второй раз — из кэша кодов, без поиска
    got2 = cp.fetch_code("59015", get={URLS[1]: card}.__getitem__)
    assert got2[0]["article"] == "MCKNL2020K12"


def test_code_search_falls_back_to_short_code(tmp_path, monkeypatch):
    import cnc1_parser as cp
    monkeypatch.setattr(cp, "CODES_CACHE", tmp_path / "codes.json")
    monkeypatch.setattr(cp, "_nav", [set()])
    card = (ROOT / "tests/fixtures/cnc1_card_synthetic.html").read_text(encoding="utf-8")
    pages = {"https://cnc1.ru/catalog/?q=00059015": "ничего не найдено",
             "https://cnc1.ru/catalog/?q=59015": '<a href="https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/">x</a>',
             URLS[1]: card}
    assert [c["code"] for c in cp.fetch_code("59015", get=pages.__getitem__)] == ["00059015"]

def test_parse_card_markup_robust():
    """Подсказка «?» в названии, <img>/<br> в значении, код другого товара до заголовка — не мешают."""
    html = ('<div class="menu">Код товара: 00000001</div><h1>MCKNL2020K12 Державка</h1><p>Код товара: 00059015</p>'
            '<table class="props_list nbg"><tr><td class="char_name"><div class="props_item"><span itemprop="name">Бренд</span>'
            '<div class="hint"><span class="icon"><i>?</i></span><div class="tooltip">Производитель</div></div></div></td>'
            '<td class="char_value"><img src="/logo.png"><span itemprop="value">Sant</span><br></td></tr>'
            '<tr><td class="char_name"><span>e, мм</span></td><td class="char_value"><span>28</span></td></tr></table>')
    c = cp_parse(html)
    assert c["code"] == "00059015" and c["brand"] == "Sant" and c["props"]["e, мм"] == "28"
    assert list(c["props"]) == ["Бренд", "e, мм"]

def cp_parse(html):
    return parse_card(html, URLS[1])

def test_parse_card_properties_group_and_template():
    """Разметка живого сайта: properties-group__name/__value, JS-шаблон <template> с #PROP_TITLE#, подсказка того же тега."""
    html = ('<h1>MCKNL2020K12 Державка</h1><span class="code_text">Код товара: </span><span class="code">00059015</span>'
            '<div class="properties__item"><div class="properties__title">Длина резца, мм</div>'
            '<div class="properties__value">125</div></div>'
            '<div class="properties-group__item"><div class="properties-group__name-wrap">'
            '<span itemprop="name" class="properties-group__name muted">Бренд</span></div>'
            '<div class="properties-group__value-wrap"><div class="properties-group__value color_dark" itemprop="value"> Sant </div></div></div>'
            '<div class="properties-group__item"><div class="properties-group__name">e, мм<div class="hint"><div class="tooltip">?</div></div></div>'
            '<div class="properties-group__value"> 28 </div></div>'
            '<template class="props-template"><div class="properties__item"><div class="properties__title"> #PROP_TITLE# </div>'
            '<div class="properties__value">#PROP_VALUE#</div></div></template>'
            '<script>var t = "<div class=\\"properties__title\\">X</div>";</script>')
    c = parse_card(html, URLS[1])
    assert c["code"] == "00059015" and c["brand"] == "Sant"
    assert c["props"] == {"Длина резца, мм": "125", "Бренд": "Sant", "e, мм": "28"}

def test_parse_card_drops_hash_placeholders():
    """Шаблон «#…#» вне <template> (например, в разметке без тега template) тоже отбрасывается."""
    html = ('<h1>X1</h1><div class="properties__title">#PROP_TITLE#</div><div class="properties__value">#PROP_VALUE#</div>'
            '<div class="properties__title">L, мм</div><div class="properties__value">125</div>')
    assert parse_card(html, URLS[1])["props"] == {"L, мм": "125"}
