package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

type closeSignalListener struct {
	net.Listener
	closed chan struct{}
}

func (l closeSignalListener) Close() error {
	err := l.Listener.Close()
	if err == nil {
		close(l.closed)
	}
	return err
}

func TestHTTPShutdownDrainsAnActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	l := closeSignalListener{listener, make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{}, 1)
	defer close(release)
	done := make(chan error, 1)
	go func() {
		done <- serveHTTP(ctx, l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "first\n")
			_ = http.NewResponseController(w).Flush()
			close(entered)
			select {
			case <-r.Context().Done():
			case <-release:
				_, _ = io.WriteString(w, "last\n")
			}
		}))
	}()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach the server")
	}
	cancel()
	select {
	case <-l.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not stop accepting connections")
	}
	select {
	case err := <-done:
		t.Fatalf("server returned before draining the request: %v", err)
	default:
	}
	release <- struct{}{}
	data, err := io.ReadAll(resp.Body)
	if err != nil || string(data) != "first\nlast\n" {
		t.Fatalf("response: %q, %v", data, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not finish shutting down")
	}
}
