//go:build unix

package main

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The service runs with UMask=0077 (deploy/axm-site.service) and nginx,
// another user, serves what it publishes: every directory on the way to a
// page and every file of a release must still be readable by others.
func TestPublishReadableUnderStrictUmask(t *testing.T) {
	h := newSiteHarness(t)
	owner := h.login("owner@alpha.example")
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	if rec := owner.do("POST", "/api/admin/site/publish", map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body)
	}
	root := h.site("alpha").Root
	cur, err := filepath.EvalSymlinks(filepath.Join(root, "current"))
	if err != nil {
		t.Fatal(err)
	}
	for p := cur; p != root; p = filepath.Dir(p) {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm()&0o005 != 0o005 {
			t.Fatalf("%s is not open to the web server: %v %v", p, st.Mode(), err)
		}
	}
	files := 0
	err = filepath.WalkDir(cur, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		need := fs.FileMode(0o004)
		if d.IsDir() {
			need = 0o005
		} else {
			files++
		}
		if st.Mode().Perm()&need != need {
			t.Errorf("%s: mode %v, the web server cannot read it", p, st.Mode().Perm())
		}
		return nil
	})
	if err != nil || files < 20 {
		t.Fatalf("walk: %v, %d files", err, files)
	}
}
