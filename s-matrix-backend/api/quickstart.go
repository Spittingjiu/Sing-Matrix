package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"s-matrix/core/acme"
	"s-matrix/core/httpsite"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

type quickDeps struct {
	Manager    *singbox.SingboxManager
	ConfigPath string
	DB         *gorm.DB
	CertDir    string
	DataDir    string
	ACME       *acme.Manager
	PanelAddr  string
}

// defaultRealityDest must be a site that speaks TLS 1.3 + HTTP/2 from this host.
// www.microsoft.com is fronted by an edge that makes REALITY fail the handshake
// (clients see EOF), so it must not be the default.
const defaultRealityDest = "www.cloudflare.com"

func QuickRealityHandler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Remark string `json:"remark"`
			Dest   string `json:"dest"`
			SNI    string `json:"sni"`
		}
		_ = c.ShouldBindJSON(&req)
		remark := strings.TrimSpace(req.Remark)
		dest := strings.TrimSpace(req.Dest)
		if dest == "" {
			dest = strings.TrimSpace(req.SNI)
		}
		if dest == "" {
			// www.microsoft.com is served by an edge that fails REALITY
			// handshake validation here (clients get EOF). Cloudflare's own
			// site is TLS 1.3 + H2 clean and is the safest default.
			dest = defaultRealityDest
		}

		keys := generateRealityMaterial("/usr/local/bin/sing-box")
		port := singbox.PickAvailablePort(0, map[int]bool{})
		if port == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no available port"})
			return
		}
		if remark == "" {
			remark = fmt.Sprintf("REALITY :%d", port)
		}
		topo := singleInboundTopology("reality-oneclick", "inbound-reality", port, remark, map[string]interface{}{
			"tag":         remark,
			"port":        port,
			"uuid":        randomUUIDLike(),
			"dest":        dest,
			"server_name": dest,
			"short_id":    keys["short_id"],
			"private_key": keys["private_key"],
			"public_key":  keys["public_key"],
		})
		respondQuick(c, dep, topo, "reality", port)
	}
}

func QuickHY2Handler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Remark string `json:"remark"`
		}
		_ = c.ShouldBindJSON(&req)
		remark := strings.TrimSpace(req.Remark)

		port := singbox.PickAvailablePort(0, map[int]bool{})
		if port == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no available port"})
			return
		}
		if remark == "" {
			remark = fmt.Sprintf("HY2 :%d", port)
		}
		// Hysteria2 is QUIC+TLS: without a certificate sing-box refuses to start,
		// which would take every other node on the host down with it.
		certName := fmt.Sprintf("hy2-%d", port)
		certPath, keyPath, _, err := singbox.EnsureSelfSignedPair(resolveCertDir(dep.CertDir), certName, "www.bing.com", singbox.LocalIPs(), []string{"www.bing.com"})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成自签证书失败: " + err.Error()})
			return
		}
		topo := singleInboundTopology("hy2-oneclick", "inbound-hy2", port, remark, map[string]interface{}{
			"tag":              remark,
			"port":             port,
			"password":         randomHex(28),
			"masquerade":       "https://www.bing.com",
			"server_name":      "www.bing.com",
			"sni":              "www.bing.com",
			"certificate_path": certPath,
			"key_path":         keyPath,
		})
		respondQuick(c, dep, topo, "hy2", port)
	}
}

// QuickAnyTLSHandler creates an AnyTLS inbound with a panel-managed self-signed
// certificate. AnyTLS requires TLS, and self-signed keeps it usable without a
// domain or ACME flow; clients connect with skip-cert-verify.
func QuickAnyTLSHandler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Remark string `json:"remark"`
			Port   int    `json:"port"`
			SNI    string `json:"sni"`
			Name   string `json:"name"`
		}
		_ = c.ShouldBindJSON(&req)
		remark := strings.TrimSpace(req.Remark)
		sni := strings.TrimSpace(req.SNI)
		if sni == "" {
			sni = "www.bing.com"
		}

		port := singbox.PickAvailablePort(req.Port, map[int]bool{})
		if port == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no available port"})
			return
		}
		if remark == "" {
			remark = fmt.Sprintf("AnyTLS :%d", port)
		}

		certName := strings.TrimSpace(req.Name)
		if certName == "" {
			certName = fmt.Sprintf("anytls-%d", port)
		}
		certPath, keyPath, created, err := singbox.EnsureSelfSignedPair(resolveCertDir(dep.CertDir), certName, sni, singbox.LocalIPs(), []string{sni})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate self-signed certificate failed: " + err.Error()})
			return
		}

		password := randomPassword(24)
		topo := singleInboundTopology("anytls-oneclick", "inbound-anytls", port, remark, map[string]interface{}{
			"tag":              remark,
			"port":             port,
			"password":         password,
			"server_name":      sni,
			"sni":              sni,
			"certificate_path": certPath,
			"key_path":         keyPath,
		})
		respondQuick(c, dep, topo, "anytls", port)
		_ = created
	}
}

func respondQuick(c *gin.Context, dep quickDeps, topo singbox.UIData, typ string, port int, extra ...gin.H) {
	_ = writeClientMeta(dep.ConfigPath, topo)
	// Persist first: the compiler must always run against the full desired
	// state (every enabled inbound), otherwise creating a second node would
	// silently drop the previous one from sing-box while leaving it in SQLite.
	if dep.DB != nil {
		for _, n := range topo.Nodes {
			sbType := quickDBType(n.Kind)
			if sbType == "" {
				continue
			}
			pTag, _ := n.Data["tag"].(string)
			payload := map[string]interface{}{}
			for _, k := range []string{"private_key", "public_key", "short_id", "uuid", "password", "dest", "server_name", "sni", "certificate_path", "key_path", "masquerade", "padding_scheme"} {
				if v, ok := n.Data[k]; ok {
					payload[k] = v
				}
			}
			payloadBytes, _ := json.Marshal(payload)
			dep.DB.Where("tag = ?", pTag).Delete(&models.Inbound{})
			dep.DB.Create(&models.Inbound{
				Tag:     pTag,
				Type:    sbType,
				Port:    port,
				Enabled: true,
				Payload: string(payloadBytes),
			})
		}
		if err := rebuildFromDB(dep.DB, dep.Manager, dep.ConfigPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "写入成功但重建配置失败: " + err.Error()})
			return
		}
	} else {
		if _, err := singbox.CompileAndWrite(topo, dep.ConfigPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := dep.Manager.Restart(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	links, _ := BuildShareLinks(dep.ConfigPath, publicHost(c))
	out := gin.H{"ok": true, "type": typ, "port": port, "nodes": topo.Nodes, "edges": topo.Edges, "links": links, "subscription": subscriptionURL(c), "running": dep.Manager.Status()}
	for _, e := range extra {
		for k, v := range e {
			out[k] = v
		}
	}
	c.JSON(http.StatusOK, out)
}

// quickDBType maps a topology node kind to the inbound type stored in SQLite.
func quickDBType(kind string) string {
	switch kind {
	case "inbound-reality":
		return "vless"
	case "inbound-hy2":
		return "hysteria2"
	case "inbound-anytls":
		return "anytls"
	}
	return ""
}

func randomPassword(n int) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return randomHex(n)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

func singleInboundTopology(id, kind string, port int, remark string, data map[string]interface{}) singbox.UIData {
	outID := "direct-oneclick"
	label := remark
	if label == "" {
		label = strings.ToUpper(strings.TrimPrefix(kind, "inbound-")) + fmt.Sprintf(" :%d", port)
	}
	return singbox.UIData{
		Nodes: []singbox.UINode{
			{ID: id, Kind: kind, Label: label, Data: data},
			{ID: outID, Kind: "outbound-direct", Label: "Direct", Data: map[string]interface{}{"kind": "outbound-direct", "tag": "direct"}},
		},
		Edges: []singbox.UIEdge{{ID: "e-" + id + "-direct", Source: id, Target: outID}},
	}
}

func writeClientMeta(configPath string, topo singbox.UIData) error {
	// Merge with whatever is already on disk: this file is the only place the
	// public half of a REALITY key is kept, so overwriting it would silently
	// degrade every previously created node's share link to pbk=.
	meta := map[string]map[string]string{}
	if data, err := os.ReadFile(configPath + ".client.json"); err == nil {
		var root struct {
			Inbounds map[string]map[string]string `json:"inbounds"`
		}
		if err := json.Unmarshal(data, &root); err == nil && root.Inbounds != nil {
			meta = root.Inbounds
		}
	}
	for _, n := range topo.Nodes {
		tag := ""
		if n.Data != nil {
			if v, ok := n.Data["tag"].(string); ok {
				tag = v
			}
		}
		if tag == "" {
			tag = n.ID
		}
		entry := meta[tag]
		if entry == nil {
			entry = map[string]string{}
		}
		for _, k := range []string{"public_key", "short_id"} {
			if n.Data != nil {
				if v, ok := n.Data[k].(string); ok && v != "" {
					entry[k] = v
				}
			}
		}
		if entry["public_key"] == "" {
			if pk, ok := n.Data["private_key"].(string); ok && pk != "" {
				if derived := singbox.DeriveRealityPublicKey(pk); derived != "" {
					entry["public_key"] = derived
				}
			}
		}
		if len(entry) > 0 {
			meta[tag] = entry
		}
	}
	if len(meta) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(map[string]interface{}{"inbounds": meta}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath+".client.json", data, 0644)
}

func generateRealityMaterial(bin string) map[string]string {
	result := map[string]string{"short_id": randomHex(8), "private_key": "", "public_key": ""}
	// Prefer computing the keypair in-process: a missing sing-box binary must
	// not degrade into an invalid hex "key" that no client can use.
	if priv, pub, err := singbox.GenerateRealityKeypair(); err == nil {
		result["private_key"] = priv
		result["public_key"] = pub
		return result
	}
	if bin == "" {
		return result
	}
	if out, err := exec.Command(bin, "generate", "reality-keypair").CombinedOutput(); err == nil {
		text := string(out)
		rePriv := regexp.MustCompile(`(?m)^PrivateKey:\s*(\S+)`)
		rePub := regexp.MustCompile(`(?m)^PublicKey:\s*(\S+)`)
		if m := rePriv.FindStringSubmatch(text); len(m) == 2 {
			result["private_key"] = m[1]
		}
		if m := rePub.FindStringSubmatch(text); len(m) == 2 {
			result["public_key"] = m[1]
		}
	}
	if result["public_key"] == "" && result["private_key"] != "" {
		result["public_key"] = singbox.DeriveRealityPublicKey(result["private_key"])
	}
	return result
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
func randomUUIDLike() string {
	h := randomHex(16)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// QuickAnyTLSDomainHandler issues a certificate for a domain the operator
// already pointed at this host, then creates the AnyTLS inbound with it. No DNS
// provider credentials are involved: the panel proves control over HTTP-01.
func QuickAnyTLSDomainHandler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Domain string `json:"domain"`
			Remark string `json:"remark"`
			Port   int    `json:"port"`
			Force  bool   `json:"force"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad json"})
			return
		}
		if dep.ACME == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ACME 未初始化"})
			return
		}
		domain := strings.ToLower(strings.TrimSpace(req.Domain))
		if domain == "" || !strings.Contains(domain, ".") {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "请填写完整域名，例如 node1.example.com"})
			return
		}
		v := acme.ValidateDomain(domain, acme.DetectPublicIP())
		if !v.OK && !req.Force {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": v.Reason, "validation": v})
			return
		}
		cert, err := dep.ACME.Obtain(domain)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "证书申请失败: " + err.Error(), "validation": v})
			return
		}
		port := singbox.PickAvailablePort(req.Port, map[int]bool{})
		if port == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "no available port"})
			return
		}
		remark := strings.TrimSpace(req.Remark)
		if remark == "" {
			remark = domain
		}
		topo := singleInboundTopology("anytls-domain", "inbound-anytls", port, remark, map[string]interface{}{
			"tag":              remark,
			"port":             port,
			"password":         randomPassword(24),
			"server_name":      domain,
			"sni":              domain,
			"certificate_path": cert.CertPath,
			"key_path":         cert.KeyPath,
		})
		password, _ := topo.Nodes[0].Data["password"].(string)
		own := fmt.Sprintf("anytls://%s@%s:%d?sni=%s#%s",
			url.QueryEscape(password), domain, port, url.QueryEscape(domain), url.QueryEscape(remark))
		st := dep.ACME.Status()

		// Make https://<domain> actually serve this panel: writing the vhost here
		// (nginx or Caddy) is the difference between "certificate issued" and
		// "you can open the panel over HTTPS". Failures are reported, not fatal.
		httpsURL := "https://" + domain
		autoRedirect := false
		siteProvider := ""
		publishErr := ""
		if dep.PanelAddr != "" {
			if res, err := httpsite.Publish(domain, cert.CertPath, cert.KeyPath, upstreamAddr(dep.PanelAddr), dep.ACME.Webroot); err != nil {
				publishErr = err.Error()
			} else {
				autoRedirect = res.Reloaded
				siteProvider = res.Provider
				httpsURL = res.URL
			}
		}
		if !autoRedirect && st.AutoRedirect {
			autoRedirect = true
			siteProvider = "builtin"
		}
		respondQuick(c, dep, topo, "anytls", port, gin.H{
			"domain":        domain,
			"cert_path":     cert.CertPath,
			"cert_expires":  cert.NotAfter.Format(time.RFC3339),
			"acme":          true,
			"link":          own,
			"mode":          st.Mode,
			"auto_redirect": autoRedirect,
			"https_url":     httpsURL,
			"site_provider": siteProvider,
			"publish_error": publishErr,
		})
	}
}
