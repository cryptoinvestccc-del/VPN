package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Lead is one order form as the site sends it. The field set mirrors the
// forms in build.py; an unknown field is rejected rather than stored.
type Lead struct {
	Source   string `json:"source"`
	Mode     string `json:"mode"`
	Name     string `json:"name"`
	Phone    string `json:"phone,omitempty"`
	When     string `json:"when,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Contact  string `json:"contact,omitempty"`
	Message  string `json:"message,omitempty"`
	Interest string `json:"interest,omitempty"`
	Budget   string `json:"budget,omitempty"`
	Consent  bool   `json:"consent"`
	// Website is a field people never see. Anything in it came from a bot.
	Website string `json:"website,omitempty"`
}

// The same rules as main.js; the browser only checks them first.
var (
	reEmail    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]{2,}$`)
	reTelegram = regexp.MustCompile(`^@?[A-Za-z0-9_]{5,32}$`)
	rePhone    = regexp.MustCompile(`^[+\d\s()-]+$`)
)

var (
	whenLabels     = map[string]string{"hour": "в течение часа", "today": "сегодня", "tomorrow": "завтра"}
	channelLabels  = map[string]string{"telegram": "Telegram", "whatsapp": "WhatsApp", "email": "Email"}
	interestLabels = map[string]string{"landing": "Лендинг", "multi-base": "Многостаночник · Базовый", "multi-custom": "Многостаночник · Под вас", "unknown": "пока не знает"}
	budgetLabels   = map[string]string{"lt100": "до 100 000 ₽", "100-300": "100–300 000 ₽", "gt300": "от 300 000 ₽", "unknown": "пока не знает"}
)

func isPhone(s string) bool {
	if !rePhone.MatchString(s) {
		return false
	}
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n >= 10 && n <= 15
}

// plain reports whether s has no control characters; multiline also lets
// line breaks and tabs through, for the free-text message.
func plain(s string, multiline bool) bool {
	for _, r := range s {
		if multiline && (r == '\n' || r == '\r' || r == '\t') {
			continue
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return false
		}
	}
	return true
}

func between(s string, lo, hi int) bool {
	n := utf8.RuneCountInString(s)
	return n >= lo && n <= hi
}

func oneOf(s string, set map[string]string) bool {
	_, ok := set[s]
	return ok
}

// Normalize trims every field and clears the ones that do not belong to
// the chosen mode, so that what is stored is exactly what was validated.
func (l *Lead) Normalize() {
	for _, f := range []*string{&l.Source, &l.Mode, &l.Name, &l.Phone, &l.When, &l.Channel, &l.Contact, &l.Message, &l.Interest, &l.Budget, &l.Website} {
		*f = strings.TrimSpace(*f)
	}
	switch l.Mode {
	case "call":
		l.Channel, l.Contact, l.Message, l.Budget = "", "", "", ""
	case "write":
		l.Phone, l.When, l.Budget = "", "", ""
	case "form":
		l.Phone, l.When, l.Channel, l.Interest = "", "", "", ""
	}
}

// Validate returns the name of the first field that is wrong, or "".
func (l *Lead) Validate() string {
	switch {
	case l.Source == "modal" && (l.Mode == "call" || l.Mode == "write"):
	case l.Source == "contacts" && l.Mode == "form":
	default:
		return "mode"
	}
	if !between(l.Name, 2, 80) || !plain(l.Name, false) {
		return "name"
	}
	if !between(l.Message, 0, 3000) || !plain(l.Message, true) {
		return "message"
	}
	if l.Interest != "" && !oneOf(l.Interest, interestLabels) {
		return "interest"
	}
	switch l.Mode {
	case "call":
		if !isPhone(l.Phone) {
			return "phone"
		}
		if l.When != "" && !oneOf(l.When, whenLabels) {
			return "when"
		}
	case "write":
		ok := false
		switch l.Channel {
		case "telegram":
			ok = reTelegram.MatchString(l.Contact) || isPhone(l.Contact)
		case "whatsapp":
			ok = isPhone(l.Contact)
		case "email":
			ok = reEmail.MatchString(l.Contact)
		default:
			return "channel"
		}
		if !ok || !between(l.Contact, 1, 120) {
			return "contact"
		}
	case "form":
		if !between(l.Contact, 1, 120) || !(reEmail.MatchString(l.Contact) || isPhone(l.Contact)) {
			return "contact"
		}
		if !between(l.Message, 10, 3000) {
			return "message"
		}
		if l.Budget != "" && !oneOf(l.Budget, budgetLabels) {
			return "budget"
		}
	}
	if !l.Consent {
		return "consent"
	}
	return ""
}

// Text is the notification a manager reads. It is plain text — Telegram
// is not asked to parse markup, so nothing a visitor typed can format or
// link anything in the chat.
func (l *Lead) Text(id string) string {
	var b strings.Builder
	title := map[string]string{"call": "Позвонить", "write": "Написать", "form": "Форма на странице контактов"}[l.Mode]
	b.WriteString("Заявка с сайта — " + title + "\n\n")
	line := func(label, value string) {
		if value != "" {
			b.WriteString(label + ": " + value + "\n")
		}
	}
	line("Имя", l.Name)
	line("Телефон", l.Phone)
	line("Когда позвонить", whenLabels[l.When])
	if l.Channel != "" {
		line("Ответить в "+channelLabels[l.Channel], l.Contact)
	} else {
		line("Контакт", l.Contact)
	}
	line("Интересует", interestLabels[l.Interest])
	line("Бюджет", budgetLabels[l.Budget])
	if l.Message != "" {
		b.WriteString("\n" + l.Message + "\n")
	}
	b.WriteString("\n№ " + id)
	return b.String()
}
