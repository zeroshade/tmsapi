package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

// Product represents a specific Type of tickets sold
type Product struct {
	ID          uint       `json:"id" gorm:"primary_key"`
	MerchantID  string     `json:"-" gorm:"type:varchar;not null;primary_key;"`
	CreatedAt   time.Time  `json:"-"`
	UpdatedAt   time.Time  `json:"-"`
	DeletedAt   *time.Time `json:"-"`
	Name        string     `json:"name"`
	Desc        string     `json:"desc"`
	Color       string     `json:"color"`
	Publish     bool       `json:"publish"`
	ShowTickets bool       `json:"showTickets"`
	Schedules   []Schedule `json:"schedList"`
}

// SaveProduct exports a handler for reading in a product and saving it to the db
func SaveProduct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var inprod Product
		if err := c.ShouldBindJSON(&inprod); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		inprod.MerchantID = c.Param("merchantid")
		db.Save(&inprod)
	}
}
