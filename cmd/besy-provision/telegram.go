package main

// The operator's Telegram bot: totals on request and a morning report.
//
// It sends numbers and nothing else — how many devices are connected,
// how many keys were issued, how much traffic went through. No key, no
// address, nothing that says which device did what leaves the server,
// because none of it is ever put together (see internal/provision/
// stats.go).
//
// Configured from the environment, so it can be switched on with a
// systemd drop-in without touching the service's command line:
//
//	BESY_TG_TOKEN_FILE  file holding the bot token (required to enable)
//	BESY_TG_CHATS       chat ids allowed to use it, comma separated
//	BESY_TG_TZ          time zone for days and the report (Asia/Yekaterinburg)
//	BESY_TG_REPORT_AT   when to send yesterday's report, HH:MM (09:00; "off" to disable)
//	BESY_TG_STATE       where the day totals are kept (/var/lib/besy-provision/stats.json)
//
// The token is read from a file rather than the environment so that it
// does not show in `systemctl show`, and it never appears in the log:
// Go's HTTP errors quote the request URL, and the URL carries the token.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the server may have no zone database

	"github.com/cryptoinvestccc-del/vpn/internal/provision"
)

type tgConfig struct {
	token     string
	chats     map[int64]bool
	loc       *time.Location
	reportH   int
	reportM   int
	report    bool
	statePath string
}

// tgConfigFromEnv returns nil when the bot is not configured.
func tgConfigFromEnv() (*tgConfig, error) {
	tokenFile := strings.TrimSpace(os.Getenv("BESY_TG_TOKEN_FILE"))
	if tokenFile == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("reading the Telegram token: %w", err)
	}
	c := &tgConfig{
		token:     strings.TrimSpace(string(raw)),
		chats:     map[int64]bool{},
		report:    true,
		reportH:   9,
		statePath: "/var/lib/besy-provision/stats.json",
	}
	if c.token == "" || strings.ContainsAny(c.token, " /\n") {
		return nil, errors.New("the Telegram token file is empty or malformed")
	}
	for _, s := range strings.Split(os.Getenv("BESY_TG_CHATS"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("BESY_TG_CHATS: %q is not a chat id", s)
		}
		c.chats[id] = true
	}
	zone := strings.TrimSpace(os.Getenv("BESY_TG_TZ"))
	if zone == "" {
		zone = "Asia/Yekaterinburg"
	}
	if c.loc, err = time.LoadLocation(zone); err != nil {
		return nil, fmt.Errorf("BESY_TG_TZ: %w", err)
	}
	if at := strings.TrimSpace(os.Getenv("BESY_TG_REPORT_AT")); at != "" {
		if at == "off" {
			c.report = false
		} else if _, err := fmt.Sscanf(at, "%d:%d", &c.reportH, &c.reportM); err != nil ||
			c.reportH < 0 || c.reportH > 23 || c.reportM < 0 || c.reportM > 59 {
			return nil, fmt.Errorf("BESY_TG_REPORT_AT: %q is not HH:MM", at)
		}
	}
	if p := strings.TrimSpace(os.Getenv("BESY_TG_STATE")); p != "" {
		c.statePath = p
	}
	return c, nil
}

// peerSource is what the bot needs from the server.
type peerSource interface {
	Peers(ctx context.Context) ([]provision.Peer, error)
}

type statsBot struct {
	cfg      *tgConfig
	device   peerSource
	owns     func(string) bool
	tally    *provision.Tally
	capacity int
	api      string // https://api.telegram.org/bot<token>
	client   *http.Client

	mu     sync.Mutex
	book   *provision.Book
	meter  provision.TrafficMeter
	counts [3]int64 // the tally at the previous sample
}

// sampleEvery is how often the totals are brought up to date. Five
// minutes catches the evening peak well enough and costs one `awg show`.
const sampleEvery = 5 * time.Minute

func runTelegram(ctx context.Context, cfg *tgConfig, device peerSource, owns func(string) bool,
	tally *provision.Tally, capacity int) {
	book, err := provision.LoadBook(cfg.statePath)
	if err != nil {
		log.Printf("besy-provision: telegram: the day totals could not be read, starting afresh: %v", err)
		book = &provision.Book{}
	}
	b := &statsBot{
		cfg: cfg, device: device, owns: owns, tally: tally, capacity: capacity,
		api:    "https://api.telegram.org/bot" + cfg.token,
		client: &http.Client{Timeout: 70 * time.Second},
		book:   book,
	}
	if len(cfg.chats) == 0 {
		log.Print("besy-provision: telegram: no chats allowed yet; send /start to the bot and add the id it replies with to BESY_TG_CHATS")
	}
	log.Printf("besy-provision: telegram: on, %d chat(s), day totals in %s", len(cfg.chats), cfg.statePath)

	go b.sampleLoop(ctx)
	if cfg.report {
		go b.reportLoop(ctx)
	}
	b.pollLoop(ctx)
}

func (b *statsBot) today(now time.Time) string { return now.In(b.cfg.loc).Format("2006-01-02") }

// sample brings the day's totals up to date and returns the current
// snapshot.
func (b *statsBot) sample(ctx context.Context) (provision.Snapshot, error) {
	peers, err := b.device.Peers(ctx)
	if err != nil {
		return provision.Snapshot{}, err
	}
	now := time.Now()
	snap := provision.Summarize(peers, b.owns, now)
	issued, forgotten, withdrawn := b.tally.Counts()

	b.mu.Lock()
	defer b.mu.Unlock()
	rx, tx := b.meter.Measure(peers, b.owns)
	b.book.Add(b.today(now), snap,
		issued-b.counts[0], forgotten-b.counts[1], withdrawn-b.counts[2], rx, tx)
	b.counts = [3]int64{issued, forgotten, withdrawn}
	if err := b.book.Save(b.cfg.statePath); err != nil {
		log.Printf("besy-provision: telegram: saving the day totals: %v", err)
	}
	return snap, nil
}

func (b *statsBot) sampleLoop(ctx context.Context) {
	t := time.NewTicker(sampleEvery)
	defer t.Stop()
	for {
		if _, err := b.sample(ctx); err != nil && ctx.Err() == nil {
			log.Printf("besy-provision: telegram: reading the peers: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// nextReport is the next time the report is due after now.
func nextReport(now time.Time, loc *time.Location, h, m int) time.Time {
	local := now.In(loc)
	at := time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)
	if !at.After(local) {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

func (b *statsBot) reportLoop(ctx context.Context) {
	for {
		wait := time.Until(nextReport(time.Now(), b.cfg.loc, b.cfg.reportH, b.cfg.reportM))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		// Bring yesterday's last minutes in before reporting it.
		_, _ = b.sample(ctx)
		yesterday := time.Now().In(b.cfg.loc).AddDate(0, 0, -1).Format("2006-01-02")
		b.mu.Lock()
		day := b.book.Get(yesterday)
		b.mu.Unlock()
		text := formatDay("📊 BESY VPN — итоги за "+humanDate(yesterday), day, b.capacity)
		for id := range b.cfg.chats {
			b.send(ctx, id, text)
		}
	}
}

// ---- Telegram ------------------------------------------------------------

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func (b *statsBot) pollLoop(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		var updates []tgUpdate
		err := b.call(ctx, "getUpdates", map[string]any{
			"offset": offset, "timeout": 50, "allowed_updates": []string{"message"},
		}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("besy-provision: telegram: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message != nil {
				b.handle(ctx, u.Message.Chat.ID, u.Message.Text)
			}
		}
	}
}

func (b *statsBot) handle(ctx context.Context, chat int64, text string) {
	cmd := strings.Fields(text)
	if len(cmd) == 0 {
		return
	}
	name := strings.ToLower(strings.SplitN(cmd[0], "@", 2)[0])

	if !b.cfg.chats[chat] {
		// Strangers learn that the bot is private and, so that setting
		// it up needs no other tool, their own chat id. Nothing else.
		if name == "/start" {
			b.send(ctx, chat, fmt.Sprintf("Это закрытый бот статистики BESY VPN.\nВаш chat id: %d", chat))
		}
		return
	}

	switch name {
	case "/now", "/сейчас":
		snap, err := b.sample(ctx)
		if err != nil {
			b.send(ctx, chat, "Не удалось прочитать данные сервера.")
			return
		}
		b.send(ctx, chat, formatNow(snap, b.capacity))
	case "/today", "/сегодня":
		if _, err := b.sample(ctx); err != nil {
			b.send(ctx, chat, "Не удалось прочитать данные сервера.")
			return
		}
		date := b.today(time.Now())
		b.mu.Lock()
		day := b.book.Get(date)
		b.mu.Unlock()
		b.send(ctx, chat, formatDay("📊 Сегодня, "+humanDate(date)+" (с 00:00)", day, b.capacity))
	case "/week", "/неделя":
		b.mu.Lock()
		days := lastDays(b.book, time.Now().In(b.cfg.loc), 7)
		b.mu.Unlock()
		b.send(ctx, chat, formatWeek(days))
	default:
		b.send(ctx, chat, helpText)
	}
}

const helpText = "BESY VPN — статистика сервера\n\n" +
	"/now — сколько подключено сейчас\n" +
	"/today — итоги за сегодня\n" +
	"/week — последние 7 дней\n\n" +
	"Каждое утро приходят итоги за вчера. Только суммы: бот не знает, кто и что делал."

func (b *statsBot) send(ctx context.Context, chat int64, text string) {
	if err := b.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text}, nil); err != nil {
		log.Printf("besy-provision: telegram: sending: %v", err)
	}
}

// call runs one Bot API method. Its errors never carry the URL, which
// carries the token.
func (b *statsBot) call(ctx context.Context, method string, params any, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.api+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: building the request", method)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("%s: %v", method, err)
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("%s: HTTP %d, unreadable reply", method, resp.StatusCode)
	}
	if !out.OK {
		return fmt.Errorf("%s: %s", method, out.Description)
	}
	if result != nil {
		return json.Unmarshal(out.Result, result)
	}
	return nil
}

// ---- Text ----------------------------------------------------------------

func formatNow(s provision.Snapshot, capacity int) string {
	return fmt.Sprintf("🟢 Сейчас подключено: %d\n"+
		"Активны за сутки: %d · за 7 дней: %d\n"+
		"Действующих ключей: %d из %d",
		s.Connected, s.Active24h, s.Active7d, s.Keys, capacity)
}

func formatDay(title string, d provision.Day, capacity int) string {
	return fmt.Sprintf("%s\n\n"+
		"Пик одновременно: %d\n"+
		"Активны за сутки: %d\n"+
		"Новых ключей: %d\n"+
		"Удалили сами: %d · отозвано за неактивность: %d\n"+
		"Действующих ключей: %d из %d\n"+
		"Трафик: %s (к пользователям %s, от них %s)",
		title, d.PeakConnected, d.Active24h, d.Issued, d.Forgotten, d.Withdrawn,
		d.Keys, capacity, bytesRU(d.RxBytes+d.TxBytes), bytesRU(d.TxBytes), bytesRU(d.RxBytes))
}

func formatWeek(days []provision.Day) string {
	var sb strings.Builder
	sb.WriteString("📈 Последние 7 дней\nдата · пик · активны · новые · трафик\n")
	for _, d := range days {
		fmt.Fprintf(&sb, "%s · %d · %d · %d · %s\n",
			humanDate(d.Date), d.PeakConnected, d.Active24h, d.Issued, bytesRU(d.RxBytes+d.TxBytes))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// lastDays returns the n days ending today, oldest first, zero where
// nothing was recorded.
func lastDays(b *provision.Book, today time.Time, n int) []provision.Day {
	out := make([]provision.Day, 0, n)
	for i := n - 1; i >= 0; i-- {
		out = append(out, b.Get(today.AddDate(0, 0, -i).Format("2006-01-02")))
	}
	return out
}

// humanDate turns 2026-09-29 into 29.09.
func humanDate(date string) string {
	if t, err := time.Parse("2006-01-02", date); err == nil {
		return t.Format("02.01")
	}
	return date
}

// bytesRU prints a byte count the way a person reads it, in Russian units.
func bytesRU(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d Б", n)
	}
	units := []string{"КБ", "МБ", "ГБ", "ТБ", "ПБ"}
	v, i := float64(n)/unit, 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", v, units[i]), ".", ",", 1)
}
