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
	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

type quickDeps struct {
	Manager    *singbox.SingboxManager
	ConfigPath string
	DB         *gorm.DB
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
			"password":   randomHex(28),
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
	// Persist to DB
	if dep.DB != nil {
		for _, n := range topo.Nodes {
			if n.Kind == "inbound-reality" || n.Kind == "inbound-hy2" {
				sbType := "vless"
				if n.Kind == "inbound-hy2" {
					sbType = "hysteria2"
				}
				pTag, _ := n.Data["tag"].(string)
				pPort := port // use the port parameter directly
				payload := map[string]interface{}{}
				for _, k := range []string{"private_key", "public_key", "short_id", "uuid", "password", "dest"} {
					if v, ok := n.Data[k]; ok {
						payload[k] = v
					}
				}
				payloadBytes, _ := json.Marshal(payload)
				dep.DB.Where("tag = ?", pTag).Delete(&models.Inbound{})
				dep.DB.Create(&models.Inbound{
					Tag:     pTag,
					Type:    sbType,
					Port:    int(pPort),
					Enabled: true,
					Payload: string(payloadBytes),
				})
			}
		}
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
	result := map[string]string{"short_id": randomHex(8), "private_key": randomHex(43), "public_key": ""}
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
func randomUUIDLike() string {
	h := randomHex(16)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}


