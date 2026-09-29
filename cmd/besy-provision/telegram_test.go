package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/provision"
)

type staticPeers []provision.Peer

func (p staticPeers) Peers(context.Context) ([]provision.Peer, error) { return p, nil }

// fakeTelegram records what the bot sends.
type fakeTelegram struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (f *fakeTelegram) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			f.mu.Lock()
			f.sent = append(f.sent, m)
			f.mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeTelegram) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.sent {
		out = append(out, m["text"].(string))
	}
	return out
}

func testBot(t *testing.T, tg *fakeTelegram, peers staticPeers) *statsBot {
	t.Helper()
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")
	return &statsBot{
		cfg: &tgConfig{
			chats: map[int64]bool{42: true}, loc: loc,
			statePath: filepath.Join(t.TempDir(), "stats.json"),
		},
		device: peers, tally: &provision.Tally{}, capacity: 4000,
		api: tg.server(t).URL + "/botSECRET", client: http.DefaultClient,
		book: &provision.Book{},
	}
}

func TestStrangersLearnOnlyTheirChatID(t *testing.T) {
	tg := &fakeTelegram{}
	b := testBot(t, tg, nil)

	b.handle(context.Background(), 7, "/start")
	b.handle(context.Background(), 7, "/now")
	b.handle(context.Background(), 7, "/week")

	got := tg.texts()
	if len(got) != 1 || !strings.Contains(got[0], "7") || strings.Contains(got[0], "подключено") {
		t.Errorf("a stranger was sent %q", got)
	}
}

func TestNowCountsConnectedDevices(t *testing.T) {
	tg := &fakeTelegram{}
	now := time.Now()
	b := testBot(t, tg, staticPeers{
		{PublicKey: "A", LastHandshake: now.Add(-time.Minute)},
		{PublicKey: "B", LastHandshake: now.Add(-2 * time.Hour)},
		{PublicKey: "C"},
	})

	b.handle(context.Background(), 42, "/now")
	got := tg.texts()
	if len(got) != 1 || !strings.Contains(got[0], "Сейчас подключено: 1") ||
		!strings.Contains(got[0], "Активны за сутки: 2") || !strings.Contains(got[0], "3 из 4000") {
		t.Errorf("sent %q", got)
	}
	// Nothing identifying goes out.
	if strings.ContainsAny(got[0], "ABC") {
		t.Errorf("a key reached the message: %q", got[0])
	}
}

func TestTodayAndWeekReadTheBook(t *testing.T) {
	tg := &fakeTelegram{}
	b := testBot(t, tg, staticPeers{{PublicKey: "A", LastHandshake: time.Now()}})

	b.handle(context.Background(), 42, "/today")
	b.handle(context.Background(), 42, "/week")
	b.handle(context.Background(), 42, "/whatever")

	got := tg.texts()
	if len(got) != 3 {
		t.Fatalf("sent %d messages", len(got))
	}
	if !strings.Contains(got[0], "Пик одновременно: 1") {
		t.Errorf("today: %q", got[0])
	}
	if strings.Count(got[1], "\n") != 8 { // title, header, seven days
		t.Errorf("week: %q", got[1])
	}
	if !strings.Contains(got[2], "/now") {
		t.Errorf("unknown commands should get the help: %q", got[2])
	}
	if _, err := os.Stat(b.cfg.statePath); err != nil {
		t.Errorf("the day totals were not saved: %v", err)
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	b := &statsBot{api: "http://127.0.0.1:1/botSECRET-TOKEN", client: &http.Client{Timeout: time.Second}}
	err := b.call(context.Background(), "getUpdates", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("the error quotes the token: %v", err)
	}
}

func TestNextReport(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")      // UTC+5
	before := time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC) // 08:00 there
	if got := nextReport(before, loc, 9, 0); !got.Equal(time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("before nine: %v", got)
	}
	after := time.Date(2026, 9, 29, 5, 0, 0, 0, time.UTC) // 10:00 there
	if got := nextReport(after, loc, 9, 0); !got.Equal(time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("after nine: %v", got)
	}
}

func TestConfigFromEnv(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	_ = os.WriteFile(token, []byte("123:ABC\n"), 0o600)

	t.Setenv("BESY_TG_TOKEN_FILE", "")
	if c, err := tgConfigFromEnv(); c != nil || err != nil {
		t.Fatalf("unset should mean off, got %v %v", c, err)
	}

	t.Setenv("BESY_TG_TOKEN_FILE", token)
	t.Setenv("BESY_TG_CHATS", "42, -1001")
	t.Setenv("BESY_TG_REPORT_AT", "08:30")
	c, err := tgConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.token != "123:ABC" || !c.chats[42] || !c.chats[-1001] || c.reportH != 8 || c.reportM != 30 {
		t.Errorf("got %+v", c)
	}

	t.Setenv("BESY_TG_REPORT_AT", "25:00")
	if _, err := tgConfigFromEnv(); err == nil {
		t.Error("25:00 was accepted")
	}
	t.Setenv("BESY_TG_REPORT_AT", "")
	t.Setenv("BESY_TG_CHATS", "me")
	if _, err := tgConfigFromEnv(); err == nil {
		t.Error("a non-numeric chat id was accepted")
	}
}

func TestBytesRU(t *testing.T) {
	for n, want := range map[uint64]string{
		0: "0 Б", 1023: "1023 Б", 1536: "1,5 КБ", 5 << 30: "5,0 ГБ",
	} {
		if got := bytesRU(n); got != want {
			t.Errorf("bytesRU(%d) = %q, want %q", n, got, want)
		}
	}
}
