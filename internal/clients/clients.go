// Package clients holds the set of credentials a server accepts, one per
// client, so access can be granted and withdrawn for a single device
// without disturbing the others.
//
// A single shared key cannot express that: revoking one device means
// changing the key everywhere, which turns every revocation into an
// outage for everyone. That is workable for a handful of personal
// devices and untenable for anything with users.
package clients

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Client is one device's credential.
type Client struct {
	// ID names the client for the operator: it appears in logs and in
	// the file, never on the wire. Nothing about a packet reveals which
	// client sent it.
	ID string `yaml:"id"`

	// PSKBase64 is this client's 32-byte pre-shared key.
	PSKBase64 string `yaml:"psk"`

	// PreviousPSKBase64 lets one client rotate its key without downtime,
	// exactly as psk_previous does for the shared key.
	PreviousPSKBase64 string `yaml:"psk_previous,omitempty"`

	// Disabled revokes this client. The entry is kept rather than
	// deleted so the operator retains a record of who had access, and so
	// a revocation cannot be silently undone by an entry reappearing.
	Disabled bool `yaml:"disabled,omitempty"`
}

// File is the on-disk shape of the credential list.
type File struct {
	Clients []Client `yaml:"clients"`
}

// Credential is one usable key belonging to a client.
type Credential struct {
	ClientID string
	Key      [32]byte
}

// Set is an immutable snapshot of the credentials a server will accept.
type Set struct {
	credentials []Credential
	enabled     map[string]bool
}

// Load reads and validates a credential file.
func Load(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return NewSet(f.Clients)
}

// NewSet validates clients and builds the lookup snapshot.
func NewSet(list []Client) (*Set, error) {
	set := &Set{enabled: make(map[string]bool, len(list))}
	seen := make(map[string]bool, len(list))

	for _, c := range list {
		if c.ID == "" {
			return nil, errors.New("clients: every client needs an id")
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("clients: duplicate id %q", c.ID)
		}
		seen[c.ID] = true

		if c.Disabled {
			// A revoked client is still parsed, so a malformed key in a
			// disabled entry is reported rather than lying dormant until
			// the operator re-enables it.
			if _, err := decodeKey(c.PSKBase64, c.ID, "psk"); err != nil {
				return nil, err
			}
			continue
		}

		key, err := decodeKey(c.PSKBase64, c.ID, "psk")
		if err != nil {
			return nil, err
		}
		set.enabled[c.ID] = true
		set.credentials = append(set.credentials, Credential{ClientID: c.ID, Key: key})

		if c.PreviousPSKBase64 != "" {
			previous, err := decodeKey(c.PreviousPSKBase64, c.ID, "psk_previous")
			if err != nil {
				return nil, err
			}
			set.credentials = append(set.credentials, Credential{ClientID: c.ID, Key: previous})
		}
	}

	// A file in which every client is revoked is not an error. It is the
	// state an operator is in the moment after withdrawing the last
	// credential, and refusing to load it would mean the revocation
	// silently did not happen — the previous set would stay in force and
	// the device being cut off would keep working.
	//
	// Refusing everyone is an outage, which is loud and noticed within
	// minutes. Continuing to admit a credential somebody just revoked is
	// silent, and revocation is most often reached for precisely when
	// silence is the thing that costs. Callers that want to treat an
	// empty set as a startup mistake can ask with Empty.
	return set, nil
}

// NewSetFromCredentials builds a set from keys that are already decoded.
// It is how a single shared key becomes an ordinary one-client set, so
// the shared and per-client configurations share one code path.
func NewSetFromCredentials(credentials []Credential) (*Set, error) {
	if len(credentials) == 0 {
		return nil, errors.New("clients: at least one credential is required")
	}
	set := &Set{
		credentials: append([]Credential(nil), credentials...),
		enabled:     make(map[string]bool),
	}
	for _, c := range credentials {
		if c.ClientID == "" {
			return nil, errors.New("clients: every credential needs a client id")
		}
		set.enabled[c.ClientID] = true
	}
	return set, nil
}

// Credentials returns the keys to try when authenticating a new peer,
// current keys first.
func (s *Set) Credentials() []Credential { return s.credentials }

// IsEnabled reports whether a client still has access. Live sessions are
// re-checked against this so a revocation reaches connections that are
// already established, not only new ones.
func (s *Set) IsEnabled(clientID string) bool { return s.enabled[clientID] }

// Count is the number of clients currently allowed in.
// Empty reports that no client currently has access, so every connection
// would be refused. Distinguishing this from a load failure is what lets
// a caller treat "the file is broken" and "everyone is revoked"
// differently, which they are.
func (s *Set) Empty() bool { return s == nil || len(s.credentials) == 0 }

func (s *Set) Count() int { return len(s.enabled) }

func decodeKey(b64, clientID, field string) ([32]byte, error) {
	var key [32]byte
	if b64 == "" {
		return key, fmt.Errorf("clients: client %q has no %s", clientID, field)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return key, fmt.Errorf("clients: client %q has an unreadable %s: %w", clientID, field, err)
	}
	if len(raw) != 32 {
		return key, fmt.Errorf("clients: client %q has a %s of %d bytes, want 32", clientID, field, len(raw))
	}
	copy(key[:], raw)
	return key, nil
}

// Registry holds the current credential set and swaps it atomically when
// the file is reloaded, so a reload never interrupts traffic in flight.
type Registry struct {
	path    string
	current atomic.Pointer[Set]
}

// NewRegistry loads the file and returns a registry serving it.
func NewRegistry(path string) (*Registry, error) {
	set, err := Load(path)
	if err != nil {
		return nil, err
	}
	r := &Registry{path: path}
	r.current.Store(set)
	return r, nil
}

// NewStaticRegistry serves a fixed set, for a single shared key or for
// tests.
func NewStaticRegistry(set *Set) *Registry {
	r := &Registry{}
	r.current.Store(set)
	return r
}

// Current returns the credential set in force right now.
func (r *Registry) Current() *Set { return r.current.Load() }

// Replace swaps in a credential set directly, without going through a
// file. Reloading is the usual path; this is what a caller uses when the
// set comes from somewhere else — a test, or a management API later.
func (r *Registry) Replace(set *Set) { r.current.Store(set) }

// Reload re-reads the file. On any error the previous set stays in force:
// a typo in the credential file must not lock every client out.
func (r *Registry) Reload() error {
	if r.path == "" {
		return errors.New("clients: this registry has no file to reload")
	}
	set, err := Load(r.path)
	if err != nil {
		return err
	}
	r.current.Store(set)
	return nil
}
