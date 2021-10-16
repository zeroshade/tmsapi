package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/paypal"
	"github.com/zeroshade/tmsapi/stripe"
	"github.com/zeroshade/tmsapi/types"
)

var timeloc *time.Location

func init() {
	timeloc, _ = time.LoadLocation("America/New_York")
}

func addScheduleRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/schedule/:from/:to", GetSoldTickets(db))
	router.PUT("/override", checkJWT(), logActionMiddle(db), saveOverride(db))
	router.GET("/override/:date", checkJWT(), getOverrides(db))
	router.GET("/overrides/:from/:to", getOverrideRange(db))
}

type ManualOverride struct {
	ProductID uint      `json:"pid" gorm:"primary_key"`
	Time      time.Time `json:"time" gorm:"primary_key"`
	Cancelled bool      `json:"cancelled"`
	Avail     int       `json:"avail"`
}

func getOverrideRange(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ret []ManualOverride
		merchantProds := db.Model(types.Product{}).Where("merchant_id = ? AND id = product_id", c.Param("merchantid")).Select("1").SubQuery()

		db.Model(ManualOverride{}).
			Where("DATE(time) BETWEEN ? AND ? AND EXISTS ?", c.Param("from"), c.Param("to"), merchantProds).
			Find(&ret)

		c.JSON(http.StatusOK, ret)
	}
}

func getOverrides(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var overrides []ManualOverride
		db.Where("DATE(time) = ?", c.Param("date")).Find(&overrides)
		c.JSON(http.StatusOK, overrides)
	}
}

func saveOverride(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var over ManualOverride
		if err := c.ShouldBindJSON(&over); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		over.Time = over.Time.In(timeloc)

		db.Save(&over)
	}
}

func GetSoldTickets(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config types.MerchantConfig
		db.Find(&config, "id = ?", c.Param("merchantid"))

		fmt.Println(config)
		var handler PaymentHandler
		switch config.PaymentType {
		case "paypal":
			handler = &paypal.Handler{}
		case "stripe":
			handler = &stripe.Handler{}
		}

		ret, err := handler.GetSoldTickets(&config, db, c.Param("from"), c.Param("to"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusOK, ret)
		}
	}
}
