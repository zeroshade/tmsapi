package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

func addProductRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/", GetProducts(db))
	router.PUT("/product", checkJWT(), SaveProduct(db))
	router.DELETE("/product/:prodid", checkJWT(), DeleteProduct(db))
}

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
	Fish        string     `json:"fish"`
}

// SaveProduct exports a handler for reading in a product and saving it to the db
func SaveProduct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var inprod Product
		if err := c.ShouldBindJSON(&inprod); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ids := make([]uint, 0, len(inprod.Schedules))
		for _, s := range inprod.Schedules {
			ids = append(ids, s.ID)
		}
		db.Where("product_id = ?", inprod.ID).Not("id", ids).Delete(Schedule{})

		inprod.MerchantID = c.Param("merchantid")
		db.Save(&inprod)
	}
}

func GetProducts(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var prods []Product
		db.Preload("Schedules").Preload("Schedules.TimeArray").Order("name asc").Find(&prods, "merchant_id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, prods)
	}
}

func DeleteProduct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		db.Where("id = ? AND merchant_id = ?", c.Param("prodid"), c.Param("merchantid")).Delete(&Product{})
		c.Status(http.StatusOK)
	}
}
