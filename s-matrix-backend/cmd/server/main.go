package main

import (
	"log"
	"os"

	"s-matrix/api"
	"s-matrix/core/acme"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

func main() {
	dbPath := env("SMATRIX_DB", "s-matrix.db")
	addr := env("SMATRIX_ADDR", "127.0.0.1:19088")
	configPath := env("SMATRIX_CONFIG", "./config.json")
	logPath := env("SMATRIX_SINGBOX_LOG", "./singbox.log")
	certDir := env("SMATRIX_CERT_DIR", "./certs")
	dataDir := env("SMATRIX_DATA_DIR", "./data")
	acmeEmail := env("SMATRIX_ACME_EMAIL", "postmaster@zzao.de")
	singboxBin := env("SMATRIX_SINGBOX_BIN", "sing-box")
	db, err := models.OpenDatabase(dbPath)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	if err := singbox.GenerateTestConfigFile("config_test.json"); err != nil {
		log.Fatalf("generate test config failed: %v", err)
	}
	log.Printf("s-matrix backend listening on %s", addr)
	certMgr := acme.NewManager(certDir, dataDir, acmeEmail, func() string {
		if v := os.Getenv("SMATRIX_CF_TOKEN"); v != "" {
			return v
		}
		raw, err := os.ReadFile(dataDir + "/cf_token")
		if err != nil {
			return ""
		}
		return string(raw)
	})
	certMgr.StartRenewalLoop()

	manager := singbox.NewSingboxManager(singboxBin, configPath, logPath)
	if _, err := os.Stat(configPath); err == nil {
		if err := manager.Start(); err != nil {
			log.Printf("sing-box auto-start failed: %v", err)
		} else {
			log.Printf("sing-box auto-started with %s", configPath)
		}
	}
	if err := api.NewRouter(api.RouterDeps{Manager: manager, ConfigPath: configPath, LogPath: logPath, DB: db, CertDir: certDir, DataDir: dataDir, ACME: certMgr}).Run(addr); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
