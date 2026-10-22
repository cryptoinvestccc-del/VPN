"""Живая проверка парсера cnc1.ru (запускается в GitHub Actions). Сохраняет HTML в live_html/ и падает при ошибке."""
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src" / "cncfiller"))
import cnc1_parser as cp

out = Path("live_html"); out.mkdir(exist_ok=True)
url = "https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/"
html = cp.http_get(url)
(out / "mcknl2020k12.html").write_text(html, encoding="utf-8")
c = cp.parse_card(html, url)
print("CARD:", c["article"], c["code"], c["brand"], len(c["props"]), "props")
for k, v in c["props"].items():
    print("  ", k, "=", v)
errors = []
if c["code"] != "00059015": errors.append(f"код {c['code']!r} != 00059015")
if c["brand"] != "Sant": errors.append(f"бренд {c['brand']!r} != Sant")
if c["props"].get("e, мм") != "28": errors.append(f"e, мм = {c['props'].get('e, мм')!r} != 28")
for tpl in cp.SEARCH_URLS:
    try:
        h = cp.http_get(cp.BASE + tpl.format(q="00059015"))
        (out / ("search_" + tpl.split("/")[1] + ".html")).write_text(h, encoding="utf-8")
        print("SEARCH", tpl, "mcknl2020k12" in h.lower())
    except Exception as ex:
        print("SEARCH", tpl, "ERROR", ex)
try:
    sm = cp.http_get(cp.BASE + "/sitemap.xml"); (out / "sitemap.xml").write_text(sm, encoding="utf-8")
    print("SITEMAP ok", sm.count("<loc>"))
except Exception as ex:
    print("SITEMAP ERROR", ex)
if errors:
    print("FAIL:", "; ".join(errors)); sys.exit(1)
print("OK")
