package web

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"modularnost/internal/store"
)

type apiUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func toAPIUser(u store.User) apiUser {
	return apiUser{ID: u.ID, Username: u.Username, Role: u.Role}
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]map[string]string{"error": {"code": code, "message": msg}})
}

// jsonOnly refuses state-changing requests that are not JSON. A plain HTML form
// cannot send application/json, so this is the CSRF guard for cookie sessions.
func jsonOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "send application/json")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) apiRequire(need string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, status, msg := s.authorize(r, need)
		if status == http.StatusUnauthorized {
			writeAPIError(w, status, "unauthorized", msg)
			return
		}
		if status != 0 {
			writeAPIError(w, status, "forbidden", msg)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	writeAPIError(w, http.StatusNotFound, "not_found", "no such endpoint")
}

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "expected {\"username\", \"password\"}")
		return
	}
	u, err := s.DB.Authenticate(in.Username, in.Password)
	if errors.Is(err, store.ErrNoUser) {
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
		return
	}
	if err == nil {
		err = s.startSession(w, r, u.ID)
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, toAPIUser(u))
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.endSession(w, r); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, toAPIUser(userFrom(r)))
}
