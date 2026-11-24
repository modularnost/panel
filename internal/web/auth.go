package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"modularnost/internal/store"
)

const (
	sessionCookie     = "panel_session"
	sessionTTL        = 30 * 24 * time.Hour
	maxPasswordChecks = 4
)

var errPasswordChecksBusy = errors.New("password checks are busy")

func (s *Server) authenticatePassword(r *http.Request, name, pass string) (store.User, time.Duration, error) {
	ip, user := loginKeys(r, s.TrustedProxies, name)
	finish, wait := s.logins.reserve(ip, user)
	if wait > 0 {
		return store.User{}, wait, nil
	}
	if s.passwordChecks.Add(1) > maxPasswordChecks {
		s.passwordChecks.Add(-1)
		finish(false, false)
		return store.User{}, 0, errPasswordChecksBusy
	}
	defer s.passwordChecks.Add(-1)
	u, err := s.DB.Authenticate(name, pass)
	finish(errors.Is(err, store.ErrNoUser), err == nil)
	return u, 0, err
}

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
	if name, pass, ok := r.BasicAuth(); ok {
		u, wait, err := s.authenticatePassword(r, name, pass)
		if wait == 0 && err == nil {
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

func (s *Server) sessionCookie(r *http.Request, userID int64) (*http.Cookie, error) {
	token, _, err := s.DB.CreateToken(userID, "", "", sessionTTL)
	if err != nil {
		return nil, err
	}
	c := s.sessionCookieAttrs(r)
	c.Value = token
	c.Expires = time.Now().Add(sessionTTL)
	return c, nil
}

func (s *Server) sessionCookieAttrs(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Path:     "/",
		HttpOnly: true,
		Secure: r.TLS != nil || (isTrusted(peerAddr(r.RemoteAddr), s.TrustedProxies) &&
			r.Header.Get("X-Forwarded-Proto") == "https"),
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) revokeSession(r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return s.DB.DeleteTokenByValue(c.Value)
	}
	return nil
}

// changePassword also ends the user's sessions: a password that was stolen
// should not keep working through a session opened with it.
func (s *Server) changePassword(id int64, password string) error {
	if err := s.DB.SetPassword(id, password); err != nil {
		return err
	}
	return s.DB.RevokeSessions(id)
}
