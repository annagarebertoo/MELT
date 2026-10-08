package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"servicio": "reservas",
			"estado":   "ok",
		})
	})

	router.Run(":8082")
}
