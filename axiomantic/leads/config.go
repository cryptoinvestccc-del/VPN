package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	_ "time/tzdata" // workspaces name their zone; a bare server may have no zoneinfo
)

// Brand is what makes a client's copy of the panel look like theirs.
type Brand struct {
	Accent string `json:"accent"`
	Logo   string `json:"logo"`
}

// Workspace is one site whose leads one team works on: the studio itself,
// or a client the studio hosted a site for. Leads, users and every admin
// request belong to exactly one workspace.
type Workspace struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	TZ           string   `json:"tz"`
	Origins      []string `json:"origins"`
	SiteURL      string   `json:"siteURL,omitempty"`
	TelegramChat string   `json:"telegramChat,omitempty"`
	Brand        Brand    `json:"brand"`
	// Site, when set, lets the panel edit and publish this workspace's site.
	Site *SiteConfig `json:"site,omitempty"`
	// NoLeads hides the leads section for sites that do not take forms here.
	NoLeads bool `json:"noLeads,omitempty"`

	loc *time.Location
}

// SiteConfig says where a workspace's site is built and published.
//
//	builder  — the site generator's command line, run without a shell
//	template — a built copy of the site; every build starts from a copy of it
//	root     — releases/, current (what nginx serves) and preview/
//	data     — the draft, uploaded pictures and the record of releases;
//	           outside root, so a draft is never on the web before it is
//	           published
//	schema   — what may be edited (cms/schema.json)
type SiteConfig struct {
	Builder  []string `json:"builder"`
	Template string   `json:"template"`
	Root     string   `json:"root"`
	Data     string   `json:"data"`
	Schema   string   `json:"schema"`
	Keep     int      `json:"keep,omitempty"`
}

func (c *SiteConfig) validate() error {
	if len(c.Builder) == 0 || c.Builder[0] == "" {
		return errors.New("site.builder is the generator's command line")
	}
	for name, p := range map[string]string{"template": c.Template, "root": c.Root, "data": c.Data, "schema": c.Schema} {
		if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fmt.Errorf("site.%s must be an absolute, clean path", name)
		}
	}
	if rel, err := filepath.Rel(c.Root, c.Data); err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return errors.New("site.data must be outside site.root: drafts are not for the web")
	}
	if c.Keep == 0 {
		c.Keep = 10
	}
	if c.Keep < 2 || c.Keep > 100 {
		return errors.New("site.keep is between 2 and 100 releases")
	}
	return nil
}

// User is a person allowed into one workspace's panel.
type User struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Workspace string `json:"workspace"`
	Hash      string `json:"hash"`
	Disabled  bool   `json:"disabled,omitempty"`
}

// AdminConfig is admin.json: who may sign in, and to which workspace.
// It holds password hashes, so it lives outside the site with mode 0600.
type AdminConfig struct {
	// DefaultWorkspace gets the leads stored before workspaces existed and
	// the ones posted without an Origin. It is named, never implied by
	// order, so adding a client first cannot hand them the studio's leads.
	DefaultWorkspace string       `json:"defaultWorkspace"`
	Workspaces       []*Workspace `json:"workspaces"`
	Users            []*User      `json:"users"`
}

const (
	roleOwner   = "owner"
	roleManager = "manager"
	roleViewer  = "viewer"
)

var roleRank = map[string]int{roleViewer: 1, roleManager: 2, roleOwner: 3}

var (
	reWorkspaceID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	reUserID      = regexp.MustCompile(`^u_[0-9a-f]{12}$`)
	reAccent      = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	reChat        = regexp.MustCompile(`^-?[0-9]{1,20}$|^@[A-Za-z0-9_]{5,32}$`)
)

func (c *AdminConfig) Workspace(id string) *Workspace {
	for _, w := range c.Workspaces {
		if w.ID == id {
			return w
		}
	}
	return nil
}

func (c *AdminConfig) User(id string) *User {
	for _, u := range c.Users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

// UserByLogin matches logins without regard to case: people type their
// e-mail as they like.
func (c *AdminConfig) UserByLogin(login string) *User {
	login = normLogin(login)
	for _, u := range c.Users {
		if normLogin(u.Login) == login {
			return u
		}
	}
	return nil
}

func normLogin(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Members lists the users of one workspace, never anyone else's.
func (c *AdminConfig) Members(ws string) []*User {
	var out []*User
	for _, u := range c.Users {
		if u.Workspace == ws {
			out = append(out, u)
		}
	}
	return out
}

// normOrigin reduces a URL to scheme://host[:port], the form browsers send
// in the Origin header.
func normOrigin(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil ||
		strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not an origin like https://example.ru", s)
	}
	for _, r := range u.Host {
		if r > 0x7e {
			// Browsers send IDN hosts as punycode; a Cyrillic origin here
			// would never match one.
			return "", fmt.Errorf("%q: write the domain in punycode, e.g. https://xn--80ak6aa92e.xn--p1ai", s)
		}
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

// label is text shown as a name in the panel: no control characters and
// no invisible formatting such as bidi overrides that could disguise it.
func label(s string, lo, hi int) bool {
	if !between(strings.TrimSpace(s), lo, hi) || !plain(s, false) {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// Validate checks the whole file and fills in what is derived from it.
// A config that fails here is never used: the previous one stays.
func (c *AdminConfig) Validate() error {
	if len(c.Workspaces) == 0 {
		return errors.New("admin config: no workspaces")
	}
	ids := map[string]bool{}
	origins := map[string]string{}
	for _, w := range c.Workspaces {
		if !reWorkspaceID.MatchString(w.ID) {
			return fmt.Errorf("admin config: workspace id %q: use a-z, 0-9 and dashes", w.ID)
		}
		if ids[w.ID] {
			return fmt.Errorf("admin config: workspace %q twice", w.ID)
		}
		ids[w.ID] = true
		if !label(w.Name, 1, 60) {
			return fmt.Errorf("admin config: workspace %q needs a plain name up to 60 characters", w.ID)
		}
		if w.TZ == "" {
			w.TZ = "Europe/Moscow"
		}
		loc, err := time.LoadLocation(w.TZ)
		if err != nil {
			return fmt.Errorf("admin config: workspace %q: time zone %q unknown", w.ID, w.TZ)
		}
		w.loc = loc
		for i, o := range w.Origins {
			n, err := normOrigin(o)
			if err != nil {
				return fmt.Errorf("admin config: workspace %q: %v", w.ID, err)
			}
			if other, dup := origins[n]; dup {
				return fmt.Errorf("admin config: origin %s belongs to %q and %q", n, other, w.ID)
			}
			origins[n] = w.ID
			w.Origins[i] = n
		}
		if w.SiteURL != "" {
			if u, err := url.Parse(w.SiteURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("admin config: workspace %q: siteURL must be an http(s) address", w.ID)
			}
		}
		if w.Site != nil {
			if err := w.Site.validate(); err != nil {
				return fmt.Errorf("admin config: workspace %q: %v", w.ID, err)
			}
		}
		if w.TelegramChat != "" && !reChat.MatchString(w.TelegramChat) {
			return fmt.Errorf("admin config: workspace %q: telegramChat is a chat id or @channel", w.ID)
		}
		if w.Brand.Accent == "" {
			w.Brand.Accent = "#3D4BF0"
		}
		if !reAccent.MatchString(w.Brand.Accent) {
			return fmt.Errorf("admin config: workspace %q: accent must look like #3D4BF0", w.ID)
		}
		if w.Brand.Logo == "" {
			r, _ := utf8.DecodeRuneInString(w.Name)
			w.Brand.Logo = strings.ToUpper(string(r))
		}
		if !label(w.Brand.Logo, 1, 2) || strings.ContainsAny(w.Brand.Logo, "<>&\"' ") {
			return fmt.Errorf("admin config: workspace %q: logo is one or two letters", w.ID)
		}
	}
	if c.DefaultWorkspace == "" && len(c.Workspaces) == 1 {
		c.DefaultWorkspace = c.Workspaces[0].ID
	}
	if !ids[c.DefaultWorkspace] {
		return errors.New("admin config: defaultWorkspace must name the workspace that gets old and Origin-less leads")
	}
	uids := map[string]bool{}
	logins := map[string]bool{}
	for _, u := range c.Users {
		if !reUserID.MatchString(u.ID) || uids[u.ID] {
			return fmt.Errorf("admin config: bad or repeated user id %q", u.ID)
		}
		uids[u.ID] = true
		l := normLogin(u.Login)
		if !between(l, 3, 120) || strings.ContainsAny(l, " \t\r\n") || !plain(l, false) {
			return fmt.Errorf("admin config: user %s: login must be 3–120 characters without spaces", u.ID)
		}
		if logins[l] {
			return fmt.Errorf("admin config: login %q twice", u.Login)
		}
		logins[l] = true
		if !label(u.Name, 1, 80) {
			return fmt.Errorf("admin config: user %s needs a name up to 80 characters", u.ID)
		}
		if roleRank[u.Role] == 0 {
			return fmt.Errorf("admin config: user %s: role is owner, manager or viewer", u.ID)
		}
		if !ids[u.Workspace] {
			return fmt.Errorf("admin config: user %s: workspace %q does not exist", u.ID, u.Workspace)
		}
		if _, err := parseHash(u.Hash); err != nil {
			return fmt.Errorf("admin config: user %s: %v", u.ID, err)
		}
	}
	return nil
}

// WorkspaceForOrigin finds whose site a browser posted from.
func (c *AdminConfig) WorkspaceForOrigin(origin string) *Workspace {
	n, err := normOrigin(origin)
	if err != nil {
		return nil
	}
	for _, w := range c.Workspaces {
		for _, o := range w.Origins {
			if o == n {
				return w
			}
		}
	}
	return nil
}

// LoadAdminConfig reads and validates admin.json. A file that others on
// the host could read is refused: it holds password hashes.
func LoadAdminConfig(path string) (*AdminConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("admin config: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("admin config: %w", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("admin config %s is readable by others (mode %v): chmod 600 it", path, st.Mode().Perm())
	}
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var c AdminConfig
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("admin config: %v", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveAdminConfig replaces the file in one step, so the server never reads
// half of it.
func SaveAdminConfig(path string, c *AdminConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'))
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	// Keep the owner of the file being replaced: the service reads it as
	// its own user, and root running the console must not take it away.
	if st, err := os.Stat(path); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
			if err := tmp.Chown(int(sys.Uid), int(sys.Gid)); err != nil {
				tmp.Close()
				return fmt.Errorf("cannot keep the owner of %s: %w", path, err)
			}
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// ConfigSource re-reads admin.json when the file changes, so a user added,
// disabled or given a new password from the console takes effect without
// a restart. A broken edit keeps the last good config.
type ConfigSource struct {
	path    string
	mu      sync.Mutex
	cur     *AdminConfig
	mod     time.Time
	size    int64
	checked time.Time
	now     func() time.Time
	logf    func(string, ...any)
	failed  string
	// onReload sees the old and the new config whenever a change is taken
	// in; the panel uses it to end sessions of users who lost access.
	onReload func(old, cur *AdminConfig)
}

func NewConfigSource(path string, now func() time.Time, logf func(string, ...any)) (*ConfigSource, error) {
	c, err := LoadAdminConfig(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &ConfigSource{path: path, cur: c, mod: st.ModTime(), size: st.Size(), checked: now(), now: now, logf: logf}, nil
}

// Get returns the current config, looking at the file at most once a second.
func (s *ConfigSource) Get() *AdminConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now.Sub(s.checked) < time.Second && !now.Before(s.checked) {
		return s.cur
	}
	s.checked = now
	st, err := os.Stat(s.path)
	if err != nil || (st.ModTime().Equal(s.mod) && st.Size() == s.size) {
		return s.cur
	}
	c, err := LoadAdminConfig(s.path)
	if err != nil {
		if msg := err.Error(); msg != s.failed {
			s.logf("admin config not reloaded, keeping the previous one: %v", err)
			s.failed = msg
		}
		return s.cur
	}
	old := s.cur
	s.cur, s.mod, s.size, s.failed = c, st.ModTime(), st.Size(), ""
	s.logf("admin config reloaded: %d workspaces, %d users", len(c.Workspaces), len(c.Users))
	if s.onReload != nil {
		s.onReload(old, c)
	}
	return c
}

// Update changes admin.json from the panel: under the same lock as the
// console, on a fresh read of the file, then taken in at once.
func (s *ConfigSource) Update(change func(c *AdminConfig) error) error {
	unlock, err := lockConfig(s.path)
	if err != nil {
		return err
	}
	c, err := LoadAdminConfig(s.path)
	if err == nil {
		if err = change(c); err == nil {
			err = SaveAdminConfig(s.path, c)
		}
	}
	unlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.checked = time.Time{}
	s.mod = time.Time{}
	s.mu.Unlock()
	s.Get()
	return nil
}

// revoked lists users of old who lost their access in cur: removed,
// disabled, moved to another workspace or given a new password. Their
// sessions must end at once — not only when a session is next used, or a
// stolen cookie that sat idle while the user was disabled would work
// again after «user enable».
func revoked(old, cur *AdminConfig) []string {
	var out []string
	for _, u := range old.Users {
		n := cur.User(u.ID)
		if n == nil || n.Disabled || n.Workspace != u.Workspace || n.Hash != u.Hash {
			out = append(out, u.ID)
		}
	}
	return out
}
