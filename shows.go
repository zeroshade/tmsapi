package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/types"
)

func addShowRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/shows", GetShows(db))
	router.GET("/shows/:showid", GetShow(db))
	router.PUT("/shows", checkJWT(), logActionMiddle(db), SaveShow(db))
	router.DELETE("/shows/:showid", checkJWT(), logActionMiddle(db), DeleteShow(db))
}

func GetShows(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var shows []struct {
			types.Show
			StartDate time.Time `json:"start"`
			EndDate   time.Time `json:"end"`
		}
		db.Model(&types.Show{}).Where("merchant_id = ?", c.Param("merchantid")).
			Select("*, lower(dates) as start_date, upper(dates) - (not upper_inc(dates))::int AS end_date").
			Scan(&shows)
		c.JSON(http.StatusOK, shows)
	}
}

func GetShow(db *gorm.DB) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var show struct {
			types.Show
			StartDate time.Time `json:"start"`
			EndDate   time.Time `json:"end"`
		}
		db.Model(&types.Show{}).Where("merchant_id = ? AND id = ?",
			ctx.Param("merchantid"), ctx.Param("showid")).
			Select("*, lower(dates) as start_date, upper(dates) - (not upper_inc(dates))::int as end_date").
			Scan(&show)
		ctx.JSON(http.StatusOK, show)
	}
}

func SaveShow(db *gorm.DB) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var inshow types.Show
		if err := ctx.ShouldBindJSON(&inshow); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if inshow.ID < 0 {
			inshow.ID = 0
		}
		inshow.MerchantID = ctx.Param("merchantid")
		db.Save(&inshow)
	}
}

func DeleteShow(db *gorm.DB) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		db.Where("id = ? AND merchant_id = ?", ctx.Param("showid"), ctx.Param("merchantid")).Delete(&types.Show{})
		ctx.Status(http.StatusOK)
	}
}
