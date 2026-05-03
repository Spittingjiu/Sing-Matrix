package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
)

type quickDeps struct {
	Manager    *singbox.SingboxManager
	ConfigPath string
}

func QuickRealityHandler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys := generateRealityMaterial("/usr/local/bin/sing-box")
		port := singbox.PickAvailablePort(0, map[int]bool{})
		if port == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no available port"})
			return
		}
		topo := singleInboundTopology("reality-oneclick", "inbound-reality", port, map[string]interface{}{
			"tag":         "reality-oneclick",
			"port":        port,
			"uuid":        randomUUIDLike(),
			"dest":        "www.microsoft.com",
			"server_name": "www.microsoft.com",
			"short_id":    keys["short_id"],
			"private_key": keys["private_key"],
			"public_key":  keys["public_key"],
		})
		respondQuick(c, dep, topo, "reality", port)
	}
}

func QuickHY2Handler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		port := singbox.PickAvailablePort(0, map[int]bool{})
		if port == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no available port"})
			return
		}
		topo := singleInboundTopology("hy2-oneclick", "inbound-hy2", port, map[string]interface{}{
			"tag":        "hy2-oneclick",
			"port":       port,
			"password":   randomToken(28),
			"masquerade": "https://www.bing.com",
		})
		respondQuick(c, dep, topo, "hy2", port)
	}
}

func respondQuick(c *gin.Context, dep quickDeps, topo singbox.UIData, typ string, port int) {
	cfg, err := singbox.CompileAndWrite(topo, dep.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := dep.Manager.Restart(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "config": cfg})
		return
	}
	links, _ := BuildShareLinks(dep.ConfigPath, publicHost(c))
	c.JSON(http.StatusOK, gin.H{"ok": true, "type": typ, "port": port, "nodes": topo.Nodes, "edges": topo.Edges, "links": links, "subscription": subscriptionURL(c), "running": dep.Manager.Status(), "config": cfg})
}

func singleInboundTopology(id, kind string, port int, data map[string]interface{}) singbox.UIData {
	outID := "direct-oneclick"
	return singbox.UIData{
		Nodes: []singbox.UINode{
			{ID: id, Kind: kind, Label: strings.ToUpper(strings.TrimPrefix(kind, "inbound-")) + fmt.Sprintf(" :%d", port), Data: data},
			{ID: outID, Kind: "outbound-direct", Label: "Direct", Data: map[string]interface{}{"kind": "outbound-direct", "tag": "direct"}},
		},
		Edges: []singbox.UIEdge{{ID: "e-" + id + "-direct", Source: id, Target: outID}},
	}
}

func generateRealityMaterial(bin string) map[string]string {
	result := map[string]string{"short_id": randomHex(8), "private_key": randomToken(43), "public_key": ""}
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
	return result
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
func randomToken(bytes int) string { return randomHex(bytes) }
func randomUUIDLike() string {
	h := randomHex(16)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
