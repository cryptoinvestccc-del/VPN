package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Passwords are stored as PBKDF2-HMAC-SHA256 with a random salt, the
// iteration count OWASP recommends for it. The count travels inside the
// hash, so raising it later leaves old hashes working.
var hashIterations = 600_000

const (
	saltLen    = 16
	keyLen     = 32
	minPassLen = 10
)

type pbkdf2Hash struct {
	iter int
	salt []byte
	key  []byte
}

func parseHash(s string) (pbkdf2Hash, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return pbkdf2Hash{}, errors.New("password hash is not pbkdf2-sha256$iterations$salt$key")
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 || iter > 10_000_000 {
		return pbkdf2Hash{}, errors.New("password hash has a bad iteration count")
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(key) != keyLen {
		return pbkdf2Hash{}, errors.New("password hash is damaged")
	}
	return pbkdf2Hash{iter: iter, salt: salt, key: key}, nil
}

func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, hashIterations, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", hashIterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword compares in constant time. It runs the full derivation
// even for a damaged hash, so its timing says nothing either way.
func CheckPassword(hash, pw string) bool {
	h, err := parseHash(hash)
	if err != nil {
		h, _ = parseHash(dummyHash())
		pw += "\x00never"
	}
	key, kerr := pbkdf2.Key(sha256.New, pw, h.salt, h.iter, keyLen)
	return kerr == nil && err == nil && subtle.ConstantTimeCompare(key, h.key) == 1
}

var (
	dummyOnce sync.Once
	dummy     string
)

// dummyHash is checked against when the login is unknown, so an unknown
// login costs as much time as a wrong password.
func dummyHash() string {
	dummyOnce.Do(func() {
		dummy, _ = HashPassword("not a password anyone has")
	})
	return dummy
}

// hashPrint identifies a password hash without revealing it. A session
// remembers it, so a changed password ends the sessions made with the old one.
func hashPrint(hash string) string {
	sum := sha256.Sum256([]byte("session-binding\x00" + hash))
	return hex.EncodeToString(sum[:8])
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func newUserID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "u_" + hex.EncodeToString(b)
}

// Session is one signed-in browser.
type Session struct {
	UserID    string
	Workspace string
	CSRF      string
	print     string
	created   time.Time
	seen      time.Time
}

// Sessions keeps signed-in browsers in memory, keyed by the SHA-256 of the
// cookie value: a memory dump or a debug print does not hand out working
// cookies. A restart signs everyone out, which is acceptable for a panel.
type Sessions struct {
	mu      sync.Mutex
	m       map[[32]byte]*Session
	idle    time.Duration
	maxAge  time.Duration
	perUser int
}

func NewSessions(idle, maxAge time.Duration) *Sessions {
	return &Sessions{m: map[[32]byte]*Session{}, idle: idle, maxAge: maxAge, perUser: 20}
}

func tokenKey(token string) [32]byte { return sha256.Sum256([]byte(token)) }

// New starts a session and returns the cookie value. Expired sessions are
// swept here, and one user keeps at most perUser of them (the oldest go).
func (s *Sessions) New(u *User, now time.Time) (string, *Session) {
	token := randomToken(32)
	sess := &Session{UserID: u.ID, Workspace: u.Workspace, CSRF: randomToken(24), print: hashPrint(u.Hash), created: now, seen: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldestKey [32]byte
	var oldest *Session
	mine := 0
	for k, v := range s.m {
		if s.expired(v, now) {
			delete(s.m, k)
			continue
		}
		if v.UserID == u.ID {
			mine++
			if oldest == nil || v.created.Before(oldest.created) {
				oldest, oldestKey = v, k
			}
		}
	}
	if mine >= s.perUser && oldest != nil {
		delete(s.m, oldestKey)
	}
	s.m[tokenKey(token)] = sess
	return token, sess
}

func (s *Sessions) expired(v *Session, now time.Time) bool {
	return now.Sub(v.seen) > s.idle || now.Sub(v.created) > s.maxAge
}

// Get returns the live session for a cookie value and marks it used.
func (s *Sessions) Get(token string, now time.Time) *Session {
	if token == "" || len(token) > 128 {
		return nil
	}
	k := tokenKey(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.m[k]
	if v == nil {
		return nil
	}
	if s.expired(v, now) {
		delete(s.m, k)
		return nil
	}
	v.seen = now
	return v
}

func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	delete(s.m, tokenKey(token))
	s.mu.Unlock()
}

// DropUser ends every session of one user.
func (s *Sessions) DropUser(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if v.UserID == id {
			delete(s.m, k)
		}
	}
}

func (s *Sessions) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// validFor reports whether a session still matches the user it was made
// for: the user exists, is enabled, is in the same workspace and has the
// same password as at sign-in.
func (v *Session) validFor(u *User) bool {
	return u != nil && !u.Disabled && u.Workspace == v.Workspace && hashPrint(u.Hash) == v.print
}

func sameToken(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
