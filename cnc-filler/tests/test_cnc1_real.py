"""Реальная карточка cnc1.ru. Файл tests/fixtures/cnc1_card.html сохраняет tools/check_cnc1_live.py.

Ожидаемые значения сверены с живой страницей 22.10.2026
(https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/).
Пока карточка не сохранена (из облака сайт недоступен) — тест пропускается.
"""
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src" / "cncfiller"))
from cnc1_parser import parse_card, cards_to_rows  # noqa: E402

CARD = ROOT / "tests" / "fixtures" / "cnc1_card.html"
URL = "https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/"
EXPECTED = {"Обозначение резца": "MCKNL", "Форма пластины": "C (ромб 80°)", "Исполнение": "L (левое)",
            "Сечение резца, мм": "20 x 20", "e, мм": "28", "H, мм": "20", "L, мм": "125", "S, мм": "25"}


@pytest.mark.skipif(not CARD.exists(), reason="нет tests/fixtures/cnc1_card.html — запустите tools/check_cnc1_live.py")
def test_real_card():
    c = parse_card(CARD.read_text(encoding="utf-8"), URL)
    assert c["article"] == "MCKNL2020K12"
    assert c["code"] == "00059015"
    assert c["brand"] == "Sant"
    for k, v in EXPECTED.items():
        assert c["props"].get(k) == v, k
    h, rows = cards_to_rows([c])
    assert rows[0]["_Код товара"] == "00059015" and "e, мм" in h
