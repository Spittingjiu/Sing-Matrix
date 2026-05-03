package realtime

import (
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func TrafficWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		payload := map[string]interface{}{
			"ts":       time.Now().Unix(),
			"upload":   rand.Intn(1024 * 1024),
			"download": rand.Intn(4 * 1024 * 1024),
			"inbound":  "hy2-main",
			"outbound": "direct",
		}
		if err := conn.WriteJSON(payload); err != nil {
			return
		}
	}
}
