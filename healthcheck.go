package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

func checkHealth(mode, addr string) error {
	path := "/healthz"
	switch mode {
	case "firewall", "firewall-clear":
		return nil
	case "agent":
		path = "/health"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("health check address: %w", err)
	}
	switch host {
	case "":
		host = "localhost"
	case "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	target := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: path}
	client := http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(target.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: %s", resp.Status)
	}
	return nil
}
