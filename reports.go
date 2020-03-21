package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

func addReportRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/reports", GetReports(db))
	router.PUT("/reports", checkJWT(), SaveReport(db))
	router.DELETE("/reports/:id", checkJWT(), DeleteReport(db))
}

type Report struct {
	CreatedAt  *time.Time `json:"createdAt"`
	UpdatedAt  *time.Time `json:"updatedAt"`
	DeletedAt  *time.Time `json:"deletedAt"`
	ID         uint       `json:"id" gorm:"primary_key"`
	MerchantID string     `json:"-" gorm:"index:merchant"`
	Content    string     `json:"content"`
}

func GetReports(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var rep []Report
		db.Order("created_at desc").Find(&rep, "merchant_id = ?", c.Param("merchantid"))

		c.JSON(http.StatusOK, rep)
	}
}

func SaveReport(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var rep Report
		if err := c.ShouldBindJSON(&rep); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		rep.MerchantID = c.Param("merchantid")
		db.Save(&rep)
		c.Status(http.StatusOK)
	}
}

func DeleteReport(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		db.Delete(&Report{}, "merchant_id = ? AND id = ?", c.Param("merchantid"), c.Param("id"))

		c.Status(http.StatusNoContent)
	}
}
