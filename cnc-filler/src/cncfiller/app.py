"""Окно «Заполнитель свойств CNC» (PySide6). Макет — docs/design.html."""
import os
import re
import sys
from pathlib import Path

from PySide6.QtCore import Qt, QThread, Signal
from PySide6.QtGui import QColor
from PySide6.QtWidgets import (QApplication, QComboBox, QFileDialog, QHBoxLayout, QHeaderView, QLabel,
                               QLineEdit, QMainWindow, QMessageBox, QPushButton, QTableWidget, QTableWidgetItem,
                               QTextEdit, QVBoxLayout, QWidget)

sys.path.insert(0, str(Path(__file__).resolve().parent))
from engine import Engine, read_table, fill_rows, write_result, write_cards  # noqa: E402
import cnc1_parser  # noqa: E402


def resource(rel):
    base = getattr(sys, "_MEIPASS", Path(__file__).resolve().parents[2])
    return Path(base) / rel


ACCENT = "#0d6b5c"
STYLE = f"""
QWidget {{ font-family: 'Segoe UI'; font-size: 10pt; }}
QLineEdit {{ font-family: Consolas; font-size: 12pt; padding: 6px; border: 1px solid #c8d1cd; border-radius: 4px; }}
QPushButton {{ padding: 8px 12px; border-radius: 4px; }}
QPushButton#primary {{ background: {ACCENT}; color: white; font-weight: 600; border: 0; }}
QPushButton#ghost {{ color: {ACCENT}; border: 1.5px solid {ACCENT}; background: transparent; font-weight: 600; }}
QFrame#drop {{ border: 2px dashed {ACCENT}; border-radius: 8px; background: #eef7f4; }}
QLabel.caption {{ color: #5a6762; font-size: 8pt; font-weight: 600; }}
"""


class SiteWorker(QThread):
    """Скачивает карточки с cnc1.ru в фоне (не чаще 1 запроса в секунду)."""
    progress = Signal(str)
    done = Signal(list, list)

    def __init__(self, queries, full_scan=False):
        super().__init__()
        self.queries, self.full_scan = queries, full_scan

    def run(self):
        cards, missed = [], []
        try:
            urls = cnc1_parser.load_index()
        except Exception as ex:  # noqa: BLE001
            self.done.emit([], [f"Карта сайта не загрузилась: {ex}"])
            return
        for i, q in enumerate(self.queries, 1):
            self.progress.emit(f"cnc1.ru: {i}/{len(self.queries)} — {q}")
            try:
                got = cnc1_parser.fetch(q, urls=urls, full_scan=self.full_scan)
            except Exception as ex:  # noqa: BLE001
                got, missed = [], missed + [f"{q}: ошибка {ex}"]
            if got:
                cards += got
            else:
                missed.append(f"{q}: не найден на сайте")
        self.done.emit(cards, missed)


class Main(QMainWindow):
    def __init__(self, engine: Engine):
        super().__init__()
        self.engine = engine
        self.headers = self.result = None
        self.last = None   # 'site' — взято с cnc1.ru (сохраняем как есть) | 'fill' — заполнено из каталога
        n_cat = engine.con.execute("SELECT count(*) FROM catalog").fetchone()[0]
        n_items = engine.con.execute("SELECT count(*) FROM item").fetchone()[0]
        self.setWindowTitle(f"Заполнитель свойств CNC — {n_cat} каталогов, {n_items} разобранных позиций")
        self.resize(1200, 720)

        root = QWidget()
        h = QHBoxLayout(root)
        side = QVBoxLayout()
        h.addLayout(side, 0)

        # ---- ПАРСЕР CNC1.RU: свойства тянутся с сайта ----
        side.addWidget(self._cap("ПАРСЕР CNC1.RU"))
        self.p_field = QLineEdit()
        self.p_field.setPlaceholderText("артикул или код товара (через запятую)")
        self.p_field.returnPressed.connect(self.run_parser)
        side.addWidget(self.p_field)
        pf = QPushButton("Прикрепить файл со списком")
        pf.setObjectName("ghost")
        pf.clicked.connect(self.attach_parser_file)
        side.addWidget(pf)
        self.p_file_lbl = self._cap("")
        side.addWidget(self.p_file_lbl)
        pb = QPushButton("Спарсить → Excel")
        pb.setObjectName("primary")
        pb.clicked.connect(self.run_parser)
        side.addWidget(pb)

        side.addSpacing(16)
        # ---- ПОИСК ПО КАТАЛОГАМ: свойства из локальной базы ----
        side.addWidget(self._cap("ПОИСК ПО КАТАЛОГАМ"))
        self.c_field = QLineEdit()
        self.c_field.setPlaceholderText("артикул (или код) через запятую")
        self.c_field.returnPressed.connect(self.run_catalog)
        side.addWidget(self.c_field)
        cf = QPushButton("Прикрепить файл (выгрузка Битрикса)")
        cf.setObjectName("ghost")
        cf.clicked.connect(self.attach_catalog_file)
        side.addWidget(cf)
        self.c_file_lbl = self._cap("")
        side.addWidget(self.c_file_lbl)
        side.addWidget(self._cap("СОРТИРОВКА _SEO_SORT"))
        self.sort = QComboBox()
        self.sort.addItems(["Не менять", "по L, затем Dmin, шаг 10", "по d, затем L, шаг 10"])
        side.addWidget(self.sort)
        cb = QPushButton("Заполнить → Excel")
        cb.setObjectName("primary")
        cb.clicked.connect(self.run_catalog)
        side.addWidget(cb)

        side.addSpacing(16)
        self.save_btn = QPushButton("Сохранить Excel для 1С")
        self.save_btn.setObjectName("ghost")
        self.save_btn.clicked.connect(self.save)
        self.save_btn.setEnabled(False)
        side.addWidget(self.save_btn)
        side.addStretch(1)
        self.parser_file = self.catalog_file = None

        main = QVBoxLayout()
        h.addLayout(main, 1)
        self.stats = QLabel("Введите артикул или перетащите Excel-выгрузку из Битрикса/1С.")
        main.addWidget(self.stats)
        self.table = QTableWidget()
        self.table.setAlternatingRowColors(True)
        self.table.horizontalHeader().setSectionResizeMode(QHeaderView.ResizeToContents)
        self.table.itemSelectionChanged.connect(self.show_source)
        main.addWidget(self.table, 1)
        self.source = QTextEdit()
        self.source.setReadOnly(True)
        self.source.setMaximumHeight(130)
        main.addWidget(self.source)
        self.setCentralWidget(root)
        self.setStyleSheet(STYLE)
        self.path = None

    def _cap(self, t):
        l = QLabel(t)
        l.setProperty("class", "caption")
        return l

    # ----- ПАРСЕР CNC1.RU -----
    def _queries_from_file(self, path):
        """Список артикулов/кодов из файла: колонка _Код товара или Артикул, иначе первый непустой столбец."""
        headers, rows = read_table(path)
        qs = []
        for r in rows:
            q = r.get("_Код товара") or r.get("Артикул")
            if not q:
                for h in headers:
                    if r.get(h):
                        q = r.get(h)
                        break
            if q:
                qs.append(str(q).strip())
        return qs

    def attach_parser_file(self):
        path, _ = QFileDialog.getOpenFileName(self, "Файл со списком артикулов/кодов", "", "Excel (*.xlsx *.xlsm)")
        if path:
            self.parser_file = path
            self.p_file_lbl.setText(f"файл: {Path(path).name}")

    def run_parser(self):
        text = self.p_field.text().strip()
        if text:
            queries = [q for q in re.split(r"[,;\s]+", text) if q]
        elif self.parser_file:
            try:
                queries = self._queries_from_file(self.parser_file)
            except Exception as ex:  # noqa: BLE001
                QMessageBox.warning(self, "Файл не прочитан", str(ex))
                return
        else:
            QMessageBox.information(self, "Нечего парсить", "Введите артикул/код или прикрепите файл со списком.")
            return
        if not queries:
            QMessageBox.information(self, "Пусто", "В файле не нашлось артикулов/кодов.")
            return
        full = QMessageBox.question(self, "Поиск по коду",
                                    "Если код не найдётся поиском сайта — обойти все карточки сайта? Это долго (1 карточка в секунду), "
                                    "зато потом коды будут в кэше.") == QMessageBox.Yes if any(cnc1_parser.is_code(q) for q in queries) else False
        self.stats.setText(f"Парсю cnc1.ru: {len(queries)} запрос(ов)…")
        self.worker = SiteWorker(queries, full)
        self.worker.progress.connect(self.stats.setText)
        self.worker.done.connect(self.site_done)
        self.worker.start()

    def site_done(self, cards, missed):
        if not cards:
            self.stats.setText("С сайта ничего не получено.")
            self.source.setPlainText("\n".join(missed))
            return
        self.headers, self.rows = cnc1_parser.cards_to_rows(cards)
        self.path = str(Path.home() / "cnc1_выгрузка.xlsx")
        self.last = "site"
        self.result = None
        self._show_rows(self.headers, self.rows)
        self.save_btn.setEnabled(True)
        self.stats.setText(f"Спарсено с cnc1.ru: {len(cards)} карточек. Нажмите «Сохранить Excel для 1С».")
        self.source.setPlainText("\n".join([c["url"] for c in cards] + missed))

    # ----- ПОИСК ПО КАТАЛОГАМ -----
    def attach_catalog_file(self):
        path, _ = QFileDialog.getOpenFileName(self, "Выгрузка Битрикса/1С", "", "Excel (*.xlsx *.xlsm)")
        if path:
            self.catalog_file = path
            self.c_file_lbl.setText(f"файл: {Path(path).name}")

    def run_catalog(self):
        text = self.c_field.text().strip()
        if text:
            qs = [q for q in re.split(r"[,;\s]+", text) if q]
            self.headers = ["Артикул", "_Код товара"]
            self.rows = [{"Артикул": "" if cnc1_parser.is_code(q) else q,
                          "_Код товара": q if cnc1_parser.is_code(q) else ""} for q in qs]
        elif self.catalog_file:
            try:
                self.headers, self.rows = read_table(self.catalog_file)
            except Exception as ex:  # noqa: BLE001
                QMessageBox.warning(self, "Файл не прочитан", str(ex))
                return
            self.path = self.catalog_file
        else:
            QMessageBox.information(self, "Нечего заполнять", "Введите артикул или прикрепите файл-выгрузку.")
            return
        if text:
            self.path = str(Path.home() / "каталог_выгрузка.xlsx")
        self.fill()

    def _show_rows(self, headers, rows):
        """Показать карточки с сайта в таблице (без заполнения)."""
        cols = ["Артикул", "_Код товара"] + [h for h in headers if h not in ("Артикул", "_Код товара", "_URL")]
        self.table.clear()
        self.table.setColumnCount(len(cols))
        self.table.setHorizontalHeaderLabels(cols)
        self.table.setRowCount(len(rows))
        for i, r in enumerate(rows):
            for j, c in enumerate(cols):
                v = r.get(c)
                self.table.setItem(i, j, QTableWidgetItem("" if v is None else str(v)))

    def fill(self):
        if not self.headers:
            QMessageBox.information(self, "Нет файла", "Сначала перетащите Excel-выгрузку.")
            return
        sort_by = {1: "L, мм", 2: "d, мм"}.get(self.sort.currentIndex())
        self.sort_by = sort_by
        self.last = "fill"
        self.result = fill_rows(self.engine, self.headers, self.rows, sort_by)
        r = self.result
        self.stats.setText(f"Найдено в каталогах: <b>{r.found}</b> · заполнено ячеек: <b>{r.filled_cells}</b> · "
                           f"расхождений: <b>{len(r.conflicts)}</b> · с пустыми колонками: <b>{len(r.not_found)}</b>")
        cols = ["Артикул", "_Код товара"] + [h for h in self.headers if h and h not in ("Артикул", "_Код товара")
                                            and any(h in x["filled"] for x in r.rows)]
        if sort_by:
            cols.append("_SEO_Sort")
        self.table.clear()
        self.table.setColumnCount(len(cols))
        self.table.setHorizontalHeaderLabels(cols)
        self.table.setRowCount(len(r.rows))
        for i, x in enumerate(r.rows):
            for j, c in enumerate(cols):
                v = x["values"].get(c)
                it = QTableWidgetItem("" if v is None else str(v))
                if c in x["filled"]:
                    it.setBackground(QColor("#FFF2A8"))
                elif c in x["changed"]:
                    it.setBackground(QColor("#D9E8FB"))
                self.table.setItem(i, j, it)
        self.save_btn.setEnabled(True)

    def show_source(self):
        if not self.result:
            return
        items = self.table.selectedItems()
        if not items:
            return
        art = self.table.item(items[0].row(), 0).text()
        lines = [f"{s[2]} = {s[3]} ← {s[4]}" for s in self.result.sources if s[0] == art]
        lines += [f"Расхождение: {c[2]} на сайте {c[3]}, в каталоге {c[4]}" for c in self.result.conflicts if c[0] == art]
        lines += [f"Не заполнено: {n[2]}" for n in self.result.not_found if n[0] == art]
        self.source.setPlainText("\n".join(lines) or "Для этой строки ничего не менялось.")

    def save(self):
        if self.last == "site":
            suffix, ok = "_сайт", bool(self.rows)
        else:
            suffix, ok = "_заполнено", bool(self.result)
        if not ok:
            return
        default = str(Path(self.path).with_name(Path(self.path).stem + suffix + ".xlsx"))
        path, _ = QFileDialog.getSaveFileName(self, "Сохранить", default, "Excel (*.xlsx)")
        if not path:
            return
        if self.last == "site":
            write_cards(self.headers, self.rows, path)   # карточки сайта как есть
        else:
            write_result(self.result, self.headers, path, self.sort_by)
        self.stats.setText(f"Сохранено: {path}")


def main():
    db = resource("data/catalog.db")
    if not db.exists():
        app = QApplication(sys.argv)
        QMessageBox.critical(None, "Нет базы", f"Не найден файл базы {db}. Запустите build_db.py.")
        return 1
    app = QApplication(sys.argv)
    w = Main(Engine(str(db)))
    w.show()
    if os.environ.get("CNCFILLER_SMOKE"):
        return 0
    return app.exec()


if __name__ == "__main__":
    sys.exit(main())
