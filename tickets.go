package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
)

// TicketCategory holds the name of a price type and the mapping of
// categories to prices for that price structure
type TicketCategory struct {
	ID         uint            `json:"id" gorm:"primary_key"`
	Name       string          `json:"name"`
	Categories postgres.Hstore `json:"categories"`
}

// GetTicketCats returns a function that fetchs all the Categories from the db
func GetTicketCats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cats []TicketCategory
		db.Find(&cats)
		c.JSON(http.StatusOK, cats)
	}
}

func DeleteTicketsCat(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		db.Where("id = ?", c.Param("id")).Delete(TicketCategory{})
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

		for _, c := range cat {
			db.Save(&c)
		}
		c.Status(http.StatusOK)
	}
}

func TripOnDate(d time.Time) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("TO_TIMESTAMP(LEFT(RIGHT(sku, 13), -3)::INTEGER) = ?", d)
	}
}

func GetCheckouts(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var orders []*CheckoutOrder
		var units []PurchaseUnit
		db.Where("payee_merchant_id = ?", c.Param("merchantid")).Find(&units)

		for idx := range units {
			db.Where("checkout_id = ?", units[idx].CheckoutID).Find(&units[idx].Payments.Captures)
			db.Where("checkout_id = ?", units[idx].CheckoutID).Find(&units[idx].Items)

			o := &CheckoutOrder{}
			db.Preload("Payer").Find(o, "id = ?", units[idx].CheckoutID)
			o.PurchaseUnits = []PurchaseUnit{units[idx]}
			orders = append(orders, o)
		}

		c.JSON(http.StatusOK, orders)
	}
}

func TripsOnDay(d string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("date_trunc('day', TO_TIMESTAMP(LEFT(RIGHT(sku, 13), -3)::INTEGER)) = ?", d)
	}
}

func GetPurchases(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ret []PurchaseItem
		db.Table("purchase_items as pi").Scopes(TripsOnDay(c.Param("date"))).
			Select("pi.*").
			Joins("LEFT JOIN purchase_units as pu ON pi.checkout_id = pu.checkout_id").
			Where("pu.payee_merchant_id = ?", c.Param("merchantid")).
			Scan(&ret)

		checkouts := make(map[string]*CheckoutOrder)
		for _, i := range ret {
			checkouts[i.CheckoutID] = nil
		}

		ids := make([]string, len(checkouts))
		for k := range checkouts {
			ids = append(ids, k)
		}

		var co []CheckoutOrder
		db.Preload("Payer").Where("id in (?)", ids).Find(&co)

		for idx := range co {
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Payments.Captures)
			db.Where("checkout_id = ?", co[idx].ID).Find(&co[idx].PurchaseUnits[0].Items)
		}

		c.JSON(http.StatusOK, gin.H{"items": ret, "orders": co})
	}
}
