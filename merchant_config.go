package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

type MerchantConfig struct {
	ID           string `json:"-" gorm:"primary_key"`
	PassTitle    string `json:"passTitle"`
	NotifyNumber string `json:"notifyNumber"`
	EmailFrom    string `json:"emailFrom"`
	EmailName    string `json:"emailName"`
	EmailContent string `json:"emailContent"`
	SendSMS      bool   `json:"sendSMS" gorm:"default:false"`
}

func GetMerchantConfig(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var conf MerchantConfig
		db.Find(&conf, "id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, conf)
	}
}

func UpdateMerchantConfig(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var conf MerchantConfig
		if err := c.ShouldBindJSON(&conf); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		conf.ID = c.Param("merchantid")
		db.Save(&conf)
		c.Status(http.StatusOK)
	}
}
