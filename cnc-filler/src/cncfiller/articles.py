"""Нормализация и расшифровка артикулов державок."""
import re

LENGTH_LETTER = {"F": 80, "G": 90, "H": 100, "J": 110, "K": 125, "L": 140, "M": 150, "N": 160,
                 "P": 170, "Q": 180, "R": 200, "S": 250, "T": 300, "U": 350, "V": 400}


def norm(article: str) -> str:
    """S40T-MSKNR12, 's40t msknr 12' → S40TMSKNR12."""
    return re.sub(r"[^A-Z0-9]", "", str(article).upper())


def expand_pattern(pattern: str):
    """Шаблон из seed → список (артикул, исполнение).

    'S20R-MCKNR/L12' → S20R-MCKNR12 (R), S20R-MCKNL12 (L)
    'S16Q-SVQR/L_11' и 'S16Q-SVU_11' → варианты B и C (SVUBR11, SVUCR11 …)
    'S20R-SCLC09H' → S20R-SCLCR09H, S20R-SCLCL09H
    'SNR/L0008K08' → SNR0008K08, SNL0008K08
    """
    p = pattern.strip()
    m = re.match(r"^([A-Z]\d{2}[A-Z])-(\d{2})-(.+)$", p)   # 'S07K-08-SCLCR/L06' → 'S07K-SCLCR/L06-08'
    if m:
        p = f"{m.group(1)}-{m.group(3)}-{m.group(2)}"
    if "R/L" in p:
        variants = [(p.replace("R/L", "R", 1), "R"), (p.replace("R/L", "L", 1), "L")]
    else:
        m = re.match(r"^(.*-)?([A-Z_]+?)(\d.*)$", p)
        if not m:
            return [(p, "N")]
        pre, letters, tail = m.group(1) or "", m.group(2), m.group(3)
        variants = [(f"{pre}{letters}R{tail}", "R"), (f"{pre}{letters}L{tail}", "L")]
    out = []
    for art, hand in variants:
        if "_" in art:
            # '_' в серии SV: пластина VB (B) или VC (C); буква B/C стоит перед R/L
            for bc in ("B", "C"):
                a = art.replace(f"{hand}_", f"{bc}{hand}", 1).replace(f"_{hand}", f"{bc}{hand}", 1)
                out.append((a, hand))
        else:
            out.append((art, hand))
    return out


_HOLDER = re.compile(r"^([A-Z])(\d{2})([A-Z])-([A-Z]{3,5})([RL])(\d{2})")
_THREAD = re.compile(r"^SN([RL])(\d{4})([A-Z])(\d{2})")


def decode(article: str) -> dict:
    """Что можно сказать по самому артикулу (без каталога)."""
    a = str(article).strip().upper()
    out = {}
    m = _HOLDER.match(a)
    if m:
        kind, d, letter, series, hand, size = m.groups()
        out.update({"d": int(d), "hand": hand, "series": series, "insert_size": size, "bar_type": kind})
        if letter in LENGTH_LETTER:
            out["L"] = LENGTH_LETTER[letter]
        return out
    m = _THREAD.match(a)
    if m:
        hand, dd, letter, size = m.groups()
        out.update({"d": int(dd), "hand": hand, "series": "SN", "insert_size": size})
        if letter in LENGTH_LETTER:
            out["L"] = LENGTH_LETTER[letter]
    return out
