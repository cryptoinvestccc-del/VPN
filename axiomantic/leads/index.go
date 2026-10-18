package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Lead statuses in the order a deal moves through them.
var statusOrder = []string{"new", "contacted", "proposal", "won", "lost", "spam"}

var statusLabels = map[string]string{
	"new": "Новая", "contacted": "Связались", "proposal": "КП отправлено",
	"won": "Договор", "lost": "Отказ", "spam": "Спам",
}

func isOpen(status string) bool {
	return status == "new" || status == "contacted" || status == "proposal"
}

// leadState is a lead as the panel sees it: the stored record plus
// everything the team has done to it since.
type leadState struct {
	rec      Record
	status   string
	assignee string
	updated  time.Time
	answered time.Time // first time anyone acted on it; zero while untouched
	events   []Event   // oldest first
	notes    int
}

type feedItem struct {
	time  time.Time
	lead  *leadState
	event int // index into lead.events; -1 is the lead arriving
}

type wsIndex struct {
	byID  map[string]*leadState
	leads []*leadState // oldest first
	feed  []feedItem   // oldest first
}

// Index holds every workspace's leads in memory, rebuilt from the store
// file at start. Every read goes through one workspace's part of it, so
// a lead id from another workspace finds nothing.
type Index struct {
	mu        sync.RWMutex
	ws        map[string]*wsIndex
	defaultWS string
}

func NewIndex(defaultWS string) *Index {
	return &Index{ws: map[string]*wsIndex{}, defaultWS: defaultWS}
}

func (x *Index) part(ws string) *wsIndex {
	p := x.ws[ws]
	if p == nil {
		p = &wsIndex{byID: map[string]*leadState{}}
		x.ws[ws] = p
	}
	return p
}

func (x *Index) wsOf(s string) string {
	if s == "" {
		return x.defaultWS
	}
	return s
}

func (x *Index) AddRecord(r Record) {
	x.mu.Lock()
	defer x.mu.Unlock()
	p := x.part(x.wsOf(r.WS))
	if p.byID[r.ID] != nil {
		return
	}
	st := &leadState{rec: r, status: "new", updated: r.Time}
	p.byID[r.ID] = st
	p.leads = append(p.leads, st)
	if n := len(p.leads); n > 1 && p.leads[n-2].rec.Time.After(r.Time) {
		sort.SliceStable(p.leads, func(i, j int) bool { return p.leads[i].rec.Time.Before(p.leads[j].rec.Time) })
	}
	p.addFeed(feedItem{time: r.Time, lead: st, event: -1})
}

func (p *wsIndex) addFeed(f feedItem) {
	p.feed = append(p.feed, f)
	if n := len(p.feed); n > 1 && p.feed[n-2].time.After(f.time) {
		sort.SliceStable(p.feed, func(i, j int) bool { return p.feed[i].time.Before(p.feed[j].time) })
	}
}

// AddEvent applies one event. Events of unknown leads are ignored.
func (x *Index) AddEvent(e Event) {
	x.mu.Lock()
	defer x.mu.Unlock()
	p := x.part(x.wsOf(e.WS))
	st := p.byID[e.LeadID]
	if st == nil {
		return
	}
	st.events = append(st.events, e)
	switch e.Ev {
	case evStatus:
		st.status = e.To
	case evAssign:
		st.assignee = e.To
	case evNote:
		st.notes++
	case evErase:
		st.rec.Lead = erasedLead(st.rec.Lead)
		st.rec.Erased = true
		for i := range st.events {
			st.events[i].Text = ""
		}
	}
	if e.Ev != evErase && st.answered.IsZero() {
		st.answered = e.Time
	}
	if e.Time.After(st.updated) {
		st.updated = e.Time
	}
	p.addFeed(feedItem{time: e.Time, lead: st, event: len(st.events) - 1})
}

// LeadView is a consistent copy of one lead, safe to use after the lock
// is released.
type LeadView struct {
	Rec      Record
	Status   string
	Assignee string
	Updated  time.Time
	Answered time.Time
	Events   []Event
	Notes    int
}

func (st *leadState) view() LeadView {
	return LeadView{Rec: st.rec, Status: st.status, Assignee: st.assignee, Updated: st.updated,
		Answered: st.answered, Events: append([]Event(nil), st.events...), Notes: st.notes}
}

// Get finds a lead of this workspace only.
func (x *Index) Get(ws, id string) (LeadView, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	p := x.ws[ws]
	if p == nil {
		return LeadView{}, false
	}
	st := p.byID[id]
	if st == nil {
		return LeadView{}, false
	}
	return st.view(), true
}

// All returns every lead of a workspace, oldest first.
func (x *Index) All(ws string) []LeadView {
	x.mu.RLock()
	defer x.mu.RUnlock()
	p := x.ws[ws]
	if p == nil {
		return nil
	}
	out := make([]LeadView, len(p.leads))
	for i, st := range p.leads {
		out[i] = st.view()
	}
	return out
}

// FeedItem is one line of the activity feed.
type FeedItem struct {
	Time  time.Time
	Lead  LeadView
	Event *Event
}

// Feed returns the newest n things that happened in a workspace.
func (x *Index) Feed(ws string, n int) []FeedItem {
	x.mu.RLock()
	defer x.mu.RUnlock()
	p := x.ws[ws]
	if p == nil {
		return nil
	}
	var out []FeedItem
	for i := len(p.feed) - 1; i >= 0 && len(out) < n; i-- {
		f := p.feed[i]
		item := FeedItem{Time: f.time, Lead: f.lead.view()}
		if f.event >= 0 {
			e := f.lead.events[f.event]
			item.Event = &e
		}
		out = append(out, item)
	}
	return out
}

// matches is the free-text search of the leads list.
func (v *LeadView) matches(q string) bool {
	if q == "" {
		return true
	}
	l := v.Rec.Lead
	for _, s := range []string{l.Name, l.Phone, l.Contact, l.Message, v.Rec.ID} {
		if s != "" && strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	// "+7 (900) 123-45-67" should be found by "9001234567"
	if d := digitsOf(q); len(d) >= 4 && strings.Trim(q, "0123456789+()- ") == "" {
		for _, s := range []string{l.Phone, l.Contact} {
			if strings.Contains(digitsOf(s), d) {
				return true
			}
		}
	}
	return false
}

func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
