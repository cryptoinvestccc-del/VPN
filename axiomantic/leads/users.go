package main

import (
	"errors"
	"net/http"
	"strings"
)

// Team management from the panel, for the owner of a workspace: add a
// person, change their role, disable them, give them a new password, or
// remove them. Every user id comes from the browser and is looked up in
// the owner's own workspace only. An owner cannot lock themself out.

func (a *Admin) userRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/users", a.auth(roleOwner, a.handleAddUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", a.auth(roleOwner, a.handleChangeUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", a.auth(roleOwner, a.handleRemoveUser))
	mux.HandleFunc("PUT /api/admin/me/password", a.auth(roleViewer, a.handleOwnPassword))
}

var errNotFound = errors.New("not found")

func memberView(u *User) map[string]any {
	return map[string]any{"id": u.ID, "name": u.Name, "login": u.Login, "role": u.Role, "initials": initials(u.Name), "active": !u.Disabled}
}

func (a *Admin) handleAddUser(w http.ResponseWriter, r *http.Request, ac *actor) {
	var body struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}
	if code, e := decodeJSON(w, r, 4<<10, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	login, name := strings.TrimSpace(body.Login), strings.TrimSpace(body.Name)
	switch {
	case !between(login, 3, 120) || strings.ContainsAny(login, " \t\r\n") || !label(login, 3, 120):
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "login"})
		return
	case !label(name, 1, 80):
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "name"})
		return
	case roleRank[body.Role] == 0:
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "role"})
		return
	}
	pw := randomToken(15)
	hash, err := HashPassword(pw)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	u := &User{ID: newUserID(), Login: login, Name: name, Role: body.Role, Workspace: ac.ws.ID, Hash: hash}
	err = a.cfg.Update(func(c *AdminConfig) error {
		if c.UserByLogin(login) != nil {
			return errDuplicate
		}
		c.Users = append(c.Users, u)
		return nil
	})
	if errors.Is(err, errDuplicate) {
		// logins are unique across every workspace on this server
		adminJSON(w, http.StatusConflict, apiError{Error: "duplicate", Field: "login"})
		return
	}
	if err != nil {
		a.log.Printf("admin: add user: %v", err)
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.log.Printf("admin: %s added user %s to %s", ac.user.ID, u.ID, ac.ws.ID)
	adminJSON(w, http.StatusOK, map[string]any{"user": memberView(u), "password": pw})
}

var errDuplicate = errors.New("duplicate login")

func (a *Admin) handleChangeUser(w http.ResponseWriter, r *http.Request, ac *actor) {
	id := r.PathValue("id")
	var body struct {
		Name          *string `json:"name"`
		Role          *string `json:"role"`
		Disabled      *bool   `json:"disabled"`
		ResetPassword bool    `json:"resetPassword"`
	}
	if code, e := decodeJSON(w, r, 4<<10, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	if body.Name != nil && !label(strings.TrimSpace(*body.Name), 1, 80) {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "name"})
		return
	}
	if body.Role != nil && roleRank[*body.Role] == 0 {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "role"})
		return
	}
	if id == ac.user.ID && ((body.Role != nil && *body.Role != roleOwner) || (body.Disabled != nil && *body.Disabled)) {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "self"})
		return
	}
	var pw, hash string
	if body.ResetPassword {
		pw = randomToken(15)
		var err error
		if hash, err = HashPassword(pw); err != nil {
			adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
			return
		}
	}
	var out *User
	err := a.cfg.Update(func(c *AdminConfig) error {
		u := c.User(id)
		if u == nil || u.Workspace != ac.ws.ID {
			return errNotFound
		}
		if body.Name != nil {
			u.Name = strings.TrimSpace(*body.Name)
		}
		if body.Role != nil {
			u.Role = *body.Role
		}
		if body.Disabled != nil {
			u.Disabled = *body.Disabled
		}
		if hash != "" {
			u.Hash = hash
		}
		cp := *u
		out = &cp
		return nil
	})
	if errors.Is(err, errNotFound) {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	if err != nil {
		a.log.Printf("admin: change user: %v", err)
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.log.Printf("admin: %s changed user %s", ac.user.ID, id)
	res := map[string]any{"user": memberView(out)}
	if pw != "" {
		res["password"] = pw
	}
	adminJSON(w, http.StatusOK, res)
}

func (a *Admin) handleRemoveUser(w http.ResponseWriter, r *http.Request, ac *actor) {
	id := r.PathValue("id")
	if id == ac.user.ID {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "self"})
		return
	}
	err := a.cfg.Update(func(c *AdminConfig) error {
		for i, u := range c.Users {
			if u.ID == id && u.Workspace == ac.ws.ID {
				c.Users = append(c.Users[:i], c.Users[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	if errors.Is(err, errNotFound) {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.log.Printf("admin: %s removed user %s", ac.user.ID, id)
	adminJSON(w, http.StatusOK, reply{OK: true})
}

// handleOwnPassword lets anyone change their own password, knowing the
// current one. Other sessions of the person end; this browser gets a new one.
func (a *Admin) handleOwnPassword(w http.ResponseWriter, r *http.Request, ac *actor) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if code, e := decodeJSON(w, r, 4<<10, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	now := a.now()
	key := "pw\x00" + ac.user.ID
	if ok, retry := a.loginFail.Allow(key, now); !ok {
		tooMany(w, retry)
		return
	}
	if len([]rune(body.New)) < minPassLen || len(body.New) > 256 {
		a.loginFail.Refund(key)
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "new"})
		return
	}
	slot, ok := a.takeSlot(r, true)
	if !ok {
		a.loginFail.Refund(key)
		adminJSON(w, http.StatusServiceUnavailable, apiError{Error: "busy"})
		return
	}
	good := CheckPassword(ac.user.Hash, body.Current)
	<-slot
	if !good {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "current"})
		return
	}
	a.loginFail.Refund(key)
	if body.New == body.Current {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "new"})
		return
	}
	hash, err := HashPassword(body.New)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	var updated *User
	err = a.cfg.Update(func(c *AdminConfig) error {
		u := c.User(ac.user.ID)
		if u == nil {
			return errNotFound
		}
		u.Hash = hash
		cp := *u
		updated = &cp
		return nil
	})
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	// The reload ended every session of this user, this one included.
	token, _ := a.sessions.New(updated, now)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", MaxAge: int(sessionMaxAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	a.log.Printf("admin: %s changed their password", ac.user.ID)
	adminJSON(w, http.StatusOK, reply{OK: true})
}
