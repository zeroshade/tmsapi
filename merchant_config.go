package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/types"
)

func addMerchantConfigRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/config", GetMerchantConfig(db))
	router.PUT("/config", checkJWT(), logActionMiddle(db), UpdateMerchantConfig(db))
}

func GetMerchantConfig(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var conf types.MerchantConfig
		db.Find(&conf, "id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, conf)
	}
}

func UpdateMerchantConfig(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var conf types.MerchantConfig
		if err := c.ShouldBindJSON(&conf); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		conf.ID = c.Param("merchantid")
		db.Model(&conf).Save(&conf)
		c.Status(http.StatusOK)
	}
}
