package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"modularnost/internal/agent"
	"modularnost/internal/docker"
	"modularnost/internal/modules"
	"modularnost/internal/store"
	"modularnost/internal/updates"
	"modularnost/internal/web"
)

// version is set at build time with -ldflags "-X main.version=..."; "dev" otherwise.
var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// bootstrapAdmin creates the first admin when there are no users: the password
// is PANEL_ADMIN_PASSWORD, or generated and logged once.
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

	// Agent mode: the same binary, one per node, only reads container stats.
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

	// Random per start by default: modules get the new token on the next discovery.
	secret := env("PANEL_MODULE_SECRET", store.NewToken())

	web.Version = version
	registry := modules.NewRegistry(dk, secret)
	// Gives modules their token without waiting for someone to open a page.
	go registry.Refresh(context.Background())

	checker := updates.New(version, os.Getenv("PANEL_UPDATE_CHECK") != "off")
	go checker.Run(context.Background(), 6*time.Hour)

	addr := env("PANEL_ADDR", ":8080")
	srv := &web.Server{
		Docker:       dk,
		DB:           db,
		Modules:      registry,
		Updates:      checker,
		AgentService: os.Getenv("PANEL_AGENT_SERVICE"),
		Metrics:      agent.NewCollector(os.Getenv("PANEL_AGENT_SERVICE"), env("PANEL_AGENT_PORT", "8080"), secret, dk),
		Addr:    addr,
		Docker:  dk,
		DB:      db,
		Modules: registry,
		Updates: checker,
		Metrics: agent.NewCollector(os.Getenv("PANEL_AGENT_SERVICE"), env("PANEL_AGENT_PORT", "8080"), secret, dk),
	}
	log.Printf("modularnost %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, srv.Routes()))
}
