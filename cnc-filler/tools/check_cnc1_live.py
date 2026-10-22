"""Проверка парсера на живом cnc1.ru — шаг workflow .github/workflows/build-cnc-filler.yml перед сборкой exe.

1. Карта сайта → карточки артикула MCKNL2020K12 (на сайте их две: 00059015 и 00115826).
2. Поиск сайта по коду 00059015.
3. HTML карточки и страницы поиска — в live_check/ (artifact прогона); по свежей карточке следующим шагом идёт
   tests/test_cnc1_real.py (CNC1_CARD=live_check/…). Сохранённая карточка tests/fixtures/cnc1_card.html не перезаписывается.
4. Код выхода 1, если parse_card не достал код 00059015, бренд Sant и «e, мм» = 28.
   Поиск по коду не найден — предупреждение в прогоне, сборку не останавливает.
В лог печатаются куски разметки вокруг заголовка, кода и характеристик — по ним правится парсер.
Только чтение, не чаще 1 запроса в секунду.
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src" / "cncfiller"))
import cnc1_parser as cp  # noqa: E402

ARTICLE, CODE = "MCKNL2020K12", "00059015"
KNOWN_URL = "https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/"
OUT = ROOT / "live_check"


def snippet(html: str, marker: str, width=900) -> str:
    i = html.find(marker)
    if i < 0:
        return f"  [{marker!r} не найден]"
    return "  " + re.sub(r"\s+", " ", html[max(0, i - width // 3): i + width])


def main() -> int:
    OUT.mkdir(exist_ok=True)
    errors = []

    try:
        urls = cp.load_index(refresh=True)
        print(f"Карта сайта: {len(urls)} адресов в каталоге")
    except Exception as ex:  # noqa: BLE001
        urls = []
        print(f"::warning::Карта сайта не загрузилась: {ex}")
    found = cp.find_urls(ARTICLE, urls)
    print(f"Артикул {ARTICLE}: {len(found)} карточек")
    for u in found:
        print("  ", u)
    if not found:
        print(f"::warning::{ARTICLE} не найден в карте сайта, беру известный адрес")

    card_url = next((u for u in found if u.rstrip("/").endswith(ARTICLE.lower())), KNOWN_URL)
    html = cp.http_get(card_url)
    (OUT / f"cnc1_card_{ARTICLE}.html").write_text(html, encoding="utf-8")
    print(f"Карточка {card_url}: {len(html)} символов → live_check/cnc1_card_{ARTICLE}.html")

    print("\nРазметка (классы в карточке):")
    for cls in cp.NAME_CLS + cp.VALUE_CLS + ("props_list", "properties__item", "itemprop=\"name\"", "itemprop=\"value\""):
        print(f"  {cls}: {html.count(cls)}")
    for marker in ("<h1", "Код товара", ">Бренд<", "Бренд", "e, мм"):
        print(f"--- вокруг {marker!r}:\n{snippet(html, marker)}")

    c = cp.parse_card(html, card_url)
    print(f"\nparse_card: артикул={c['article']} код={c['code']} бренд={c['brand']} характеристик={len(c['props'])}")
    for k, v in c["props"].items():
        print(f"   {k}: {v}")
    if c["code"] != CODE:
        errors.append(f"код товара {c['code']!r}, ожидался {CODE}")
    if c["brand"] != "Sant":
        errors.append(f"бренд {c['brand']!r}, ожидался Sant")
    if c["props"].get("e, мм") != "28":
        errors.append(f"«e, мм» = {c['props'].get('e, мм')!r}, ожидалось 28")

    search_url = cp.BASE + cp.SEARCH_URLS[0].format(q=CODE)
    search = cp.http_get(search_url)
    (OUT / f"cnc1_search_{CODE}.html").write_text(search, encoding="utf-8")
    print(f"\nПоиск {search_url}: {len(search)} символов; ссылки результатов: {cp._result_links(search, cp.http_get)[:10]}")
    by_code = cp.fetch_code(CODE, urls=urls)
    print(f"fetch_code({CODE}): {[x['url'] for x in by_code]}")
    if not any(x["code"] == CODE for x in by_code):
        print(f"::warning::Поиск по коду {CODE} не нашёл карточку — проверить SEARCH_URLS")

    if errors:
        for e in errors:
            print(f"::error::{e}")
        return 1
    print("\nПарсер работает на живом сайте.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
