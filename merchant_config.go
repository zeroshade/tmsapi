package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

func addMerchantConfigRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/config", GetMerchantConfig(db))
	router.PUT("/config", checkJWT(), logActionMiddle(db), UpdateMerchantConfig(db))
}

type SandboxInfo struct {
	ID         string         `gorm:"primary_key"`
	SandboxIDs pq.StringArray `gorm:"type:text[]"`
}

type MerchantConfig struct {
	ID              string `json:"-" gorm:"primary_key"`
	PassTitle       string `json:"passTitle"`
	NotifyNumber    string `json:"notifyNumber"`
	EmailFrom       string `json:"emailFrom"`
	EmailName       string `json:"emailName"`
	EmailContent    string `json:"emailContent"`
	SendSMS         bool   `json:"sendSMS" gorm:"default:false"`
	TermsConds      string `json:"terms"`
	SandboxID       string `json:"-"`
	TwilioAcctSID   string `json:"-"`
	TwilioAcctToken string `json:"-"`
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
		db.Model(&conf).Updates(&conf)
		c.Status(http.StatusOK)
	}
}
