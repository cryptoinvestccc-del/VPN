package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Schema describes what the panel may change in a site's content: sections
// of fields, each with a type and limits. The panel draws its forms from it
// and the server checks every save against it, so whatever reaches the
// site generator has the shape and the characters the generator expects.
// Each site the studio builds ships its own schema.
type Schema struct {
	Version  int       `json:"version"`
	Site     string    `json:"site"`
	Groups   []Group   `json:"groups"`
	Sections []Section `json:"sections"`
}

type Group struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Section struct {
	ID     string   `json:"id"`
	Group  string   `json:"group"`
	Title  string   `json:"title"`
	Where  string   `json:"where"`
	Hint   string   `json:"hint"`
	Icon   string   `json:"icon"`
	Fields []*Field `json:"fields"`
}

// Field is one value. Key is a content name ("HERO"), a path into one
// ("SITE.operator") at the top of a section, or a key of an object.
type Field struct {
	Key         string   `json:"key,omitempty"`
	Label       string   `json:"label,omitempty"`
	Hint        string   `json:"hint,omitempty"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	ReadOnly    bool     `json:"readonly,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	Unique      bool     `json:"unique,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Integer     bool     `json:"integer,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Options     [][2]any `json:"options,omitempty"`
	OptionsFrom string   `json:"optionsFrom,omitempty"`
	Fixed       bool     `json:"fixed,omitempty"`
	FixedKeys   bool     `json:"fixedKeys,omitempty"`
	Icon        bool     `json:"icon,omitempty"` // an image uploaded as a square icon (favicon)
	ItemTitle   any      `json:"itemTitle,omitempty"`
	Item        *Field   `json:"item,omitempty"`
	Items       []*Field `json:"items,omitempty"`
	Fields      []*Field `json:"fields,omitempty"`

	re *regexp.Regexp
}

var fieldTypes = map[string]bool{
	"text": true, "textarea": true, "number": true, "toggle": true, "select": true, "color": true, "date": true,
	"url": true, "email": true, "image": true, "list": true, "tuple": true, "object": true, "map": true, "blocks": true,
}

func LoadSchema(path string) (*Schema, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("schema %s: %v", path, err)
	}
	seen := map[string]bool{}
	for i := range s.Sections {
		sec := &s.Sections[i]
		if sec.ID == "" || seen[sec.ID] {
			return nil, fmt.Errorf("schema: section %q missing or repeated", sec.ID)
		}
		seen[sec.ID] = true
		for _, f := range sec.Fields {
			if f.Key == "" {
				return nil, fmt.Errorf("schema: section %s has a field without a key", sec.ID)
			}
			if err := f.prepare(); err != nil {
				return nil, fmt.Errorf("schema: %s.%s: %v", sec.ID, f.Key, err)
			}
		}
	}
	return &s, nil
}

func (f *Field) prepare() error {
	if !fieldTypes[f.Type] {
		return fmt.Errorf("unknown type %q", f.Type)
	}
	if f.Pattern != "" {
		re, err := regexp.Compile(f.Pattern)
		if err != nil {
			return err
		}
		f.re = re
	}
	for _, c := range append(append(append([]*Field{}, f.Items...), f.Fields...), f.Item) {
		if c != nil {
			if err := c.prepare(); err != nil {
				return err
			}
		}
	}
	if (f.Type == "list" || f.Type == "map") && f.Item == nil {
		return fmt.Errorf("%s needs an item", f.Type)
	}
	return nil
}

func (s *Schema) Section(id string) *Section {
	for i := range s.Sections {
		if s.Sections[i].ID == id {
			return &s.Sections[i]
		}
	}
	return nil
}

// FieldError says which value is wrong and why, in words for the editor.
type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string { return e.Path + ": " + e.Message }

// validator checks one save. full is the whole content as it would be
// after the save (options such as «a case» come from it); cur is the value
// being replaced, for read-only parts and fixed lists.
type validator struct {
	full  map[string]any
	media func(string) bool
}

var (
	reColor   = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	reMediaID = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// textOK refuses control characters (line breaks only in long text) and
// bidi controls, which could reorder what visitors read around them.
func textOK(s string, multiline bool) bool {
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			if !multiline {
				return false
			}
		case unicode.IsControl(r), r == ' ', r == ' ',
			r >= '‪' && r <= '‮', r >= '⁦' && r <= '⁩':
			return false
		}
	}
	return true
}

func (v *validator) check(f *Field, path string, val, cur any) (any, error) {
	bad := func(msg string, a ...any) (any, error) {
		return nil, &FieldError{Path: path, Message: fmt.Sprintf(msg, a...)}
	}
	if f.ReadOnly {
		if !jsonEqual(val, cur) {
			return bad("это поле не меняется")
		}
		return cur, nil
	}
	switch f.Type {
	case "text", "textarea", "url", "email", "date", "color", "select", "image":
		s, ok := val.(string)
		if !ok {
			if val == nil && !f.Required {
				return "", nil
			}
			return bad("нужен текст")
		}
		if f.Type != "textarea" {
			s = strings.TrimSpace(s)
		}
		if !textOK(s, f.Type == "textarea") {
			return bad("недопустимые символы")
		}
		n := utf8.RuneCountInString(s)
		if s == "" {
			if f.Required {
				return bad("заполните поле")
			}
			return "", nil
		}
		max := 300.0
		if f.Type == "textarea" {
			max = 4000
		}
		if f.Max != nil {
			max = *f.Max
		}
		if float64(n) > max {
			return bad("не длиннее %d символов", int(max))
		}
		if f.Min != nil && float64(n) < *f.Min {
			return bad("не короче %d символов", int(*f.Min))
		}
		if f.re != nil && !f.re.MatchString(s) {
			return bad("не подходит формат")
		}
		switch f.Type {
		case "url":
			if s != "#" {
				u, err := url.Parse(s)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return bad("нужна ссылка вида https://…")
				}
			}
		case "email":
			if !reEmail.MatchString(s) {
				return bad("нужна почта вида name@domain.ru")
			}
		case "date":
			if _, err := time.Parse("2006-01-02", s); err != nil {
				return bad("нужна дата ГГГГ-ММ-ДД")
			}
		case "color":
			if !reColor.MatchString(s) {
				return bad("нужен цвет вида #3341FF")
			}
		case "image":
			if !reMediaID.MatchString(s) || v.media == nil || !v.media(s) {
				return bad("картинка не найдена — загрузите её снова")
			}
		case "select":
			if !v.optionOK(f, s) {
				return bad("выберите вариант из списка")
			}
		}
		return s, nil
	case "number":
		n, ok := val.(float64)
		if !ok {
			if val == nil && !f.Required {
				return nil, nil
			}
			return bad("нужно число")
		}
		if f.Integer && n != float64(int64(n)) {
			return bad("нужно целое число")
		}
		if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return bad("число вне допустимых границ")
		}
		return n, nil
	case "toggle":
		b, ok := val.(bool)
		if !ok {
			return bad("нужно да или нет")
		}
		return b, nil
	case "tuple":
		arr, ok := val.([]any)
		if !ok || len(arr) != len(f.Items) {
			return bad("неполное значение")
		}
		curArr, _ := cur.([]any)
		out := make([]any, len(arr))
		for i, it := range f.Items {
			var c any
			if i < len(curArr) {
				c = curArr[i]
			}
			r, err := v.check(it, fmt.Sprintf("%s[%d]", path, i), arr[i], c)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case "list", "blocks":
		arr, ok := val.([]any)
		if !ok {
			return bad("нужен список")
		}
		curArr, _ := cur.([]any)
		if f.Fixed && len(arr) != len(curArr) {
			return bad("в этом списке нельзя добавлять и удалять пункты")
		}
		if f.Min != nil && float64(len(arr)) < *f.Min {
			return bad("нужно хотя бы %d", int(*f.Min))
		}
		max := 200.0
		if f.Max != nil {
			max = *f.Max
		}
		if float64(len(arr)) > max {
			return bad("не больше %d", int(max))
		}
		out := make([]any, len(arr))
		for i := range arr {
			var c any
			if f.Fixed && i < len(curArr) {
				c = curArr[i]
			}
			p := fmt.Sprintf("%s[%d]", path, i)
			var r any
			var err error
			if f.Type == "blocks" {
				r, err = checkBlock(p, arr[i])
			} else {
				r, err = v.check(f.Item, p, arr[i], c)
			}
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		if f.Item != nil && f.Item.Type == "object" {
			for _, k := range f.Item.Fields {
				if !k.Unique {
					continue
				}
				seen := map[string]int{}
				for i, it := range out {
					s, _ := it.(map[string]any)[k.Key].(string)
					if j, dup := seen[s]; dup {
						return nil, &FieldError{Path: fmt.Sprintf("%s[%d].%s", path, i, k.Key), Message: fmt.Sprintf("повторяет пункт %d", j+1)}
					}
					seen[s] = i
				}
			}
		}
		return out, nil
	case "object":
		obj, ok := val.(map[string]any)
		if !ok {
			return bad("нужен набор полей")
		}
		curObj, _ := cur.(map[string]any)
		out := map[string]any{}
		known := map[string]bool{}
		for _, k := range f.Fields {
			known[k.Key] = true
			x, present := obj[k.Key]
			if !present {
				if c, had := curObj[k.Key]; had && (k.ReadOnly || k.Hidden) {
					out[k.Key] = c
					continue
				}
				if k.Required {
					return nil, &FieldError{Path: path + "." + k.Key, Message: "заполните поле"}
				}
				continue
			}
			r, err := v.check(k, path+"."+k.Key, x, curObj[k.Key])
			if err != nil {
				return nil, err
			}
			out[k.Key] = r
		}
		for k := range obj {
			if !known[k] {
				return nil, &FieldError{Path: path + "." + k, Message: "неизвестное поле"}
			}
		}
		return out, nil
	case "map":
		obj, ok := val.(map[string]any)
		if !ok {
			return bad("нужен набор полей")
		}
		curObj, _ := cur.(map[string]any)
		if f.FixedKeys {
			if len(obj) != len(curObj) {
				return bad("набор ключей не меняется")
			}
			for k := range obj {
				if _, ok := curObj[k]; !ok {
					return bad("набор ключей не меняется")
				}
			}
		}
		out := map[string]any{}
		for k, x := range obj {
			r, err := v.check(f.Item, path+"."+k, x, curObj[k])
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	}
	return bad("неизвестный тип поля")
}

// checkBlock is one piece of an article: a paragraph, a subheading or a
// list. The generator renders each as escaped text in its own tag.
func checkBlock(path string, val any) (any, error) {
	bad := func(msg string) (any, error) { return nil, &FieldError{Path: path, Message: msg} }
	arr, ok := val.([]any)
	if !ok || len(arr) != 2 {
		return bad("неполный блок")
	}
	kind, _ := arr[0].(string)
	switch kind {
	case "p", "h2", "quote":
		s, ok := arr[1].(string)
		max := 3000
		switch kind {
		case "h2":
			max = 160
		case "quote":
			max = 600
		}
		s = strings.TrimSpace(s)
		if !ok || s == "" || utf8.RuneCountInString(s) > max || !textOK(s, kind == "p") {
			return bad("пустой, слишком длинный или с недопустимыми символами текст")
		}
		return []any{kind, s}, nil
	case "ul":
		items, ok := arr[1].([]any)
		if !ok || len(items) == 0 || len(items) > 30 {
			return bad("в списке от 1 до 30 пунктов")
		}
		out := make([]any, len(items))
		for i, it := range items {
			s, ok := it.(string)
			s = strings.TrimSpace(s)
			if !ok || s == "" || utf8.RuneCountInString(s) > 400 || !textOK(s, false) {
				return nil, &FieldError{Path: fmt.Sprintf("%s[%d]", path, i), Message: "пустой или слишком длинный пункт"}
			}
			out[i] = s
		}
		return []any{"ul", out}, nil
	}
	return bad("блок — абзац, подзаголовок, список или цитата")
}

// optionOK checks a select against its options: fixed ones, the keys of a
// map ("CATEGORIES") or a field of a list ("CASES.slug") in the content.
func (v *validator) optionOK(f *Field, s string) bool {
	for _, o := range f.Options {
		if fmt.Sprint(o[0]) == s {
			return true
		}
	}
	if f.OptionsFrom == "" {
		return false
	}
	name, sub, _ := strings.Cut(f.OptionsFrom, ".")
	switch src := v.full[name].(type) {
	case map[string]any:
		if sub == "" {
			_, ok := src[s]
			return ok
		}
		// "BLOG.categories" inside an object: a map's keys, or the first
		// item of each pair in a list (which keeps its order)
		switch inner := src[sub].(type) {
		case map[string]any:
			_, ok := inner[s]
			return ok
		case []any:
			for _, it := range inner {
				if pair, ok := it.([]any); ok && len(pair) > 0 && pair[0] == s {
					return true
				}
			}
		}
		return false
	case []any:
		for _, it := range src {
			if m, ok := it.(map[string]any); ok && m[sub] == s {
				return true
			}
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// getPath and setPath reach "SITE.operator" style keys in the content.
func getPath(c map[string]any, key string) any {
	top, sub, nested := strings.Cut(key, ".")
	if !nested {
		return c[top]
	}
	m, _ := c[top].(map[string]any)
	return m[sub]
}

func setPath(c map[string]any, key string, val any) {
	top, sub, nested := strings.Cut(key, ".")
	if !nested {
		c[top] = val
		return
	}
	m, _ := c[top].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	m[sub] = val
	c[top] = m
}

// ApplySection checks a section's new values against the schema and
// returns the content with them in place. Keys of the section that are
// not sent stay as they are; anything else sent is refused. Parts of an
// object that other sections edit ("SITE.operator") are kept.
func (s *Schema) ApplySection(content map[string]any, secID string, values map[string]any, media func(string) bool) (map[string]any, error) {
	sec := s.Section(secID)
	if sec == nil {
		return nil, &FieldError{Path: secID, Message: "нет такого раздела"}
	}
	next := deepCopy(content).(map[string]any)
	byKey := map[string]*Field{}
	for _, f := range sec.Fields {
		byKey[f.Key] = f
	}
	for k := range values {
		if byKey[k] == nil {
			return nil, &FieldError{Path: k, Message: "поле не из этого раздела"}
		}
	}
	// Options come from the content after the save, so a review can point
	// at a case renamed in the same save.
	for k, val := range values {
		setPath(next, k, deepCopy(val))
	}
	v := &validator{full: next, media: media}
	for _, f := range sec.Fields {
		val, sent := values[f.Key]
		if !sent {
			continue
		}
		cur := getPath(content, f.Key)
		out, err := v.check(f, f.Key, val, cur)
		if err != nil {
			return nil, err
		}
		if f.Type == "object" {
			// keep keys of this object that belong to other sections
			if curObj, ok := cur.(map[string]any); ok {
				merged := out.(map[string]any)
				for k, x := range curObj {
					if _, set := merged[k]; !set {
						merged[k] = x
					}
				}
			}
		}
		setPath(next, f.Key, out)
	}
	// a select elsewhere may point at something this save removed
	if err := s.checkRefs(next, media); err != nil {
		return nil, err
	}
	return next, nil
}

// checkRefs re-checks every select in the content, so removing a case
// that a review points at is refused with a clear message.
func (s *Schema) checkRefs(content map[string]any, media func(string) bool) error {
	v := &validator{full: content, media: media}
	for _, sec := range s.Sections {
		for _, f := range sec.Fields {
			if err := v.walkSelects(f, f.Key, getPath(content, f.Key)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *validator) walkSelects(f *Field, path string, val any) error {
	switch f.Type {
	case "select":
		if s, ok := val.(string); ok && s != "" && !v.optionOK(f, s) {
			return &FieldError{Path: path, Message: "ссылается на то, чего больше нет"}
		}
	case "tuple":
		arr, _ := val.([]any)
		for i, it := range f.Items {
			if i < len(arr) {
				if err := v.walkSelects(it, fmt.Sprintf("%s[%d]", path, i), arr[i]); err != nil {
					return err
				}
			}
		}
	case "list":
		arr, _ := val.([]any)
		for i, x := range arr {
			if err := v.walkSelects(f.Item, fmt.Sprintf("%s[%d]", path, i), x); err != nil {
				return err
			}
		}
	case "object":
		m, _ := val.(map[string]any)
		for _, k := range f.Fields {
			if err := v.walkSelects(k, path+"."+k.Key, m[k.Key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, y := range x {
			m[k] = deepCopy(y)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, y := range x {
			a[i] = deepCopy(y)
		}
		return a
	}
	return v
}
