package main

import (
	"log"
	"os"

	"modularnost/internal/healthcheck"
)

func main() {
	addr := os.Getenv("PANEL_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if err := healthcheck.Check(os.Getenv("PANEL_MODE"), addr); err != nil {
		log.Fatal(err)
	}
}
