"""Окно «Заполнитель свойств CNC» (PySide6). Макет — docs/design.html."""
import os
import re
import sys
from pathlib import Path

from PySide6.QtCore import Qt, QThread, Signal
from PySide6.QtGui import QColor, QFont
from PySide6.QtWidgets import (QApplication, QComboBox, QFileDialog, QFrame, QHBoxLayout, QHeaderView, QLabel,
                               QLineEdit, QMainWindow, QMessageBox, QPushButton, QTableWidget, QTableWidgetItem,
                               QTextEdit, QVBoxLayout, QWidget)

sys.path.insert(0, str(Path(__file__).resolve().parent))
from engine import Engine, read_table, fill_rows, write_result, fmt_num  # noqa: E402
from articles import decode  # noqa: E402
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


class DropZone(QFrame):
    def __init__(self, on_file):
        super().__init__()
        self.setObjectName("drop")
        self.on_file = on_file
        self.setAcceptDrops(True)
        lay = QVBoxLayout(self)
        self.label = QLabel("<b>Перетащите Excel</b><br>или нажмите, чтобы выбрать файл")
        self.label.setAlignment(Qt.AlignCenter)
        lay.addWidget(self.label)
        self.setMinimumHeight(90)

    def mousePressEvent(self, e):
        path, _ = QFileDialog.getOpenFileName(self, "Выберите выгрузку", "", "Excel (*.xlsx *.xlsm)")
        if path:
            self.on_file(path)

    def dragEnterEvent(self, e):
        if e.mimeData().hasUrls():
            e.acceptProposedAction()

    def dropEvent(self, e):
        for u in e.mimeData().urls():
            self.on_file(u.toLocalFile())
            break


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
        n_cat = engine.con.execute("SELECT count(*) FROM catalog").fetchone()[0]
        n_items = engine.con.execute("SELECT count(*) FROM item").fetchone()[0]
        self.setWindowTitle(f"Заполнитель свойств CNC — {n_cat} каталогов, {n_items} разобранных позиций")
        self.resize(1200, 720)

        root = QWidget()
        h = QHBoxLayout(root)
        side = QVBoxLayout()
        h.addLayout(side, 0)

        side.addWidget(self._cap("ОДИН АРТИКУЛ"))
        self.article = QLineEdit()
        self.article.setPlaceholderText("например S40T-MSKNR12")
        self.article.returnPressed.connect(self.lookup)
        side.addWidget(self.article)
        self.brand = QComboBox()
        self.brand.addItems(["Любой бренд", "MetalCraft", "Sant"])
        side.addWidget(self.brand)
        b = QPushButton("Найти")
        b.setObjectName("primary")
        b.clicked.connect(self.lookup)
        side.addWidget(b)

        site = QPushButton("Взять с cnc1.ru (артикул или код)")
        site.setObjectName("ghost")
        site.clicked.connect(self.from_site)
        side.addWidget(site)

        side.addSpacing(12)
        side.addWidget(self._cap("EXCEL-ВЫГРУЗКА"))
        self.drop = DropZone(self.load_file)
        side.addWidget(self.drop)
        side.addWidget(self._cap("СОРТИРОВКА _SEO_SORT"))
        self.sort = QComboBox()
        self.sort.addItems(["Не менять", "по L, затем Dmin, шаг 10", "по d, затем L, шаг 10"])
        side.addWidget(self.sort)
        self.fill_btn = QPushButton("Заполнить")
        self.fill_btn.setObjectName("primary")
        self.fill_btn.clicked.connect(self.fill)
        side.addWidget(self.fill_btn)
        self.save_btn = QPushButton("Сохранить Excel для 1С")
        self.save_btn.setObjectName("ghost")
        self.save_btn.clicked.connect(self.save)
        self.save_btn.setEnabled(False)
        side.addWidget(self.save_btn)
        side.addStretch(1)
        side_w = QWidget()

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

    # ----- один артикул -----
    def lookup(self):
        art = self.article.text().strip()
        if not art:
            return
        brand = None if self.brand.currentIndex() == 0 else self.brand.currentText()
        vals, src, m = self.engine.site_values(art, brand)
        self.table.clear()
        self.table.setColumnCount(3)
        self.table.setHorizontalHeaderLabels(["Колонка сайта", "Значение", "Источник"])
        self.table.setRowCount(len(vals))
        for i, (k, v) in enumerate(vals.items()):
            for j, x in enumerate((k, fmt_num(v) if isinstance(v, float) else v, src.get(k, ""))):
                it = QTableWidgetItem(str(x))
                if j == 1:
                    it.setBackground(QColor("#FFF2A8") if m else QColor("#D9E8FB"))
                self.table.setItem(i, j, it)
        if m:
            self.stats.setText(f"<b>{art}</b> найден: {m.source}")
            self.source.setPlainText("Параметры каталога: " + ", ".join(f"{k} = {fmt_num(v)}" for k, v in m.params.items()))
        else:
            hint = self.engine.series_hint(art)
            self.stats.setText(f"<b>{art}</b> нет в разобранной базе. Показано только то, что видно из артикула.")
            self.source.setPlainText("Серия встречается в каталогах: " + ("; ".join(hint) if hint else "нет") +
                                     "\nРасшифровка артикула: " + str(decode(art)))

    # ----- cnc1.ru -----
    def from_site(self):
        """Поле артикула (можно несколько через запятую/пробел) или колонка Артикул/_Код товара из загруженного Excel."""
        text = self.article.text().strip()
        if text:
            queries = [q for q in re.split(r"[,;\s]+", text) if q]
        elif self.headers:
            queries = []
            for r in self.rows:
                q = r.get("_Код товара") or r.get("Артикул")
                if q:
                    queries.append(str(q).strip())
        else:
            QMessageBox.information(self, "Нечего искать", "Введите артикул/код или загрузите Excel со списком.")
            return
        full = QMessageBox.question(self, "Поиск по коду",
                                    "Если код не найдётся поиском сайта — обойти все карточки сайта? Это долго (1 карточка в секунду), "
                                    "зато потом коды будут в кэше.") == QMessageBox.Yes if any(cnc1_parser.is_code(q) for q in queries) else False
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
        self.drop.label.setText(f"<b>Взято с cnc1.ru</b><br>{len(self.rows)} карточек")
        self.stats.setText(f"С сайта: {len(cards)} карточек. Нажмите «Заполнить».")
        self.source.setPlainText("\n".join([c["url"] for c in cards] + missed))

    # ----- Excel -----
    def load_file(self, path):
        try:
            self.headers, self.rows = read_table(path)
        except Exception as ex:  # noqa: BLE001
            QMessageBox.warning(self, "Файл не прочитан", str(ex))
            return
        self.path = path
        self.drop.label.setText(f"<b>{Path(path).name}</b><br>{len(self.rows)} строк")
        self.stats.setText("Файл загружен. Нажмите «Заполнить».")

    def fill(self):
        if not self.headers:
            QMessageBox.information(self, "Нет файла", "Сначала перетащите Excel-выгрузку.")
            return
        sort_by = {1: "L, мм", 2: "d, мм"}.get(self.sort.currentIndex())
        self.sort_by = sort_by
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
        if not self.result:
            return
        default = str(Path(self.path).with_name(Path(self.path).stem + "_заполнено.xlsx"))
        path, _ = QFileDialog.getSaveFileName(self, "Сохранить", default, "Excel (*.xlsx)")
        if path:
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
