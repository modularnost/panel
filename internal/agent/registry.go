package agent

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"time"

	"modularnost/internal/docker"
)

type registryProbeRequest struct {
	Endpoint   string `json:"endpoint"`
	Image      string `json:"image"`
	SealedAuth []byte `json:"sealed_auth"`
}

func registryProbeCipher(secret string) cipher.AEAD {
	key := sha256.Sum256([]byte("panel:worker-registry-probe:" + secret))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCMWithRandomNonce(block)
	return aead
}

func registerRegistryProbeRoute(mux *http.ServeMux, dk *docker.Client, secret string, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /registry-probe", auth(func(w http.ResponseWriter, r *http.Request) {
		var request registryProbeRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		if err := dec.Decode(&request); err != nil || dec.Decode(new(any)) != io.EOF ||
			!docker.ValidRegistryProbe(request.Image, request.Endpoint) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		encoded, err := registryProbeCipher(secret).Open(nil, nil, request.SealedAuth, []byte(request.Image))
		if err != nil || len(encoded) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
		defer cancel()
		result, err := dk.RegistryProbe(ctx, request.Endpoint, request.Image, string(encoded))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
}

func (c *Collector) VerifyRegistry(ctx context.Context, addr netip.Addr, endpoint, image, encodedAuth string) (string, error) {
	request := registryProbeRequest{
		Endpoint: endpoint, Image: image,
		SealedAuth: registryProbeCipher(c.secret).Seal(nil, nil, []byte(encodedAuth), []byte(image)),
	}
	httpClient := *c.http
	httpClient.Timeout = 75 * time.Second
	var result string
	err := c.nodeRequest(ctx, &httpClient, addr, http.MethodPost, "/registry-probe", request, &result)
	return result, err
}
