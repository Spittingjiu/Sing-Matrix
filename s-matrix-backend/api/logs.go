package api

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var logUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func LogsWSHandler(logPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := logUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if logPath == "" {
			logPath = "/etc/s-matrix/singbox.log"
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("INFO S-Matrix log telemetry connected"))
		for {
			f, err := os.Open(logPath)
			if err != nil {
				_ = conn.WriteMessage(websocket.TextMessage, []byte("WARN waiting for log file: "+err.Error()))
				time.Sleep(2 * time.Second)
				continue
			}
			if stat, err := f.Stat(); err == nil && stat.Size() > 8192 {
				_, _ = f.Seek(-8192, io.SeekEnd)
			}
			reader := bufio.NewReader(f)
			for {
				line, err := reader.ReadString('\n')
				if line != "" {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(line)); err != nil {
						_ = f.Close()
						return
					}
				}
				if err != nil {
					if err == io.EOF {
						time.Sleep(700 * time.Millisecond)
						continue
					}
					_ = f.Close()
					break
				}
			}
		}
	}
}
