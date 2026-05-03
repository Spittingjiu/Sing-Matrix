package main

import (
	"log"
	"os"

	"github.com/Spittingjiu/Sing-Matrix/backend/internal/api"
	"github.com/Spittingjiu/Sing-Matrix/backend/internal/database"
)

func main() {
	addr := env("SMATRIX_ADDR", "127.0.0.1:18088")
	dbPath := env("SMATRIX_DB", "./data/smatrix.db")
	singboxConfig := env("SMATRIX_SINGBOX_CONFIG", "./data/sing-box/config.json")

	db, err := database.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	router := api.NewRouter(api.Dependencies{
		DB:              db,
		SingBoxConfig:   singboxConfig,
		SingBoxBin:      env("SMATRIX_SINGBOX_BIN", "sing-box"),
		GeneratedConfig: env("SMATRIX_GENERATED_CONFIG", "./data/generated/config.json"),
	})

	log.Printf("S-Matrix listening on http://%s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func env(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
