package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostUsageRequiresAgentToken(t *testing.T) {
	handler := Routes("agent-secret", "node", nil)
	for _, tt := range []struct {
		token string
		code  int
	}{
		{"", http.StatusUnauthorized},
		{Token("other-secret"), http.StatusUnauthorized},
		{Token("agent-secret"), http.StatusOK},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/host-usage", nil)
		req.Header.Set("X-Panel-Token", tt.token)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tt.code {
			t.Fatalf("got status %d, want %d", res.Code, tt.code)
		}
	}
}
