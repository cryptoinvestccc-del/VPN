"""Запуск: python -m pytest tests  (сначала python src/cncfiller/build_db.py)"""
import sys
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src" / "cncfiller"))
from engine import Engine, read_table, fill_rows  # noqa: E402
from articles import expand_pattern  # noqa: E402

E = Engine(str(ROOT / "data" / "catalog.db"))

def test_expand():
    assert expand_pattern("S16Q-SVU_11")[0] == ("S16Q-SVUBR11", "R")
    assert expand_pattern("SNR/L0008K08") == [("SNR0008K08", "R"), ("SNL0008K08", "L")]

def test_thread_from_catalog():
    v, s, m = E.site_values("SNR0032T16", "MetalCraft")
    assert m and v["Dmin, мм"] == 36 and v["S, мм"] == 18.5 and v["L1, мм"] == 38

def test_pthk_formula():
    v, s, m = E.site_values("C10M-SCLCR06", "MetalCraft")
    assert v["S, мм"] == 6  # A 11 − d/2

def test_no_guessing():
    v, s, m = E.site_values("S40U-SVUCR16", "MetalCraft")
    assert m is None and "Dmin, мм" not in v and v["Длина резца, мм"] == 350

def test_sant_not_filled_from_metalcraft():
    assert E.find("SNR0016M16", "Sant") == []

import pytest

@pytest.mark.skipif(not (ROOT / "data" / "samples" / "bitrix_threading_SNR_SNL.xlsx").exists(), reason="samples не в репозитории")
def test_only_empty_and_no_blanks():
    h, rows = read_table(ROOT / "data" / "samples" / "bitrix_threading_SNR_SNL.xlsx")
    r = fill_rows(E, h, rows)
    assert r.filled_cells == 409
    for x in r.rows:
        for c in x["filled"]:
            orig = next(rr for rr in rows if str(rr["Артикул"]).strip() == x["values"]["Артикул"]).get(c)
            assert orig in (None, "") or str(orig).strip() in ("", "-")
