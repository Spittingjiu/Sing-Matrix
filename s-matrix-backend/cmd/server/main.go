package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"s-matrix/api"
	"s-matrix/core/acme"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

// version is stamped at build time (-X main.version=...).
var version = "dev"

func main() {
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-version", "-v", "version":
			fmt.Printf("smatrix %s\n", version)
			return
		case "--help", "-h", "help":
			fmt.Println("smatrix - sing-box panel\n\nUsage: smatrix [--version]\nConfiguration is read from SMATRIX_* environment variables.")
			return
		}
	}

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

	// Repair rows written by older versions before the core is touched, so an
	// old node cannot block startup.
	api.MigrateLegacyInbounds(db, certDir)

	manager := singbox.NewSingboxManager(singboxBin, configPath, logPath)
	if raw, err := os.ReadFile(configPath); err == nil {
		// Self-heal: a config written by an older version may be rejected by the
		// core (for example hysteria2 without TLS). Recompile it from the DB so
		// the host does not stay offline after an upgrade.
		if cerr := singbox.CheckConfig(raw); cerr != nil {
			log.Printf("existing config rejected (%v); rebuilding from database", cerr)
			if rerr := api.RebuildConfig(db, manager, configPath); rerr != nil {
				log.Printf("rebuild failed: %v", rerr)
			} else {
				log.Printf("config rebuilt from database")
			}
		}
		if err := manager.Start(); err != nil {
			log.Printf("sing-box auto-start failed: %v", err)
		} else {
			log.Printf("sing-box auto-started with %s", configPath)
		}
	}

	engine := api.NewRouter(api.RouterDeps{Manager: manager, ConfigPath: configPath, LogPath: logPath, DB: db, CertDir: certDir, DataDir: dataDir, ACME: certMgr, PanelAddr: addr})

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
