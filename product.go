package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/types"
)

func addProductRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/", GetProducts(db))
	router.PUT("/product", checkJWT(), SaveProduct(db))
	router.DELETE("/product/:prodid", checkJWT(), DeleteProduct(db))
	router.GET("/boats", getBoats(db))
	router.PUT("/boats", checkJWT(), modifyBoat(db))
	router.POST("/boats", checkJWT(), createBoat(db))
	router.DELETE("/boats", checkJWT(), deleteBoat(db))
}

type Boat struct {
	ID    int    `json:"id" gorm:"primary key"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func getBoats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boats []Boat
		db.Find(&boats)
		c.JSON(http.StatusOK, boats)
	}
}

func modifyBoat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boat Boat
		if err := c.ShouldBindJSON(&boat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		db.Save(&boat)
		c.Status(http.StatusOK)
	}
}

func createBoat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boat Boat
		if err := c.ShouldBindJSON(&boat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		db.Create(&boat)
		c.Status(http.StatusOK)
	}
}

func deleteBoat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boat Boat
		if err := c.ShouldBindJSON(&boat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		db.Delete(&boat)
		c.Status(http.StatusOK)
	}
}

// Product represents a specific Type of tickets sold
type Product struct {
	ID          uint             `json:"id" gorm:"primary_key"`
	MerchantID  string           `json:"-" gorm:"type:varchar;not null;primary_key;"`
	CreatedAt   time.Time        `json:"-"`
	UpdatedAt   time.Time        `json:"-"`
	DeletedAt   *time.Time       `json:"-"`
	Name        string           `json:"name"`
	Desc        string           `json:"desc"`
	Color       string           `json:"color"`
	Publish     bool             `json:"publish"`
	ShowTickets bool             `json:"showTickets"`
	Schedules   []types.Schedule `json:"schedList"`
	Fish        string           `json:"fish"`
	Boat        *Boat            `json:"-"`
	BoatID      uint             `json:"boatId" gorm:"default:1"`
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
		db.Where("product_id = ?", inprod.ID).Not("id", ids).Delete(types.Schedule{})

		inprod.MerchantID = c.Param("merchantid")
		db.Save(&inprod)
	}
}

func GetProducts(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var prods []Product
		db.Preload("Boat").Preload("Schedules").Preload("Schedules.TimeArray").Order("name asc").Find(&prods, "merchant_id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, prods)
	}
}

func DeleteProduct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		db.Where("id = ? AND merchant_id = ?", c.Param("prodid"), c.Param("merchantid")).Delete(&Product{})
		c.Status(http.StatusOK)
	}
}
