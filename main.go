package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
	"modularnost/internal/web"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=$(git describe --tags --always)"
//
// Left as "dev" for a plain `go build`, which is honest — that binary came from
// a working tree, not a release.
var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// bootstrapAdmin creates the first admin if there are no users yet.
// The password comes from PANEL_ADMIN_PASSWORD, or is generated and printed to
// the log once — the panel never comes up without a way to log in.
func bootstrapAdmin(db *store.DB) error {
	n, err := db.CountUsers()
	if err != nil || n > 0 {
		return err
	}
	password := os.Getenv("PANEL_ADMIN_PASSWORD")
	generated := password == ""
	if generated {
		password = store.NewToken()[:16]
	}
	u, err := db.CreateUser(env("PANEL_ADMIN_USER", "admin"), password, store.RoleAdmin)
	if err != nil {
		return err
	}
	if generated {
		log.Printf("created user %q with password: %s (change it after logging in)", u.Username, password)
	} else {
		log.Printf("created user %q", u.Username)
	}
	return nil
}

func main() {
	dk, err := docker.New()
	if err != nil {
		log.Fatalf("docker: %v", err)
	}
	defer dk.Close()

	// Agent mode: same binary, deployed as a global service so every node has
	// one. It only reads container stats and answers the panel.
	if os.Getenv("PANEL_MODE") == "agent" {
		node := os.Getenv("PANEL_NODE")
		if node == "" {
			if node, err = dk.NodeName(context.Background()); err != nil {
				log.Fatalf("node name: %v", err)
			}
		}
		secret := os.Getenv("PANEL_MODULE_SECRET")
		if secret == "" {
			log.Fatal("PANEL_MODULE_SECRET is required in agent mode: the panel and its agents must share it")
		}
		addr := env("PANEL_ADDR", ":8080")
		log.Printf("modularnost %s: agent for node %q listening on %s", version, node, addr)
		log.Fatal(agent.Serve(addr, secret, node, dk))
	}

	db, err := store.Open(env("PANEL_DB", "modularnost.db"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	if err := db.PurgeExpiredTokens(); err != nil {
		log.Printf("purging expired tokens: %v", err)
	}
	if err := bootstrapAdmin(db); err != nil {
		log.Fatalf("first admin: %v", err)
	}

	// Secret for module tokens. New on every start by default: modules are
	// handed the resulting token on the next discovery pass, so there is
	// nothing to persist and no reason to.
	secret := env("PANEL_MODULE_SECRET", store.NewToken())

	web.Version = version
	registry := modules.NewRegistry(dk, secret)
	// Hands every module its token within a TTL of the panel starting, without
	// waiting for someone to open a page.
	go registry.Refresh(context.Background())

	srv := &web.Server{
		Docker:  dk,
		DB:      db,
		Modules: registry,
		Metrics: agent.NewCollector(os.Getenv("PANEL_AGENT_SERVICE"), env("PANEL_AGENT_PORT", "8080"), secret, dk),
	}
	addr := env("PANEL_ADDR", ":8080")
	log.Printf("modularnost %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, srv.Routes()))
}
