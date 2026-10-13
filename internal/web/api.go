package web

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"modularnost/internal/docker"
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

type apiService struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Stack   string            `json:"stack,omitempty"`
	Image   string            `json:"image"`
	Digest  string            `json:"digest,omitempty"`
	Mode    string            `json:"mode"`
	Running uint64            `json:"running"`
	Desired uint64            `json:"desired"`
	Healthy bool              `json:"healthy"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// toAPIService leaves labels out of the list: there can be dozens per service,
// and only the detail view needs them.
func toAPIService(sv docker.Service, withLabels bool) apiService {
	out := apiService{
		ID: sv.ID, Name: sv.Name, Stack: sv.Stack, Image: sv.Image, Digest: sv.Digest,
		Mode: sv.Mode, Running: sv.Running, Desired: sv.Desired, Healthy: sv.Healthy(),
	}
	if withLabels {
		out.Labels = sv.Labels
	}
	return out
}
