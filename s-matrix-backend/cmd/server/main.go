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
	configPath := env("SMATRIX_CONFIG", "./config.json")
	logPath := env("SMATRIX_SINGBOX_LOG", "./singbox.log")
	singboxBin := env("SMATRIX_SINGBOX_BIN", "sing-box")
	if _, err := models.OpenDatabase(dbPath); err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	if err := singbox.GenerateTestConfigFile("config_test.json"); err != nil {
		log.Fatalf("generate test config failed: %v", err)
	}
	log.Printf("s-matrix backend listening on %s", addr)
	manager := singbox.NewSingboxManager(singboxBin, configPath, logPath)
	if _, err := os.Stat(configPath); err == nil {
		if err := manager.Start(); err != nil {
			log.Printf("sing-box auto-start failed: %v", err)
		} else {
			log.Printf("sing-box auto-started with %s", configPath)
		}
	}
	if err := api.NewRouter(staticFiles, api.RouterDeps{Manager: manager, ConfigPath: configPath, LogPath: logPath}).Run(addr); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
