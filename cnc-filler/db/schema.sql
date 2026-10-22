-- Заполнитель свойств CNC: схема базы (SQLite). См. docs/design.html
PRAGMA foreign_keys=ON;
CREATE TABLE brand(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, site_brand TEXT);           -- site_brand: какой бренд писать в 1С
CREATE TABLE brand_alias(alias TEXT PRIMARY KEY, brand_id INTEGER NOT NULL REFERENCES brand(id)); -- kkdaoku -> MetalCraft
CREATE TABLE catalog(id INTEGER PRIMARY KEY, code TEXT UNIQUE NOT NULL, brand_id INTEGER REFERENCES brand(id),
  title TEXT, year TEXT, file_name TEXT, pages INTEGER, is_scan INTEGER, page_rule TEXT);           -- page_rule: '(c+5)//2' и т.п.
CREATE TABLE page(id INTEGER PRIMARY KEY, catalog_id INTEGER NOT NULL REFERENCES catalog(id), pdf_page INTEGER NOT NULL,
  catalog_page INTEGER, ocr_text TEXT, UNIQUE(catalog_id,pdf_page));
CREATE VIRTUAL TABLE page_fts USING fts5(ocr_text, content='page', content_rowid='id');
CREATE TABLE product_group(id INTEGER PRIMARY KEY, code TEXT UNIQUE NOT NULL, name TEXT);       -- holder_bore, holder_thread, insert...
CREATE TABLE param(code TEXT PRIMARY KEY, name TEXT, unit TEXT);                                 -- d, h, b, L, L1, H, f, F, A, Dmin, LE, IC, S, DI, RE
CREATE TABLE series(id INTEGER PRIMARY KEY, catalog_id INTEGER NOT NULL REFERENCES catalog(id), group_id INTEGER REFERENCES product_group(id),
  code TEXT NOT NULL, page_id INTEGER REFERENCES page(id), attrs_json TEXT);                      -- углы, пластина, крепление
CREATE TABLE item(id INTEGER PRIMARY KEY, series_id INTEGER NOT NULL REFERENCES series(id), article_norm TEXT NOT NULL,
  article_raw TEXT, hand TEXT CHECK(hand IN ('R','L','N')), page_id INTEGER REFERENCES page(id), verified INTEGER DEFAULT 0);
CREATE INDEX item_article ON item(article_norm);
CREATE TABLE item_param(item_id INTEGER NOT NULL REFERENCES item(id), param_code TEXT NOT NULL REFERENCES param(code),
  value_num REAL, value_text TEXT, source TEXT CHECK(source IN ('catalog','article','formula')), note TEXT, PRIMARY KEY(item_id,param_code));
CREATE TABLE grade(id INTEGER PRIMARY KEY, brand_id INTEGER REFERENCES brand(id), code TEXT NOT NULL, iso TEXT, coating TEXT,
  composition TEXT, insert_type TEXT, site_material TEXT, page_id INTEGER REFERENCES page(id), UNIQUE(brand_id,code));
CREATE TABLE grade_avail(item_id INTEGER REFERENCES item(id), grade_id INTEGER REFERENCES grade(id), level TEXT CHECK(level IN ('recommended','available')), PRIMARY KEY(item_id,grade_id));
CREATE TABLE site_template(id INTEGER PRIMARY KEY, group_id INTEGER REFERENCES product_group(id), name TEXT, sample_file TEXT);
CREATE TABLE site_column(id INTEGER PRIMARY KEY, template_id INTEGER NOT NULL REFERENCES site_template(id), pos INTEGER,
  header TEXT NOT NULL, param_code TEXT REFERENCES param(code), fmt TEXT, const_value TEXT, dict TEXT);  -- fmt 'Ø{d}'
CREATE TABLE dict_value(dict TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(dict,value));
CREATE TABLE rule(id INTEGER PRIMARY KEY, group_id INTEGER REFERENCES product_group(id), kind TEXT, body TEXT);  -- 'length_letter', 'iso1832', 'formula'
CREATE TABLE conflict(id INTEGER PRIMARY KEY, article TEXT, code TEXT, column_header TEXT, site_value TEXT, catalog_value TEXT, source TEXT);
