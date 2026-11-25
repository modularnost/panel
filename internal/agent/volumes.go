package agent

import (
	"net/http"

	"modularnost/internal/docker"
)

func registerVolumeRoutes(mux *http.ServeMux, dk *docker.Client, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /volumes", auth(func(w http.ResponseWriter, r *http.Request) {
		volumes, err := dk.LocalVolumes(r.Context())
		localOperationResponse(w, volumes, err)
	}))
	mux.HandleFunc("GET /volumes/{name}", auth(func(w http.ResponseWriter, r *http.Request) {
		volume, err := dk.LocalVolume(r.Context(), r.PathValue("name"))
		localOperationResponse(w, volume, err)
	}))
	mux.HandleFunc("PUT /volumes/{name}", auth(func(w http.ResponseWriter, r *http.Request) {
		volume, err := dk.CreateLocalVolume(r.Context(), r.PathValue("name"))
		localOperationResponse(w, volume, err)
	}))
	mux.HandleFunc("DELETE /volumes/{name}", auth(func(w http.ResponseWriter, r *http.Request) {
		localOperationResponse(w, nil, dk.RemoveLocalVolume(r.Context(), r.PathValue("name")))
	}))
}
