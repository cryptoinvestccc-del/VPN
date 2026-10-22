"""Собирает data/catalog.db из сверенных seed-данных и OCR-индекса каталогов.

Запуск: python src/cncfiller/build_db.py
"""
import csv
import json
import re
import sqlite3
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "data"
DB = DATA / "catalog.db"

sys.path.insert(0, str(Path(__file__).resolve().parent))
from articles import expand_pattern, norm  # noqa: E402

CATALOGS = [
    # code, brand, site_brand, title, page_rule
    ("UNKNOWN112", "MetalCraft", "MetalCraft", "kkdaoku / MetalCraft (Wenling Youyi)", ""),
    ("YOUYIQIE", "Youyiqie", "MetalCraft", "Youyiqie CNC — токарные державки", "(c+5)//2"),
    ("WEIDING", "PTHK", "MetalCraft", "PTHK / Guangdong Weiding", ""),
    ("SANT2022", "Sant", "Sant", "SANT 2022-2023", ""),
    ("SANTTURN", "Sant", "Sant", "SANT 2024-2025 Turning", ""),
    ("ZZJG", "Jinggong", "Jinggong", "Zhuzhou Jinggong 2025-2026", ""),
    ("HUAREAL_2627", "Huareal", "Huareal", "Huareal 2026-2027", ""),
    ("HUAREAL_CN", "Huareal", "Huareal", "Huareal 2024-2025 (кит.)", ""),
    ("HUAREAL_EN", "Huareal", "Huareal", "Huareal 2024-2025 Official", ""),
    ("KYLIN2025", "Huareal", "Huareal", "Huareal Kylin 2025", ""),
    ("HU_ULTRA", "Huareal", "Huareal", "Huareal Ultra Universal 2025", ""),
    ("HU_HSERIES", "Huareal", "Huareal", "Huareal H-series", ""),
    ("HU_USERIES", "Huareal", "Huareal", "Huareal U-series", ""),
    ("HU_MINI", "Huareal", "Huareal", "Huareal мини-расточные", ""),
    ("HADSTO", "Hadsto", "Huareal", "HADSTO 2024-2025", ""),
    ("BRIGHT", "Bright-Tools", "Bright-Tools", "Bright-Tools 2025", ""),
    ("AONE", "A-ONE", "A-ONE", "A-ONE WEDM", ""),
    ("MAKI", "Maki Bishi", "Maki Bishi", "MAKI BISHI EDM", ""),
]
ALIASES = {"kkdaoku": "MetalCraft", "wenling youyi": "MetalCraft", "metalcraft": "MetalCraft",
           "hadsto": "Hadsto", "huareal": "Huareal", "sant": "Sant", "pthk": "PTHK", "weiding": "PTHK"}


def build(db_path: Path = DB) -> dict:
    if db_path.exists():
        db_path.unlink()
    con = sqlite3.connect(db_path)
    con.executescript((ROOT / "db" / "schema.sql").read_text(encoding="utf-8"))
    cur = con.cursor()

    brand_id = {}
    for _, brand, site_brand, _, _ in CATALOGS:
        if brand not in brand_id:
            cur.execute("INSERT INTO brand(name, site_brand) VALUES(?,?)", (brand, site_brand))
            brand_id[brand] = cur.lastrowid
    for alias, brand in ALIASES.items():
        cur.execute("INSERT INTO brand_alias VALUES(?,?)", (alias, brand_id[brand]))

    cat_id = {}
    for code, brand, _, title, rule in CATALOGS:
        cur.execute("INSERT INTO catalog(code, brand_id, title, page_rule) VALUES(?,?,?,?)",
                    (code, brand_id[brand], title, rule))
        cat_id[code] = cur.lastrowid

    # OCR-индекс страниц + полнотекстовый поиск
    pages = 0
    for code in cat_id:
        f = DATA / "index" / f"idx_{code}.json"
        if not f.exists():
            continue
        for p, text in json.loads(f.read_text(encoding="utf-8")).items():
            cur.execute("INSERT INTO page(catalog_id, pdf_page, ocr_text) VALUES(?,?,?)", (cat_id[code], int(p), text))
            pages += 1
    cur.execute("INSERT INTO page_fts(page_fts) VALUES('rebuild')")

    groups = {}
    params = {"d": "Диаметр хвостовика/сечение d", "h": "h", "b": "b", "L": "Общая длина", "L1": "L1 / длина головки",
              "H": "H", "f": "Смещение вершины f", "F": "Смещение вершины F", "A": "A (PTHK)", "Dmin": "Мин. диаметр растачивания"}
    for code, name in params.items():
        cur.execute("INSERT INTO param VALUES(?,?,?)", (code, name, "мм"))

    # Сверенные таблицы
    series_id, item_ids = {}, {}
    n_items = 0
    with open(DATA / "seed" / "verified_params.csv", encoding="utf-8") as fh:
        for row in csv.DictReader(fh):
            g = row["group"]
            if g not in groups:
                cur.execute("INSERT INTO product_group(code, name) VALUES(?,?)", (g, g))
                groups[g] = cur.lastrowid
            skey = (row["catalog"], row["series"])
            if skey not in series_id:
                cur.execute("INSERT INTO series(catalog_id, group_id, code) VALUES(?,?,?)",
                            (cat_id[row["catalog"]], groups[g], row["series"]))
                series_id[skey] = cur.lastrowid
            page = row["page_pdf"] or None
            page_cat = row["page_catalog"] or None
            for art, hand in expand_pattern(row["article_pattern"]):
                ikey = (row["catalog"], art)
                if ikey not in item_ids:
                    cur.execute("INSERT INTO item(series_id, article_norm, article_raw, hand, page_id, verified) VALUES(?,?,?,?,?,1)",
                                (series_id[skey], norm(art), art, hand, None))
                    item_ids[ikey] = cur.lastrowid
                    cur.execute("UPDATE item SET page_id=(SELECT id FROM page WHERE catalog_id=? AND pdf_page=?) WHERE id=?",
                                (cat_id[row["catalog"]], page, item_ids[ikey]))
                    n_items += 1
                note = row["note"] + (f"; стр. каталога {page_cat}" if page_cat else "")
                cur.execute("INSERT OR REPLACE INTO item_param VALUES(?,?,?,?,?,?)",
                            (item_ids[ikey], row["param"], float(row["value"]), None, "catalog", note))

    # Сплавы
    ig = json.loads((DATA / "seed" / "inserts_grades.json").read_text(encoding="utf-8"))
    for code, g in ig["grades"].items():
        b = g.get("brand", "Huareal")
        cur.execute("INSERT INTO grade(brand_id, code, iso, coating, composition, insert_type, site_material) VALUES(?,?,?,?,?,?,?)",
                    (brand_id[b], code, g.get("iso"), g.get("coating"), g.get("composition"), g.get("type"), g.get("site_material")))

    # Справочники 1С
    dicts = json.loads((DATA / "dicts" / "1c_dictionaries.json").read_text(encoding="utf-8"))
    for name, values in dicts.items():
        if name.startswith("_"):
            continue
        for v in values:
            cur.execute("INSERT INTO dict_value VALUES(?,?)", (name, v))

    con.commit()
    con.close()
    return {"pages": pages, "items": n_items, "grades": len(ig["grades"])}


if __name__ == "__main__":
    print(build())
