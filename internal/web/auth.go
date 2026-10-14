package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"modularnost/internal/store"
)

const (
	sessionCookie = "panel_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type ctxKey struct{}

func userFrom(r *http.Request) store.User {
	u, _ := r.Context().Value(ctxKey{}).(store.User)
	return u
}

// identify resolves the user from a session cookie, a Bearer token or basic
// auth. Basic and Bearer are there for CI and curl, which have no cookie jar.
func (s *Server) identify(r *http.Request) (store.User, bool) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, err := s.DB.UserByToken(c.Value); err == nil {
			return u, true
		}
	}
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if u, err := s.DB.UserByToken(token); err == nil {
			return u, true
		}
	}
	if name, pass, ok := r.BasicAuth(); ok {
		if u, err := s.DB.Authenticate(name, pass); err == nil {
			return u, true
		}
	}
	return store.User{}, false
}

// authorize returns the caller, or a non-zero status and message to refuse with.
func (s *Server) authorize(r *http.Request, need string) (store.User, int, string) {
	u, ok := s.identify(r)
	if !ok {
		return u, http.StatusUnauthorized, "log in first"
	}
	if !store.AtLeast(u.Role, need) {
		return u, http.StatusForbidden, "role " + need + " required, you have " + u.Role
	}
	return u, 0, ""
}

func (s *Server) require(need string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, status, msg := s.authorize(r, need)
		if status != 0 {
			s.denied(w, r, status, msg)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (s *Server) denied(w http.ResponseWriter, r *http.Request, code int, msg string) {
	browser := strings.Contains(r.Header.Get("Accept"), "text/html")
	if code == http.StatusUnauthorized && browser && r.Method == http.MethodGet {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	http.Error(w, msg, code)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "login.html", r.FormValue("next"))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	u, err := s.DB.Authenticate(r.FormValue("username"), r.FormValue("password"))
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, "login.html", "")
		return
	}
	c, err := s.sessionCookie(r, u.ID)
	if err != nil {
		fail(w, err)
		return
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, safeNext(r.FormValue("next")), http.StatusSeeOther)
}

func (s *Server) sessionCookie(r *http.Request, userID int64) (*http.Cookie, error) {
	token, err := s.DB.CreateToken(userID, "", "", sessionTTL)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		// TLS ends at the proxy, so the panel itself usually sees plain HTTP.
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		// Lax blocks cross-site POSTs, so no separate CSRF tokens are needed.
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	}, nil
}

func (s *Server) revokeSession(r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return s.DB.DeleteTokenByValue(c.Value)
	}
	return nil
}

func clearedCookie() *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1}
}

// safeNext keeps the post-login redirect on this site: "//host" and "/\host"
// start with a slash too, but browsers read them as another origin.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.revokeSession(r); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, clearedCookie())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type usersPage struct {
	Me       store.User
	Users    []store.User
	Tokens   []store.Token
	NewToken string // shown exactly once, right after creation
}

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	s.renderUsers(w, r, "", "users.html")
}

// renderFragment answers a mutation: htmx swaps just the list.
func (s *Server) renderFragment(w http.ResponseWriter, r *http.Request, newToken string) {
	s.renderUsers(w, r, newToken, "userlist")
}

func (s *Server) renderUsers(w http.ResponseWriter, r *http.Request, newToken, tmpl string) {
	users, err := s.DB.Users()
	if err != nil {
		fail(w, err)
		return
	}
	tokens, err := s.DB.Tokens()
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, tmpl, usersPage{userFrom(r), users, tokens, newToken})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	_, err := s.DB.CreateUser(r.FormValue("username"), r.FormValue("password"), r.FormValue("role"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderFragment(w, r, "")
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.guardLastAdmin(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.DB.DeleteUser(id); err != nil {
		fail(w, err)
		return
	}
	s.renderFragment(w, r, "")
}

func (s *Server) setUserRole(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	role := r.FormValue("role")
	if role != store.RoleAdmin {
		if err := s.guardLastAdmin(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := s.DB.SetRole(id, role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderFragment(w, r, "")
}

func (s *Server) setUserPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.changePassword(id, r.FormValue("password")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderFragment(w, r, "")
}

// changePassword also ends the user's sessions: a password that was stolen
// should not keep working through a session opened with it.
func (s *Server) changePassword(id int64, password string) error {
	if err := s.DB.SetPassword(id, password); err != nil {
		return err
	}
	return s.DB.RevokeSessions(id)
}

func (s *Server) guardLastAdmin(id int64) error {
	last, err := s.DB.IsLastAdmin(id)
	if err != nil {
		return err
	}
	if last {
		return errors.New("this is the last admin — appoint another one first")
	}
	return nil
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	userID, _ := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "a token needs a name", http.StatusBadRequest)
		return
	}
	var ttl time.Duration
	if days, _ := strconv.Atoi(r.FormValue("days")); days > 0 {
		ttl = time.Duration(days) * 24 * time.Hour
	}
	token, err := s.DB.CreateToken(userID, name, r.FormValue("role"), ttl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderFragment(w, r, token)
}

func (s *Server) deleteAPIToken(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.DB.DeleteToken(id); err != nil {
		fail(w, err)
		return
	}
	s.renderFragment(w, r, "")
}
