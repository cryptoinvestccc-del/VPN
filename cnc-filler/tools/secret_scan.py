"""Поиск секретов в исходниках и собранном exe (проверка безопасности №1 из CLAUDE.md).

Запуск: python tools/secret_scan.py src tools tests data dist/cnc-filler.exe
Exe PyInstaller распаковывается (архив PKG и вложенный PYZ), иначе сжатые модули не видны поиску.
Код выхода 1 — найдено совпадение; печатается файл и шаблон, сам секрет не печатается.
"""
import re
import sys
import zlib
from pathlib import Path

PATTERNS = {
    "OpenAI-style key sk-…": rb"sk-[A-Za-z0-9_-]{20,}",
    "Supabase service-role key": rb"service_rol[e]",   # [e] — чтобы сканер не находил сам себя
    "PEM private key": rb"-----BEGIN [A-Z ]*PRIVATE KEY-----\s*[A-Za-z0-9+/=\s]{64}",
    "GCP private_key_id": rb"\"private_key_id\"\s*:",
    "Telegram bot token": rb"\b\d{8,10}:AA[A-Za-z0-9_-]{30,}",
    "AWS access key": rb"\bAKIA[0-9A-Z]{16}\b",
    "GitHub token": rb"\bgh[pousr]_[A-Za-z0-9]{36,}",
    "Slack token": rb"\bxox[abprs]-[A-Za-z0-9-]{10,}",
}
SKIP_DIRS = {".git", ".venv", "__pycache__", "build"}


def blobs_from_exe(path: Path):
    from PyInstaller.archive.readers import CArchiveReader, PKG_ITEM_PYZ
    pkg = CArchiveReader(str(path))
    for name, entry in pkg.toc.items():
        if entry[-1] == PKG_ITEM_PYZ:
            pyz = pkg.open_embedded_archive(name)
            for mod in pyz.toc:
                data = pyz.extract(mod, raw=True) or b""
                try:
                    data = zlib.decompress(data)   # старые PyInstaller отдают сжатые данные
                except zlib.error:
                    pass                           # 6.x — уже распакованный marshal-код
                yield f"{path}!{name}!{mod}", data
        else:
            yield f"{path}!{name}", pkg.extract(name)


def blobs(path: Path):
    if path.is_dir():
        for p in sorted(path.rglob("*")):
            if p.is_file() and not SKIP_DIRS & set(p.parts):
                yield str(p), p.read_bytes()
    elif path.suffix.lower() == ".exe":
        yield str(path), path.read_bytes()
        yield from blobs_from_exe(path)
    elif path.is_file():
        yield str(path), path.read_bytes()


def main(args) -> int:
    found, n = [], 0
    for a in args:
        for name, data in blobs(Path(a)):
            n += 1
            for label, rx in PATTERNS.items():
                if re.search(rx, data):
                    found.append((name, label))
    for name, label in found:
        print(f"НАЙДЕНО: {label} — {name}")
    print(f"Проверено объектов: {n}; совпадений: {len(found)}")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:] or ["."]))
