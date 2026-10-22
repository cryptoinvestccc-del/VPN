"""Реальная карточка cnc1.ru MCKNL2020K12 (код 00059015).

tests/fixtures/cnc1_card.html — живая страница, снятая раннером GitHub 22.10.2026 (ветка cnc-filler-live,
совпадает со снятой в прогоне 39762816635 с точностью до сессии и случайных id; sessid замаскирован нулями).
В workflow тот же тест идёт по свежей карточке: CNC1_CARD=live_check/cnc1_card_MCKNL2020K12.html.
"""
import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src" / "cncfiller"))
from cnc1_parser import parse_card, cards_to_rows  # noqa: E402

CARD = Path(os.environ.get("CNC1_CARD") or ROOT / "tests" / "fixtures" / "cnc1_card.html")
URL = "https://cnc1.ru/catalog/reztsy_derzhavki_tokarnye/reztsy_prokhodnye/mcknr_mcknl/mcknl2020k12/"
# 15 свойств полной таблицы (properties-group) + «Длина резца, мм» из краткого списка (properties__)
EXPECTED = {"Бренд": "Sant", "Обозначение резца": "MCKNL", "Крепление пластины": "M - Прижим клин-прихватом сверху",
            "Форма пластины": "C (ромб 80°)", "Задний угол пластины": "N (0°)", "Исполнение": "L (левое)",
            "Сечение резца, мм": "20 x 20", "Главный угол в плане": "K (75°)", "Устанавливаемая пластина": "CN.. 1204..",
            "a, мм": "20", "b, мм": "20", "e, мм": "28", "H, мм": "20", "L, мм": "125", "S, мм": "25",
            "Длина резца, мм": "125"}


def test_real_card():
    c = parse_card(CARD.read_text(encoding="utf-8"), URL)
    assert c["article"] == "MCKNL2020K12"
    assert c["code"] == "00059015"
    assert c["brand"] == "Sant"
    for k, v in EXPECTED.items():
        assert c["props"].get(k) == v, k
    assert not [k for k in c["props"] if k.startswith("#")]   # шаблон #PROP_TITLE# не попадает
    h, rows = cards_to_rows([c])
    assert rows[0]["_Код товара"] == "00059015" and rows[0]["Бренд"] == "Sant" and rows[0]["e, мм"] == "28"


def test_real_card_exact_props():
    """Сохранённая карточка — ровно 16 свойств (на живой странице сайт может добавить новые)."""
    if os.environ.get("CNC1_CARD"):
        return
    assert parse_card(CARD.read_text(encoding="utf-8"), URL)["props"] == EXPECTED
