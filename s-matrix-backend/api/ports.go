package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
)

func AvailablePortHandler(c *gin.Context) {
	preferred, _ := strconv.Atoi(c.Query("preferred"))
	port := singbox.PickAvailablePort(preferred, map[int]bool{})
	if port == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no available port"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"port": port, "preferred": preferred})
}
