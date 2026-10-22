"""Парсер карточек товаров с cnc1.ru по артикулу.

Как работает:
1. Индекс URL товаров берётся из карты сайта (sitemap.xml → sitemap-iblock-*.xml) и кэшируется в data/cnc1_urls.json.
   В URL карточки есть артикул: .../reztsy_rastochnye/s_sckcr_s_sckcl/s12m_sckcr06_rezets_tokarnyy.../
2. Артикул ищется по совпадению начала «слага» (S12M-SCKCR06 → s12m_sckcr06…). Один артикул может иметь
   несколько карточек (дубли с разными кодами) — возвращаются все.
3. Карточка разбирается: заголовок, «Код товара», бренд, все характеристики (шаблон сайта Aspro Max).

Сайт — собственный сайт магазина; запросы идут не чаще 1 в секунду, без авторизации, только чтение.
Живая проверка разметки — tools/check_cnc1_live.py (шаг workflow), реальная карточка — tests/fixtures/cnc1_card.html.
"""
import json
import os
import re
import sys
import time
import urllib.request
from html import unescape
from html.parser import HTMLParser
from pathlib import Path

from articles import norm

BASE = "https://cnc1.ru"
UA = "cnc-filler/1.0 (internal catalog tool)"


def _cache_dir() -> Path:
    """data/ проекта; в собранном exe — %LOCALAPPDATA%\\cnc-filler (папка _MEIPASS временная и стирается)."""
    if getattr(sys, "frozen", False):
        return Path(os.environ.get("LOCALAPPDATA") or Path.home()) / "cnc-filler"
    return Path(__file__).resolve().parents[2] / "data"


CACHE = _cache_dir() / "cnc1_urls.json"
_last = [0.0]


def _save_json(path: Path, obj):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, ensure_ascii=False), encoding="utf-8")


def http_get(url: str, timeout=20) -> str:
    wait = 1.0 - (time.time() - _last[0])
    if wait > 0:
        time.sleep(wait)
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=timeout) as r:  # noqa: S310 — только https://cnc1.ru
        _last[0] = time.time()
        return r.read().decode("utf-8", errors="replace")


# ---------- индекс URL из карты сайта ----------
def product_urls_from_sitemaps(get=http_get) -> list[str]:
    index = get(f"{BASE}/sitemap.xml")
    maps = re.findall(r"<loc>\s*([^<]+?)\s*</loc>", index)
    urls = []
    for m in maps:
        if "iblock" not in m and "aspro" not in m:
            continue
        xml = get(m)
        for u in re.findall(r"<loc>\s*([^<]+?)\s*</loc>", xml):
            path = u.split("cnc1.ru", 1)[-1]
            if path.startswith("/catalog/") and path.count("/") >= 5:   # раздел/подраздел/группа/товар
                urls.append(u)
    return sorted(set(urls))


def load_index(refresh=False, get=http_get) -> list[str]:
    if CACHE.exists() and not refresh:
        return json.loads(CACHE.read_text(encoding="utf-8"))
    urls = product_urls_from_sitemaps(get)
    _save_json(CACHE, urls)
    return urls


def slug_keys(url: str) -> list[str]:
    """Все «префиксы» слага, склеенные по токенам: s12m_sckcr06_rezets → S12M, S12MSCKCR06, S12MSCKCR06REZETS…"""
    slug = url.rstrip("/").rsplit("/", 1)[-1]
    toks = slug.split("_")
    out, acc = [], ""
    for t in toks[:6]:
        acc += t
        out.append(norm(acc))
    return out


def find_urls(article: str, urls: list[str]) -> list[str]:
    key = norm(article)
    return [u for u in urls if key in slug_keys(u)]


# ---------- разбор карточки (Aspro Max) ----------
NAME_CLS = ("char_name", "properties__title", "props_item_name")
VALUE_CLS = ("char_value", "properties__value", "props_item_value")
SKIP_CLS = ("hint", "tooltip")   # подсказки «?» у названий свойств — не часть названия
VOID = {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}


def _has(cls: str, keys) -> bool:
    words = cls.split()
    return any(k in words for k in keys)


class _Card(HTMLParser):
    """Не полагается на стек тегов: незакрытые <img>/<br> внутри ячейки не ломают разбор.

    Ячейка названия/значения читается до закрывающего тега того же имени с учётом вложенности.
    """

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.h1 = []
        self.h1_depth = 0
        self.props = []          # (name, value)
        self.cur = None          # 'name'|'value'
        self.cur_tag, self.cur_depth = None, 0
        self.skip_tag, self.skip_depth = None, 0
        self.buf = []
        self.pending_name = None
        self.text = []
        self.title_pos = None    # позиция заголовка в тексте — код товара ищем после него

    def handle_starttag(self, tag, attrs):
        if tag in VOID:
            return
        cls = dict(attrs).get("class", "") or ""
        if tag == "h1":
            self.h1_depth += 1
            if self.title_pos is None:
                self.title_pos = len(self.text)
        if self.skip_tag:
            if tag == self.skip_tag:
                self.skip_depth += 1
            return
        if self.cur:
            if tag == self.cur_tag:
                self.cur_depth += 1
            if _has(cls, SKIP_CLS):
                self.skip_tag, self.skip_depth = tag, 0
            return
        if _has(cls, NAME_CLS):
            self.cur, self.cur_tag, self.cur_depth, self.buf = "name", tag, 0, []
        elif _has(cls, VALUE_CLS):
            self.cur, self.cur_tag, self.cur_depth, self.buf = "value", tag, 0, []

    def handle_endtag(self, tag):
        if tag == "h1" and self.h1_depth:
            self.h1_depth -= 1
        if self.skip_tag:
            if tag == self.skip_tag:
                if self.skip_depth:
                    self.skip_depth -= 1
                else:
                    self.skip_tag = None
            return
        if not self.cur or tag != self.cur_tag:
            return
        if self.cur_depth:
            self.cur_depth -= 1
            return
        txt = re.sub(r"\s+", " ", unescape("".join(self.buf))).strip().rstrip(":").strip()
        if self.cur == "name":
            self.pending_name = txt
        elif self.pending_name:
            self.props.append((self.pending_name, txt))
            self.pending_name = None
        self.cur = None

    def handle_data(self, data):
        if self.h1_depth:
            self.h1.append(data)
        if self.cur and not self.skip_tag:
            self.buf.append(data)
        self.text.append(data)


def parse_card(html: str, url: str = "") -> dict:
    p = _Card()
    p.feed(html)
    p.close()
    after_title = " ".join(p.text[p.title_pos or 0:])   # «Код товара» похожих товаров в меню/шапке не берём
    text = re.sub(r"\s+", " ", unescape(after_title))
    title = re.sub(r"\s+", " ", "".join(p.h1)).strip()
    code = re.search(r"Код товара:?\s*(\d{5,8})", text)
    props = {}
    for k, v in p.props:          # полная таблица идёт после краткой — берём первое непустое
        if k and v and k not in props:
            props[k] = v
    article = title.split(" ")[0] if title else ""
    return {"url": url, "title": title, "article": article,
            "code": code.group(1).zfill(8) if code else "", "brand": props.get("Бренд", ""), "props": props}


def fetch_article(article: str, urls: list[str] | None = None, get=http_get) -> list[dict]:
    urls = urls if urls is not None else load_index(get=get)
    return [parse_card(get(u), u) for u in find_urls(article, urls)]


# ---------- поиск по коду товара ----------
CODES_CACHE = CACHE.with_name("cnc1_codes.json")   # {код: [url, ...]} — копится по мере просмотра карточек
# Проверено на сайте 22.10.2026: /catalog/?q=00059015 находит карточку, /catalog/?q=59015 и /search/?q=59015 — нет.
# Поэтому код ищется сначала полностью (8 знаков с нулями), затем без нулей.
SEARCH_URLS = ["/catalog/?q={q}", "/search/?q={q}"]
NAV_PROBE = "zzqx00000"    # заведомо пустой поиск: его ссылки — меню и подвал, они есть на любой странице
_nav = []


def _codes() -> dict:
    return json.loads(CODES_CACHE.read_text(encoding="utf-8")) if CODES_CACHE.exists() else {}


def _remember(card: dict):
    if card.get("code"):
        c = _codes()
        c.setdefault(card["code"], [])
        if card["url"] not in c[card["code"]]:
            c[card["code"]].append(card["url"])
        _save_json(CODES_CACHE, c)


def _catalog_links(html: str) -> list[str]:
    """Ссылки на /catalog/…/ глубже раздела, в порядке появления на странице, без повторов."""
    out = []
    for p in re.findall(r'href="(?:https?://(?:www\.)?cnc1\.ru)?(/catalog/[^"?#]+/)"', html):
        if p.count("/") >= 5 and p not in out:
            out.append(p)
    return out


def _result_links(html: str, get) -> list[str]:
    """Ссылки из результатов поиска: всё, чего нет на пустой странице поиска (меню, подвал)."""
    if not _nav:
        try:
            _nav.append(set(_catalog_links(get(BASE + SEARCH_URLS[0].format(q=NAV_PROBE)))))
        except Exception:  # noqa: BLE001 — без образца меню просто проверим больше ссылок
            _nav.append(set())
    return [p for p in _catalog_links(html) if p not in _nav[0]]


def is_code(q: str) -> bool:
    return bool(re.fullmatch(r"\d{5,8}", str(q).strip()))


def fetch_code(code: str, urls: list[str] | None = None, get=http_get, full_scan=False, progress=None) -> list[dict]:
    """Карточки по коду товара. Порядок: кэш кодов → поиск сайта → (по запросу) полный обход индекса URL.

    Найденная карточка принимается, только если «Код товара» на ней совпадает с искомым.
    """
    code = str(code).strip().zfill(8)
    found = []
    for u in _codes().get(code, []):
        c = parse_card(get(u), u)
        if c["code"] == code:
            found.append(c)
    if found:
        return found
    seen = set()
    for tpl in SEARCH_URLS:
        for q in dict.fromkeys((code, code.lstrip("0"))):
            try:
                html = get(BASE + tpl.format(q=q))
            except Exception:  # noqa: BLE001 — поиск может быть недоступен, идём дальше
                continue
            for path in [p for p in _result_links(html, get) if p not in seen][:20]:
                seen.add(path)
                c = parse_card(get(BASE + path), BASE + path)
                _remember(c)
                if c["code"] == code:
                    found.append(c)
            if found:
                return found
    if full_scan:
        urls = urls if urls is not None else load_index(get=get)
        known = {u for us in _codes().values() for u in us}
        todo = [u for u in urls if u not in known]
        for i, u in enumerate(todo, 1):
            c = parse_card(get(u), u)
            _remember(c)
            if progress:
                progress(i, len(todo))
            if c["code"] == code:
                found.append(c)
    return found


def fetch(query: str, **kw) -> list[dict]:
    """Один запрос: код товара (цифры) или артикул."""
    cards = fetch_code(query, **kw) if is_code(query) else fetch_article(query, kw.get("urls"), kw.get("get", http_get))
    for c in cards:
        _remember(c)
    return cards


def cards_to_rows(cards: list[dict]) -> tuple[list[str], list[dict]]:
    """Карточки → таблица в виде выгрузки (Артикул, _Код товара, Бренд, свойства…)."""
    headers = ["Артикул", "_Код товара", "Бренд"]
    rows = []
    for c in cards:
        r = {"Артикул": c["article"], "_Код товара": c["code"], "Бренд": c["brand"], "_URL": c["url"]}
        for k, v in c["props"].items():
            if k == "Бренд":
                continue
            if k not in headers:
                headers.append(k)
            r[k] = None if v in ("-", "—", "") else v
        rows.append(r)
    return headers, rows
