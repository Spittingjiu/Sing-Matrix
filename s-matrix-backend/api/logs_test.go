package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestLogsWSHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logPath := "test-singbox.log"
	defer os.Remove(logPath)
	if err := os.WriteFile(logPath, []byte("INFO boot\nWARN warm\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/ws", LogsWSHandler(logPath))
	s := httptest.NewServer(r)
	defer s.Close()
	url := "ws" + s.URL[len("http"):] + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if len(msg) == 0 {
		t.Fatal("empty ws message")
	}
}
