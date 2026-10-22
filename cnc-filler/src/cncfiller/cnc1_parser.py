"""Парсер карточек товаров с cnc1.ru по артикулу.

Как работает:
1. Индекс URL товаров берётся из карты сайта (sitemap.xml → sitemap-iblock-*.xml) и кэшируется в data/cnc1_urls.json.
   В URL карточки есть артикул: .../reztsy_rastochnye/s_sckcr_s_sckcl/s12m_sckcr06_rezets_tokarnyy.../
2. Артикул ищется по совпадению начала «слага» (S12M-SCKCR06 → s12m_sckcr06…). Один артикул может иметь
   несколько карточек (дубли с разными кодами) — возвращаются все.
3. Карточка разбирается: заголовок, «Код товара», бренд, все характеристики (шаблон сайта Aspro Max).

Сайт — собственный сайт магазина; запросы идут не чаще 1 в секунду, без авторизации, только чтение.
Разметку карточки надо один раз сверить на живом сайте (tests/fixtures/cnc1_card.html) — см. CLAUDE.md.
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
    """Папка кэша для записи. В собранном exe (onedir/onefile) каталог приложения только для чтения,
    поэтому кэш кладём в %LOCALAPPDATA%\\cnc-filler (или домой). В обычном запуске — data/ проекта."""
    if getattr(sys, "frozen", False):
        base = os.environ.get("LOCALAPPDATA") or os.environ.get("APPDATA") or str(Path.home())
        d = Path(base) / "cnc-filler"
    else:
        d = Path(__file__).resolve().parents[2] / "data"
    d.mkdir(parents=True, exist_ok=True)
    return d


CACHE = _cache_dir() / "cnc1_urls.json"
_last = [0.0]


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
    try:
        CACHE.write_text(json.dumps(urls, ensure_ascii=False), encoding="utf-8")
    except OSError:
        pass   # кэш не критичен — при неудаче просто не сохраняем
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
class _Card(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.stack = []
        self.h1 = []
        self.in_h1 = False
        self.props = []          # (name, value)
        self.cur = None          # 'name'|'value'
        self.buf = []
        self.pending_name = None
        self.text = []

    def handle_starttag(self, tag, attrs):
        cls = dict(attrs).get("class", "") or ""
        self.stack.append((tag, cls))
        if tag == "h1":
            self.in_h1 = True
        if ("char_name" in cls or "properties__title" in cls or "props_item_name" in cls) and self.cur is None:
            self.cur, self.buf = "name", []
        elif ("char_value" in cls or "properties__value" in cls or "props_item_value" in cls) and self.cur is None:
            self.cur, self.buf = "value", []

    def handle_endtag(self, tag):
        if tag == "h1":
            self.in_h1 = False
        if self.stack:
            t, cls = self.stack.pop()
            if self.cur and t == tag and any(k in cls for k in ("char_name", "properties__title", "props_item_name",
                                                                 "char_value", "properties__value", "props_item_value")):
                txt = re.sub(r"\s+", " ", "".join(self.buf)).strip().rstrip(":")
                if self.cur == "name":
                    self.pending_name = txt
                elif self.pending_name:
                    self.props.append((self.pending_name, txt))
                    self.pending_name = None
                self.cur = None

    def handle_data(self, data):
        if self.in_h1:
            self.h1.append(data)
        if self.cur:
            self.buf.append(data)
        self.text.append(data)


def parse_card(html: str, url: str = "") -> dict:
    p = _Card()
    p.feed(html)
    text = re.sub(r"\s+", " ", unescape(" ".join(p.text)))
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
SEARCH_URLS = ["/catalog/?q={q}", "/search/?q={q}"]  # поиск сайта (Aspro Max); сверить на живом сайте


def _codes() -> dict:
    return json.loads(CODES_CACHE.read_text(encoding="utf-8")) if CODES_CACHE.exists() else {}


def _remember(card: dict):
    if card.get("code"):
        c = _codes()
        c.setdefault(card["code"], [])
        if card["url"] not in c[card["code"]]:
            c[card["code"]].append(card["url"])
        try:
            CODES_CACHE.write_text(json.dumps(c, ensure_ascii=False), encoding="utf-8")
        except OSError:
            pass


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
    for tpl in SEARCH_URLS:
        try:
            html = get(BASE + tpl.format(q=code))  # сайт ищет код только с ведущими нулями
        except Exception:  # noqa: BLE001 — поиск может быть недоступен, идём дальше
            continue
        links = sorted(set(re.findall(r'href="(/catalog/[^"?#]+/)"', html)))
        for path in [p for p in links if p.count("/") >= 5][:20]:
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
