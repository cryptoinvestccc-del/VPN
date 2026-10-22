# cnc-filler — Заполнитель свойств CNC

Сборка: GitHub Actions — `.github/workflows/build-cnc-filler.yml` в корне репозитория (вкладка Actions → прогон → Artifacts → `cnc-filler-exe`).
Каждый пуш в `cnc-filler/` прогоняет тесты, проверяет парсер на живом cnc1.ru и собирает `cnc-filler.exe`.

Локально:
1. `python -m venv .venv` и `.venv\Scripts\pip install -r requirements.txt pytest`
2. `python src/cncfiller/build_db.py`, затем `python -m pytest tests`
3. `python src/cncfiller/app.py` — окно программы.
PDF-каталоги кладутся в `catalogs/` (имена в `docs/catalogs.md`). Выгрузки Битрикса (`data/samples/`) и готовые файлы (`examples_output/`) в репозиторий не кладутся — он публичный.

Структура: `docs/` проект и каталоги · `db/schema.sql` · `data/seed/` сверенные таблицы · `data/index/` OCR-индекс 18 каталогов · `data/samples/` выгрузки Битрикса-образцы · `data/dicts/` справочники 1С · `legacy_scripts/` прежние скрипты · `examples_output/` готовые файлы загрузки · `src/cncfiller/` код (пишется на Этапе 1).
