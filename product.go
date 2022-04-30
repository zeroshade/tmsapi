package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/stripe"
	"github.com/zeroshade/tmsapi/types"
)

func addProductRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/", getStripeAcct(db), GetProducts(db))
	router.GET("/product/:prodid", checkJWT(), GetProdEvenDeleted(db))
	router.PUT("/product", checkJWT(), logActionMiddle(db), SaveProduct(db))
	router.DELETE("/product/:prodid", checkJWT(), logActionMiddle(db), DeleteProduct(db))
	router.GET("/boats", getBoats(db))
	router.PUT("/boats", checkJWT(), logActionMiddle(db), modifyBoat(db))
	router.POST("/boats", checkJWT(), logActionMiddle(db), createBoat(db))
	router.DELETE("/boats", checkJWT(), logActionMiddle(db), deleteBoat(db))
}

func getBoats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boats []types.Boat
		db.Find(&boats, "merchant_id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, boats)
	}
}

func modifyBoat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boat types.Boat
		if err := c.ShouldBindJSON(&boat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		boat.MerchantID = c.Param("merchantid")

		db.Save(&boat)
		c.Status(http.StatusOK)
	}
}

func createBoat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boat types.Boat
		if err := c.ShouldBindJSON(&boat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		boat.MerchantID = c.Param("merchantid")
		db.Create(&boat)
		c.Status(http.StatusOK)
	}
}

func deleteBoat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var boat types.Boat
		if err := c.ShouldBindJSON(&boat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		boat.MerchantID = c.Param("merchantid")
		db.Delete(&boat)
		c.Status(http.StatusOK)
	}
}

// SaveProduct exports a handler for reading in a product and saving it to the db
func SaveProduct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var inprod types.Product
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

func GetProdEvenDeleted(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var prod types.Product
		db.Unscoped().Where("id = ?", c.Param("prodid")).Find(&prod)
		c.JSON(http.StatusOK, prod)
	}
}

func GetProducts(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var prods []types.Product
		if c.GetBool("stripe_managed") {
			stripe.GetProducts(db, c)
			return
		}

		db.Preload("Schedules").Preload("Schedules.TimeArray").Order("name asc").Find(&prods, "merchant_id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, prods)
	}
}

func DeleteProduct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		db.Where("id = ? AND merchant_id = ?", c.Param("prodid"), c.Param("merchantid")).Delete(&types.Product{})
		c.Status(http.StatusOK)
	}
}
