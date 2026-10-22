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

def test_fetch_by_code(tmp_path, monkeypatch):
    import cnc1_parser as cp
    monkeypatch.setattr(cp, "CODES_CACHE", tmp_path / "codes.json")
    card = (ROOT / "tests/fixtures/cnc1_card_synthetic.html").read_text(encoding="utf-8")
    pages = {"https://cnc1.ru/catalog/?q=00059015": '<a href="/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/">x</a>',
             URLS[1]: card}
    got = cp.fetch("00059015", get=pages.__getitem__, urls=URLS)
    assert [c["code"] for c in got] == ["00059015"]
    # второй раз — из кэша кодов, без поиска
    got2 = cp.fetch_code("59015", get={URLS[1]: card}.__getitem__)
    assert got2[0]["article"] == "MCKNL2020K12"
