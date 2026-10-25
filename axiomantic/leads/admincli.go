package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode/utf8"
)

const adminUsage = `Team panel setup. admin.json holds password hashes: keep it mode 600, outside the site.

  axiomantic-leads admin workspace set -config FILE -id ID -name NAME [-origins URL,URL] [-site URL]
                                       [-tz Europe/Moscow] [-accent #3D4BF0] [-logo А] [-telegram-chat ID] [-default]
                                       [-site-dir /opt/axiomantic -site-root /var/www/axiomantic -site-data /var/lib/axiomantic/site]
                                       [-no-leads]
  axiomantic-leads admin user add      -config FILE -workspace ID -login LOGIN -name NAME -role owner|manager|viewer [-password-stdin]
  axiomantic-leads admin user passwd   -config FILE -login LOGIN [-password-stdin]
  axiomantic-leads admin user disable  -config FILE -login LOGIN
  axiomantic-leads admin user enable   -config FILE -login LOGIN
  axiomantic-leads admin user remove   -config FILE -login LOGIN
  axiomantic-leads admin user list     -config FILE
  axiomantic-leads admin demo          -config FILE -store FILE -workspace ID [-n 140] [-seed 1]

Without -password-stdin a strong password is generated and printed once.
Passwords are never taken from flags: other users of the host can read
command lines in the process list.
`

// appliesNote says exactly when a change reaches the panel: the server
// rereads the file it was started with when that file changes.
const appliesNote = "A panel started with -admin pointing at this file applies it within a few seconds and ends\n" +
	"the user's open sessions. A panel reading another copy of the file needs a restart.\n"

// adminCLI runs "axiomantic-leads admin …" and returns the exit code.
func adminCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, adminUsage)
		return 2
	}
	var err error
	switch {
	case args[0] == "workspace" && len(args) > 1 && args[1] == "set":
		err = cliWorkspaceSet(args[2:], stdout)
	case args[0] == "user" && len(args) > 1:
		err = cliUser(args[1], args[2:], stdin, stdout)
	case args[0] == "demo":
		err = cliDemo(args[1:], stdout)
	default:
		fmt.Fprint(stderr, adminUsage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 2
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

// lockConfig holds an exclusive lock on admin.json.lock for one
// read-change-write, so two console commands run at once cannot undo each
// other (a «disable» lost under a concurrent «user add», say).
func lockConfig(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func loadOrEmpty(path string) (*AdminConfig, error) {
	if path == "" {
		return nil, errors.New("-config is required")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return &AdminConfig{}, nil
	}
	return LoadAdminConfig(path)
}

func cliWorkspaceSet(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("workspace set", flag.ContinueOnError)
	path := fs.String("config", "", "admin.json")
	id := fs.String("id", "", "workspace id: a-z, 0-9, dashes")
	name := fs.String("name", "", "name shown in the panel")
	origins := fs.String("origins", "", "comma-separated site origins whose forms belong here")
	site := fs.String("site", "", "site address for the «Открыть сайт» link")
	tz := fs.String("tz", "", "time zone, default Europe/Moscow")
	accent := fs.String("accent", "", "brand colour like #3D4BF0")
	logo := fs.String("logo", "", "one or two letters for the logo tile")
	chat := fs.String("telegram-chat", "", "Telegram chat id for this workspace's leads")
	def := fs.Bool("default", false, "make this the default workspace (old and Origin-less leads go there)")
	siteDir := fs.String("site-dir", "", "the site's sources (build.py, site/, cms/schema.json): turns on the site editor")
	siteRoot := fs.String("site-root", "", "where releases are published; the web server serves <root>/current")
	siteData := fs.String("site-data", "", "the editor's draft, uploads and release records (not under -site-root)")
	python := fs.String("python", "/usr/bin/python3", "Python for the site generator")
	noLeads := fs.Bool("no-leads", false, "hide the leads section for this workspace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	unlock, err := lockConfig(*path)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	w := c.Workspace(*id)
	if w == nil {
		if *name == "" {
			return errors.New("-name is required for a new workspace")
		}
		w = &Workspace{ID: *id}
		c.Workspaces = append(c.Workspaces, w)
	}
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&w.Name, *name)
	set(&w.SiteURL, *site)
	set(&w.TZ, *tz)
	set(&w.Brand.Accent, *accent)
	set(&w.Brand.Logo, *logo)
	set(&w.TelegramChat, *chat)
	if *siteDir != "" {
		if *siteRoot == "" || *siteData == "" {
			return errors.New("-site-dir needs -site-root and -site-data")
		}
		dir := filepath.Clean(*siteDir)
		w.Site = &SiteConfig{Builder: []string{*python, filepath.Join(dir, "build.py")}, Template: filepath.Join(dir, "site"),
			Root: filepath.Clean(*siteRoot), Data: filepath.Clean(*siteData), Schema: filepath.Join(dir, "cms", "schema.json")}
	}
	if *noLeads {
		w.NoLeads = true
	}
	if *def || c.DefaultWorkspace == "" {
		c.DefaultWorkspace = w.ID
	}
	if *origins != "" {
		w.Origins = nil
		for _, o := range strings.Split(*origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				w.Origins = append(w.Origins, o)
			}
		}
	}
	if err := SaveAdminConfig(*path, c); err != nil {
		return err
	}
	fmt.Fprintf(out, "workspace %s saved (default workspace: %s)\n", w.ID, c.DefaultWorkspace)
	return nil
}

// readPassword takes one line from stdin, or makes a password up.
func readPassword(fromStdin bool, stdin io.Reader) (pw string, generated bool, err error) {
	if !fromStdin {
		return randomToken(15), true, nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	pw = strings.TrimRight(line, "\r\n")
	if utf8.RuneCountInString(pw) < minPassLen {
		return "", false, fmt.Errorf("the password needs at least %d characters", minPassLen)
	}
	if len(pw) > 256 {
		return "", false, errors.New("the password is longer than 256 bytes")
	}
	return pw, false, nil
}

func cliUser(cmd string, args []string, stdin io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("user "+cmd, flag.ContinueOnError)
	path := fs.String("config", "", "admin.json")
	login := fs.String("login", "", "login, usually an e-mail")
	name := fs.String("name", "", "person's name")
	role := fs.String("role", "", "owner, manager or viewer")
	ws := fs.String("workspace", "", "workspace id")
	fromStdin := fs.Bool("password-stdin", false, "read the password from the first line of stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	unlock, err := lockConfig(*path)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := loadOrEmpty(*path)
	if err != nil {
		return err
	}
	if cmd == "list" {
		tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "LOGIN\tNAME\tROLE\tWORKSPACE\tSTATE")
		for _, u := range c.Users {
			state := "active"
			if u.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.Login, u.Name, u.Role, u.Workspace, state)
		}
		return tw.Flush()
	}
	if strings.TrimSpace(*login) == "" {
		return errors.New("-login is required")
	}
	u := c.UserByLogin(*login)
	switch cmd {
	case "add":
		if u != nil {
			return fmt.Errorf("login %s already exists", *login)
		}
		if c.Workspace(*ws) == nil {
			return fmt.Errorf("workspace %q does not exist: create it with «admin workspace set»", *ws)
		}
		pw, gen, err := readPassword(*fromStdin, stdin)
		if err != nil {
			return err
		}
		hash, err := HashPassword(pw)
		if err != nil {
			return err
		}
		c.Users = append(c.Users, &User{ID: newUserID(), Login: normLogin(*login), Name: strings.TrimSpace(*name),
			Role: *role, Workspace: *ws, Hash: hash})
		if err := SaveAdminConfig(*path, c); err != nil {
			return err
		}
		fmt.Fprintf(out, "user %s added to %s as %s\n", *login, *ws, *role)
		if gen {
			fmt.Fprintf(out, "password (shown once, pass it on privately): %s\n", pw)
		}
		return nil
	case "passwd":
		if u == nil {
			return fmt.Errorf("no user %s", *login)
		}
		pw, gen, err := readPassword(*fromStdin, stdin)
		if err != nil {
			return err
		}
		if u.Hash, err = HashPassword(pw); err != nil {
			return err
		}
		if err := SaveAdminConfig(*path, c); err != nil {
			return err
		}
		fmt.Fprintf(out, "password of %s changed\n%s", u.Login, appliesNote)
		if gen {
			fmt.Fprintf(out, "password (shown once, pass it on privately): %s\n", pw)
		}
		return nil
	case "disable", "enable":
		if u == nil {
			return fmt.Errorf("no user %s", *login)
		}
		u.Disabled = cmd == "disable"
		if err := SaveAdminConfig(*path, c); err != nil {
			return err
		}
		fmt.Fprintf(out, "user %s %sd\n%s", u.Login, cmd, appliesNote)
		return nil
	case "remove":
		if u == nil {
			return fmt.Errorf("no user %s", *login)
		}
		kept := c.Users[:0]
		for _, x := range c.Users {
			if x != u {
				kept = append(kept, x)
			}
		}
		c.Users = kept
		if err := SaveAdminConfig(*path, c); err != nil {
			return err
		}
		fmt.Fprintf(out, "user %s removed\n%s", u.Login, appliesNote)
		return nil
	}
	return fmt.Errorf("unknown command «user %s»", cmd)
}

// cliDemo fills an empty store with made-up leads, so the panel can be
// shown to a client before their site goes live. It refuses a store that
// already has leads in it: demo data must never mix with real requests.
func cliDemo(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	path := fs.String("config", "", "admin.json (team members act in the demo history)")
	store := fs.String("store", "", "lead file to create")
	wsID := fs.String("workspace", "", "workspace id")
	n := fs.Int("n", 140, "number of leads")
	seed := fs.Uint64("seed", 1, "random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := LoadAdminConfig(*path)
	if err != nil {
		return err
	}
	if c.Workspace(*wsID) == nil {
		return fmt.Errorf("workspace %q does not exist", *wsID)
	}
	var team []string
	for _, u := range c.Members(*wsID) {
		if u.Role != roleViewer && !u.Disabled {
			team = append(team, u.ID)
		}
	}
	if len(team) == 0 {
		return errors.New("the workspace needs an owner or manager to act in the demo history")
	}
	if *store == "" {
		return errors.New("-store is required")
	}
	if st, err := os.Stat(*store); err == nil && st.Size() > 0 {
		return fmt.Errorf("%s already has leads; the demo only fills an empty store", *store)
	}
	fsr, err := OpenFileStore(*store)
	if err != nil {
		return err
	}
	defer fsr.Close()
	recs, evs := demoLeads(*wsID, team, *n, *seed, time.Now().UTC())
	for _, r := range recs {
		if err := fsr.Append(r); err != nil {
			return err
		}
	}
	for _, e := range evs {
		if err := fsr.AppendEvent(e); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "%d demo leads and %d events written to %s\n", len(recs), len(evs), *store)
	return nil
}

func demoLeads(ws string, team []string, n int, seed uint64, now time.Time) ([]Record, []Event) {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	names := []string{"Ирина", "Алексей", "Мария", "Дмитрий", "Ольга", "Сергей", "Анна", "Павел", "Екатерина", "Никита",
		"Татьяна", "Артём", "Юлия", "Максим", "Наталья", "Глеб", "Вера", "Роман", "Ксения", "Тимур", "Полина", "Егор"}
	messages := []string{
		"Нужен лендинг для курса по керамике, запуск через месяц.",
		"Хотим обновить сайт стоматологии: запись онлайн, цены, врачи.",
		"Есть интернет-магазин на конструкторе, хотим уйти на свой сайт.",
		"Нужен сайт для архитектурного бюро с портфолио проектов.",
		"Сделайте, пожалуйста, страницу под рекламную кампанию — сроки горят.",
		"Интересует редизайн и SEO. Сейчас почти нет заявок с сайта.",
		"Школа английского: нужна запись на пробный урок и оплата.",
		"Подскажите стоимость многостраничного сайта для производства.",
	}
	interests := []string{"landing", "multi-base", "multi-custom", "unknown", ""}
	budgets := []string{"lt100", "100-300", "gt300", "unknown", ""}
	var recs []Record
	var evs []Event
	for i := 0; i < n; i++ {
		ago := time.Duration(rng.Float64()*rng.Float64()*100*24) * time.Hour
		t := now.Add(-ago - time.Duration(rng.IntN(3600))*time.Second)
		var b [5]byte
		for j := range b {
			b[j] = byte(rng.IntN(256))
		}
		id := t.Format("20060102") + "-" + fmt.Sprintf("%x", b)
		name := names[rng.IntN(len(names))]
		phone := fmt.Sprintf("+7 (900) 000-%02d-%02d", rng.IntN(100), rng.IntN(100))
		l := Lead{Name: name, Consent: true}
		switch rng.IntN(3) {
		case 0:
			l.Source, l.Mode, l.Phone = "modal", "call", phone
			l.When = []string{"hour", "today", "tomorrow", ""}[rng.IntN(4)]
			l.Interest = interests[rng.IntN(len(interests))]
		case 1:
			l.Source, l.Mode = "modal", "write"
			l.Channel = []string{"telegram", "whatsapp", "email"}[rng.IntN(3)]
			switch l.Channel {
			case "telegram":
				l.Contact = fmt.Sprintf("@demo_client_%d", 100+i)
			case "whatsapp":
				l.Contact = phone
			default:
				l.Contact = fmt.Sprintf("client%d@example.ru", 100+i)
			}
			l.Interest = interests[rng.IntN(len(interests))]
			l.Message = messages[rng.IntN(len(messages))]
		default:
			l.Source, l.Mode = "contacts", "form"
			l.Contact = fmt.Sprintf("client%d@example.ru", 100+i)
			l.Budget = budgets[rng.IntN(len(budgets))]
			l.Message = messages[rng.IntN(len(messages))]
		}
		recs = append(recs, Record{ID: id, Time: t, WS: ws, Lead: l})
		// what the team did with it, by how old it is
		ev := func(kind, from, to, text string, after time.Duration) {
			at := t.Add(after)
			if at.After(now) {
				return
			}
			evs = append(evs, Event{Ev: kind, ID: newEventID(), LeadID: id, WS: ws, Time: at,
				By: team[rng.IntN(len(team))], From: from, To: to, Text: text})
		}
		if ago < 3*time.Hour && rng.IntN(3) > 0 {
			continue // fresh and untouched
		}
		resp := time.Duration(5+rng.IntN(90)) * time.Minute
		ev(evAssign, "", team[rng.IntN(len(team))], "", resp-time.Minute)
		ev(evStatus, "new", "contacted", "", resp)
		old := ago > 12*24*time.Hour // older leads have mostly been decided
		switch p := rng.IntN(100); {
		case p < 8:
			ev(evStatus, "contacted", "spam", "", resp+time.Minute)
		case p < 18:
			if old {
				ev(evStatus, "contacted", "lost", "", resp+50*time.Hour)
			}
		case p < 30:
			ev(evNote, "", "", "Отправили бриф, ждём ответ.", resp+2*time.Hour)
			ev(evStatus, "contacted", "proposal", "", resp+26*time.Hour)
			if old {
				ev(evStatus, "proposal", "lost", "", resp+120*time.Hour)
			}
		case p < 46:
			ev(evStatus, "contacted", "proposal", "", resp+20*time.Hour)
			ev(evNote, "", "", "Согласовали смету, договор на подписи.", resp+70*time.Hour)
			ev(evStatus, "proposal", "won", "", resp+96*time.Hour)
		default:
			ev(evStatus, "contacted", "lost", "", resp+30*time.Hour)
		}
	}
	return recs, evs
}
