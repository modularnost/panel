package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"modularnost/internal/store"
)

const (
	sessionCookie = "panel_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type ctxKey struct{}

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
	if name, pass, ok := r.BasicAuth(); ok && s.loginBlocked(r, name) == 0 {
		// The same counters as the sign-in form: this is a password guess too.
		if u, err := s.DB.Authenticate(name, pass); err == nil {
			s.loginSucceeded(r, name)
			return u, true
		}
		s.loginFailed(r, name)
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
