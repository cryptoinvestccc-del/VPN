# cnc-filler — Заполнитель свойств CNC

Как продолжить в Claude Code:
1. Распакуйте архив в папку, например `C:\Projects\cnc-filler`.
2. Положите PDF-каталоги в `catalogs/` (имена в `docs/catalogs.md`).
3. Откройте папку в Claude Code и напишите: «Прочитай CLAUDE.md и начинай Этап 1 (MVP)».
4. Для сборки .exe: создайте репозиторий на GitHub, загрузите проект — `.github/workflows/build.yml` соберёт `cnc-filler.exe` (вкладка Actions → Artifacts).

Структура: `docs/` проект и каталоги · `db/schema.sql` · `data/seed/` сверенные таблицы · `data/index/` OCR-индекс 18 каталогов · `data/samples/` выгрузки Битрикса-образцы · `data/dicts/` справочники 1С · `legacy_scripts/` прежние скрипты · `examples_output/` готовые файлы загрузки · `src/cncfiller/` код (пишется на Этапе 1).
