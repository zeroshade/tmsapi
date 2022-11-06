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
	router.POST("/shows/orders", checkJWT(), logActionMiddle(db), GetShowOrders(db))
	router.PUT("/shows/ticket/:tktid", checkJWT(), logActionMiddle(db), SetTicketUsed(db))
	router.DELETE("/shows/ticket/:tktid", checkJWT(), logActionMiddle(db), SetTicketUsed(db))
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

func GetShowOrders(db *gorm.DB) gin.HandlerFunc {
	type OrdersReq struct {
		From     string   `json:"from"`
		To       string   `json:"to"`
		Page     uint     `json:"page"`
		PerPage  uint     `json:"perPage"`
		SortBy   []string `json:"sortBy"`
		SortDesc []bool   `json:"sortDesc"`
	}

	return func(c *gin.Context) {
		var req OrdersReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var count uint
		var ret []types.PurchaseItem

		subQuery := db.Table("sandbox_infos").
			Select("unnest(sandbox_ids)").
			Where("id = ?", c.Param("merchantid"))

		scope := db.Table("purchase_items as pi").
			Select("pi.*").
			Joins("LEFT JOIN purchase_units as pu USING(checkout_id)").
			Where(`sku ^@ 'SHOW' AND (pu.payee_merchant_id = ? or pu.payee_merchant_id in ?)`, c.Param("merchantid"), subQuery.SubQuery())

		scope.Count(&count)
		for idx, sort := range req.SortBy {
			col := sort
			switch sort {
			case "cost":
				col = "value"
			case "title":
				col = "name"
			}
			if req.SortDesc[idx] {
				scope = scope.Order(col + " desc")
			} else {
				scope = scope.Order(col)
			}
		}

		if req.PerPage > 0 {
			scope = scope.Offset((req.Page - 1) * req.PerPage).Limit(req.PerPage)
		}

		scope.Scan(&ret)

		checkouts := make(map[string]*types.CheckoutOrder)
		for _, i := range ret {
			checkouts[i.CheckoutID] = nil
		}

		ids := make([]string, len(checkouts))
		for k := range checkouts {
			ids = append(ids, k)
		}

		var co []types.CheckoutOrder
		db.Preload("Payer").Where("id in (?)", ids).Find(&co)

		usage := make(map[string]bool)
		for idx := range co {
			var used []types.TicketUsage
			db.Where("ticket_id ^@ ?", co[idx].ID).Find(&used)
			for _, tu := range used {
				usage[tu.TicketID] = tu.Used
			}
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Payments.Captures)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Items)
		}

		c.JSON(http.StatusOK, gin.H{"total": count, "used": usage, "items": ret, "orders": co})
	}
}

func SetTicketUsed(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tu := types.TicketUsage{TicketID: c.Param("tktid")}
		switch c.Request.Method {
		case http.MethodDelete:
			tu.Used = false
		case http.MethodPut:
			tu.Used = true
		}

		db.Save(&tu)
	}
}
