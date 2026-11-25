package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"

	"github.com/containerd/errdefs"

	"modularnost/internal/docker"
)

type NetworkCreated struct {
	ID string `json:"id"`
}

func localOperationResponse(w http.ResponseWriter, value any, err error) {
	if err != nil {
		switch {
		case errdefs.IsNotFound(err):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, docker.ErrProtectedNetwork):
			w.WriteHeader(http.StatusForbidden)
		case errors.Is(err, docker.ErrNetworkConflict), errdefs.IsConflict(err), errdefs.IsFailedPrecondition(err):
			w.WriteHeader(http.StatusConflict)
		case errors.Is(err, docker.ErrInvalidNetwork), errdefs.IsInvalidArgument(err):
			w.WriteHeader(http.StatusUnprocessableEntity)
		default:
			log.Printf("agent local operation: %v", err)
			w.WriteHeader(http.StatusBadGateway)
		}
		return
	}
	if value == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func registerNetworkRoutes(mux *http.ServeMux, dk *docker.Client, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /networks", auth(func(w http.ResponseWriter, r *http.Request) {
		rows, err := dk.LocalNetworks(r.Context())
		localOperationResponse(w, rows, err)
	}))
	mux.HandleFunc("GET /networks/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		n, err := dk.LocalNetwork(r.Context(), r.PathValue("id"))
		localOperationResponse(w, n, err)
	}))
	mux.HandleFunc("POST /networks", auth(func(w http.ResponseWriter, r *http.Request) {
		var in docker.NewLocalNetwork
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := dk.CreateLocalNetwork(r.Context(), in)
		localOperationResponse(w, NetworkCreated{ID: id}, err)
	}))
	mux.HandleFunc("DELETE /networks/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		localOperationResponse(w, nil, dk.RemoveLocalNetwork(r.Context(), r.PathValue("id")))
	}))
}

func (c *Collector) LocalRequest(ctx context.Context, addr netip.Addr, method, path string, input, output any) error {
	return c.nodeRequest(ctx, c.http, addr, method, path, input, output)
}

func (c *Collector) nodeRequest(ctx context.Context, httpClient *http.Client, addr netip.Addr, method, path string, input, output any) error {
	if !addr.IsValid() {
		return errors.New("node agent unavailable")
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+net.JoinHostPort(addr.String(), c.port)+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Panel-Token", Token(c.secret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(output)
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return errdefs.ErrNotFound
	case http.StatusForbidden:
		return docker.ErrProtectedNetwork
	case http.StatusConflict:
		return docker.ErrNetworkConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return docker.ErrInvalidNetwork
	default:
		return fmt.Errorf("node agent request: %s", resp.Status)
	}
}
