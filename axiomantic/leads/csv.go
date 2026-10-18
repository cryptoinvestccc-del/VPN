package main

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strings"
	"time"
)

// csvCell keeps a spreadsheet from running what a visitor typed: a cell
// that starts like a formula is turned into text.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (a *Admin) handleExport(w http.ResponseWriter, r *http.Request, ac *actor) {
	f, bad := parseFilter(r.URL.Query())
	if bad != "" {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: bad})
		return
	}
	list, _ := a.filtered(ac.ws.ID, f, r.URL.Query().Get("sort"))
	loc := ac.ws.loc
	if loc == nil {
		loc = time.UTC
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // Excel reads UTF-8 only with the mark
	cw := csv.NewWriter(&buf)
	cw.UseCRLF = true
	_ = cw.Write([]string{"№", "Получена", "Статус", "Имя", "Тип", "Источник", "Телефон", "Когда позвонить",
		"Канал", "Контакт", "Интересует", "Бюджет", "Сообщение", "Ответственный", "Изменена"})
	sources := map[string]string{"modal": "Окно заявки", "contacts": "Страница контактов"}
	for _, v := range list {
		l := v.Rec.Lead
		assignee := ""
		if p := personOf(ac.cfg, ac.ws.ID, v.Assignee); p != nil {
			assignee = p.Name
		}
		name := l.Name
		if v.Rec.Erased {
			name = "Данные удалены"
		}
		row := []string{v.Rec.ID, v.Rec.Time.In(loc).Format("2006-01-02 15:04"), statusLabels[v.Status], name,
			modeLabels[l.Mode], sources[l.Source], l.Phone, whenLabels[l.When], channelLabels[l.Channel], l.Contact,
			interestLabels[l.Interest], budgetLabels[l.Budget], l.Message, assignee, v.Updated.In(loc).Format("2006-01-02 15:04")}
		for i := range row {
			row[i] = csvCell(row[i])
		}
		_ = cw.Write(row)
	}
	cw.Flush()
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="leads-`+a.now().In(loc).Format("20060102")+`.csv"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
