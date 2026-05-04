package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
		var req struct {
			Remark string `json:"remark"`
		}
		_ = c.ShouldBindJSON(&req)
		remark := strings.TrimSpace(req.Remark)

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
		topo := singleInboundTopology("hy2-oneclick", "inbound-hy2", port, remark, map[string]interface{}{
			"tag":        remark,
			"port":       port,
			"password":   randomToken(28),
			"masquerade": "https://www.bing.com",
		})
		respondQuick(c, dep, topo, "hy2", port)
	}
}

func respondQuick(c *gin.Context, dep quickDeps, topo singbox.UIData, typ string, port int) {
	_ = writeClientMeta(dep.ConfigPath, topo)
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
	meta := map[string]map[string]string{}
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
		entry := map[string]string{}
		for _, k := range []string{"public_key", "short_id"} {
			if n.Data != nil {
				if v, ok := n.Data[k].(string); ok && v != "" {
					entry[k] = v
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

func RenameInboundHandler(dep quickDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Tag    string `json:"tag"`
			NewTag string `json:"new_tag"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Tag == "" || req.NewTag == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tag and new_tag required"})
			return
		}
		// Read current config
		data, err := os.ReadFile(dep.ConfigPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read config"})
			return
		}
		var cfg map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse config"})
			return
		}
		inbounds, ok := cfg["inbounds"].([]interface{})
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no inbounds in config"})
			return
		}
		found := false
		for _, in := range inbounds {
			if m, ok := in.(map[string]interface{}); ok && m["tag"] == req.Tag {
				m["tag"] = req.NewTag
				found = true
				break
			}
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "inbound not found"})
			return
		}
		newData, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal config"})
			return
		}
		if err := os.WriteFile(dep.ConfigPath, newData, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config"})
			return
		}
		if err := dep.Manager.Restart(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "config updated but restart failed: " + err.Error()})
			return
		}
		links, _ := BuildShareLinks(dep.ConfigPath, publicHost(c))
		c.JSON(http.StatusOK, gin.H{"ok": true, "new_tag": req.NewTag, "links": links})
	}
}
