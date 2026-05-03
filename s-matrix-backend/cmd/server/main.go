package main

import (
	"log"
	"os"

	"s-matrix/api"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

func main() {
	dbPath := env("SMATRIX_DB", "s-matrix.db")
	addr := env("SMATRIX_ADDR", "127.0.0.1:19088")
	if _, err := models.OpenDatabase(dbPath); err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	if err := singbox.GenerateTestConfigFile("config_test.json"); err != nil {
		log.Fatalf("generate test config failed: %v", err)
	}
	log.Printf("s-matrix backend listening on %s", addr)
	if err := api.NewRouter().Run(addr); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
