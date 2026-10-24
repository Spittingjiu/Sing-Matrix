package main

import (
	"log"
	"net/http"
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
	acmeEmail := env("SMATRIX_ACME_EMAIL", "")
	webroot := env("SMATRIX_ACME_WEBROOT", "./acme-webroot")
	httpsAddr := env("SMATRIX_HTTPS_ADDR", "")
	httpAddr := env("SMATRIX_HTTP_ADDR", "")
	singboxBin := env("SMATRIX_SINGBOX_BIN", "sing-box")
	db, err := models.OpenDatabase(dbPath)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	if err := singbox.GenerateTestConfigFile("config_test.json"); err != nil {
		log.Fatalf("generate test config failed: %v", err)
	}
	log.Printf("s-matrix backend listening on %s", addr)

	// Certificates are obtained over HTTP-01 only: the operator points a domain
	// at this host, and the panel proves control by serving a token on port 80.
	// No DNS provider credential is ever stored.
	certMgr := acme.NewManager(certDir, dataDir, acmeEmail, webroot)
	certMgr.HTTPSListen = httpsAddr
	certMgr.StartRenewalLoop()

	manager := singbox.NewSingboxManager(singboxBin, configPath, logPath)
	if _, err := os.Stat(configPath); err == nil {
		if err := manager.Start(); err != nil {
			log.Printf("sing-box auto-start failed: %v", err)
		} else {
			log.Printf("sing-box auto-started with %s", configPath)
		}
	}

	engine := api.NewRouter(api.RouterDeps{Manager: manager, ConfigPath: configPath, LogPath: logPath, DB: db, CertDir: certDir, DataDir: dataDir, ACME: certMgr})

	// Optional built-in TLS: when SMATRIX_HTTPS_ADDR is set (e.g. ":443") the
	// panel serves itself with the certificates it issued, and SMATRIX_HTTP_ADDR
	// (e.g. ":80") answers ACME challenges plus redirects to HTTPS.
	if httpsAddr != "" {
		tlsProvider := acme.NewTLSProvider(certDir)
		httpsSrv := &http.Server{Addr: httpsAddr, Handler: engine, TLSConfig: tlsProvider.TLSConfig()}
		go func() {
			log.Printf("[panel-tls] serving https on %s", httpsAddr)
			if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Printf("[panel-tls] https listener stopped: %v", err)
			}
		}()
		if httpAddr != "" {
			challenge := &acme.WebrootProvider{Dir: webroot}
			httpSrv := &http.Server{Addr: httpAddr, Handler: tlsProvider.RedirectHandler(challenge)}
			go func() {
				log.Printf("[panel-tls] serving http redirect + acme challenge on %s", httpAddr)
				if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("[panel-tls] http listener stopped: %v", err)
				}
			}()
		}
	}

	if err := engine.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
