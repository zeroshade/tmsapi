package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
)

type TicketCategory struct {
	Name       string          `json:"name" gorm:"primary_key"`
	Categories postgres.Hstore `json:"categories"`
}

func GetTicketCats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cats []TicketCategory
		db.Find(&cats)
		c.JSON(http.StatusOK, cats)
	}
}

func SaveTicketCats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cat []TicketCategory
		if err := c.ShouldBindJSON(&cat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		for _, c := range cat {
			db.Save(&c)
		}
	}
}
