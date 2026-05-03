package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
)

type testManager struct{}

func TestSingleInboundTopologyReality(t *testing.T) {
	topo := singleInboundTopology("reality-oneclick", "inbound-reality", 50123, map[string]interface{}{"tag": "reality-oneclick", "port": 50123, "uuid": "00000000-0000-0000-0000-000000000000", "private_key": "k", "short_id": "abcd", "dest": "www.microsoft.com"})
	cfg, err := singbox.CompileToSingbox(topo)
	if err != nil { t.Fatal(err) }
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Type != "vless" { t.Fatalf("bad inbound: %+v", cfg.Inbounds) }
	if len(cfg.Route.Rules) == 0 || cfg.Route.Rules[0].Outbound != "direct" { t.Fatalf("missing route: %+v", cfg.Route.Rules) }
}

func TestBuildShareLinksReadsOneClickConfig(t *testing.T) {
	path := "oneclick-config.json"
	defer os.Remove(path)
	cfg := `{"inbounds":[{"type":"hysteria2","tag":"hy2-oneclick","listen_port":50123,"users":[{"password":"pw"}]}]}`
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil { t.Fatal(err) }
	links, err := BuildShareLinks(path, "sbui.zzao.de")
	if err != nil { t.Fatal(err) }
	if len(links) != 1 || links[0] == "" { t.Fatalf("bad links: %#v", links) }
}

func TestQuickJSONShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/shape", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true, "type": "hy2", "port": 50123}) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shape", nil)
	r.ServeHTTP(w, req)
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil { t.Fatal(err) }
	if body["type"] != "hy2" { t.Fatalf("bad body: %v", body) }
}
