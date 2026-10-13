package web

import (
	"encoding/json"
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

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	writeAPIError(w, http.StatusNotFound, "not_found", "no such endpoint")
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
