package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
)

// QRHandler renders a QR code locally so the panel never has to call a public
// QR service (which would leak node links to a third party).
func QRHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		data := c.Query("data")
		if data == "" {
			c.String(http.StatusBadRequest, "data required")
			return
		}
		if len(data) > 4096 {
			c.String(http.StatusBadRequest, "data too long")
			return
		}
		size := 220
		if v, err := strconv.Atoi(c.Query("size")); err == nil && v >= 80 && v <= 800 {
			size = v
		}
		png, err := qrcode.Encode(data, qrcode.Medium, size)
		if err != nil {
			c.String(http.StatusInternalServerError, "qr encode failed: %v", err)
			return
		}
		c.Header("Cache-Control", "public, max-age=300")
		c.Data(http.StatusOK, "image/png", png)
	}
}
