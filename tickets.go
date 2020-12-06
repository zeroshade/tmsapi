package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
	"github.com/zeroshade/tmsapi/paypal"
	"github.com/zeroshade/tmsapi/stripe"
	"github.com/zeroshade/tmsapi/types"
)

func addTicketRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.PUT("/tickets", checkJWT(), logActionMiddle(db), SaveTicketCats(db))
	router.GET("/tickets", GetTicketCats(db))
	router.GET("/tickets/:id", checkJWT(), GetTicketCatEvenDeleted(db))
	router.GET("/items/:date", checkJWT(), GetPurchases(db))
	router.POST("/items", checkJWT(), logActionMiddle(db), GetOrders(db))
	router.DELETE("/tickets/:id", checkJWT(), logActionMiddle(db), DeleteTicketsCat(db))
	router.GET("/orders/:timestamp", checkJWT(), OrdersTimestamp(db))
	router.GET("/orders", GetCheckouts(db))
	router.POST("/passes", GetPasses(db))
	router.POST("/refund", checkJWT(), logActionMiddle(db), RefundTickets(db))
	router.POST("/transfer", checkJWT(), logActionMiddle(db), TransferTickets(db))
}

// TicketCategory holds the name of a price type and the mapping of
// categories to prices for that price structure
type TicketCategory struct {
	CreatedAt  time.Time       `json:"-"`
	UpdatedAt  time.Time       `json:"-"`
	DeletedAt  *time.Time      `json:"-"`
	ID         uint            `json:"id" gorm:"primary_key"`
	MerchantID string          `json:"-" gorm:"index:ticket_merchant"`
	Name       string          `json:"name"`
	Categories postgres.Hstore `json:"categories"`
}

// GetTicketCats returns a function that fetchs all the Categories from the db
func GetTicketCats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cats []TicketCategory
		db.Find(&cats, "merchant_id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, cats)
	}
}

func GetTicketCatEvenDeleted(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cat TicketCategory
		db.Unscoped().Where("id = ?", c.Param("id")).Find(&cat)
		c.JSON(http.StatusOK, cat)
	}
}

func DeleteTicketsCat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		db.Where("id = ? AND merchant_id = ?", c.Param("id"), c.Param("merchantid")).Delete(TicketCategory{})
		c.Status(http.StatusOK)
	}
}

// SaveTicketCats returns a function that will update and save/create
// all ticket categories that came in from a JSON request
func SaveTicketCats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cat []TicketCategory
		if err := c.ShouldBindJSON(&cat); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		for _, ct := range cat {
			ct.MerchantID = c.Param("merchantid")
			db.Save(&ct)
		}
		c.Status(http.StatusOK)
	}
}

func GetCheckouts(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var orders []*types.CheckoutOrder
		var units []types.PurchaseUnit
		db.Where("payee_merchant_id = ?", c.Param("merchantid")).Find(&units)

		for idx := range units {
			db.Where("checkout_id = ?", units[idx].CheckoutID).Find(&units[idx].Payments.Captures)
			db.Where("checkout_id = ?", units[idx].CheckoutID).Find(&units[idx].Items)

			o := &types.CheckoutOrder{}
			db.Preload("Payer").Find(o, "id = ?", units[idx].CheckoutID)
			o.PurchaseUnits = []types.PurchaseUnit{units[idx]}
			orders = append(orders, o)
		}

		c.JSON(http.StatusOK, orders)
	}
}

func TripsOnDay(d string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("date_trunc('day', TO_TIMESTAMP(SUBSTRING(sku FROM '\\d+[A-Z]+(\\d{10})\\d*')::INTEGER)) = ?", d)
	}
}

func TripTime(d string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("SUBSTRING(sku FROM '\\d+[A-Z]+(\\d{10})\\d*') = ?", d)
	}
}

type PaymentHandler interface {
	OrdersTimestamp(config *types.MerchantConfig, db *gorm.DB, timestamp string) (interface{}, error)
	GetSoldTickets(config *types.MerchantConfig, db *gorm.DB, from, to string) (interface{}, error)
	GetPassItems(conf *types.MerchantConfig, db *gorm.DB, id string) ([]types.PassItem, string)
	RefundTickets(config *types.MerchantConfig, db *gorm.DB, data json.RawMessage) (interface{}, error)
	TransferTickets(conig *types.MerchantConfig, db *gorm.DB, data []types.TransferReq) (interface{}, error)
}

func OrdersTimestamp(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config types.MerchantConfig
		db.Find(&config, "id = ?", c.Param("merchantid"))

		var handler PaymentHandler
		switch config.PaymentType {
		case "paypal":
			handler = &paypal.Handler{}
		case "stripe":
			handler = &stripe.Handler{}
		}

		ret, err := handler.OrdersTimestamp(&config, db, c.Param("timestamp"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusOK, ret)
		}
	}
}

func GetPurchases(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ret []types.PurchaseItem
		db.Table("purchase_items as pi").Scopes(TripsOnDay(c.Param("date"))).
			Select("pi.*").
			Joins("LEFT JOIN purchase_units as pu ON pi.checkout_id = pu.checkout_id").
			Where("pu.payee_merchant_id = ?", c.Param("merchantid")).
			Scan(&ret)

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

		for idx := range co {
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Payments.Captures)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Items)
		}

		c.JSON(http.StatusOK, gin.H{"items": ret, "orders": co})
	}
}

func GetOrders(db *gorm.DB) gin.HandlerFunc {
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
		scope := db.Table("purchase_items as pi").
			Select("pi.*").
			Joins("LEFT JOIN purchase_units as pu ON pi.checkout_id = pu.checkout_id").
			Where("pu.payee_merchant_id = ?", c.Param("merchantid"))

		scope.Count(&count)

		for idx, sort := range req.SortBy {
			col := sort
			switch sort {
			case "cost":
				col = "value"
			case "title":
				col = "description"
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

		for idx := range co {
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Payments.Captures)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Items)
		}

		c.JSON(http.StatusOK, gin.H{"total": count, "items": ret, "orders": co})
	}
}

func GetPasses(db *gorm.DB) gin.HandlerFunc {
	type PassesReq struct {
		Email string `json:"email"`
	}
	return func(c *gin.Context) {
		var req PassesReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if req.Email != "" {
			sub := db.Table("payers").Where("email = ?", req.Email).Select("id").SubQuery()

			type Result struct {
				CheckoutID string    `json:"checkoutId"`
				CreateTime time.Time `json:"created"`
			}
			var out []Result
			db.Table("purchase_units AS pu").
				Select("pu.checkout_id, co.create_time").
				Joins("LEFT JOIN checkout_orders AS co ON co.id = pu.checkout_id").
				Where("payer_id = ANY((SELECT array(?))::text[]) AND payee_merchant_id = ?", sub, c.Param("merchantid")).
				Scan(&out)

			c.JSON(http.StatusOK, out)
			return
		}

		c.JSON(http.StatusBadRequest, gin.H{"error": "Must have at least one"})
	}
}

func RefundTickets(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var data json.RawMessage
		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var config types.MerchantConfig
		db.Find(&config, "id = ?", c.Param("merchantid"))

		var handler PaymentHandler
		switch config.PaymentType {
		case "paypal":
			handler = &paypal.Handler{}
		case "stripe":
			handler = &stripe.Handler{}
		}

		ret, err := handler.RefundTickets(&config, db, data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, ret)
	}
}

func TransferTickets(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var data []types.TransferReq
		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var config types.MerchantConfig
		db.Find(&config, "id = ?", c.Param("merchantid"))

		var handler PaymentHandler
		switch config.PaymentType {
		case "paypal":
			handler = &paypal.Handler{}
		case "stripe":
			handler = &stripe.Handler{}
		}

		ret, err := handler.TransferTickets(&config, db, data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, ret)
	}
}
