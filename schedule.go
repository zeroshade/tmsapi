package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

var timeloc *time.Location

func init() {
	timeloc, _ = time.LoadLocation("America/New_York")
}

func addScheduleRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/schedule/:from/:to", GetSoldTickets(db))
	router.PUT("/override", checkJWT(), saveOverride(db))
	router.GET("/override/:date", checkJWT(), getOverrides(db))
	router.GET("/overrides/:from/:to", getOverrideRange(db))
}

type ManualOverride struct {
	ProductID uint      `json:"pid" gorm:"primary_key"`
	Time      time.Time `json:"time" gorm:"primary_key"`
	Cancelled bool      `json:"cancelled"`
	Avail     uint      `json:"avail"`
}

func getOverrideRange(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ret []ManualOverride
		merchantProds := db.Model(Product{}).Where("merchant_id = ? AND id = product_id", c.Param("merchantid")).Select("1").SubQuery()

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

// ScheduleTime represents a specific trip time for the schedule
type ScheduleTime struct {
	ID         uint   `json:"id"`
	ScheduleID uint   `json:"-"`
	StartTime  string `json:"startTime"`
	EndTime    string `json:"endTime"`
	Price      string `json:"price"`
}

// Schedule represents a full schedule that a Product can have multiple of
type Schedule struct {
	ProductID    uint           `json:"-"`
	ID           uint           `json:"id" gorm:"primary_key"`
	TicketsAvail uint           `json:"ticketsAvail"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	TimeArray    []ScheduleTime `json:"timeArray"`
	Days         pq.Int64Array  `json:"selectedDays" gorm:"type:integer[]"`
	NotAvail     pq.StringArray `json:"notAvailArray,nilasempty" gorm:"type:text[]"`
}

func (s *Schedule) AfterUpdate(tx *gorm.DB) (err error) {
	ids := make([]uint, 0, len(s.TimeArray))
	for _, t := range s.TimeArray {
		ids = append(ids, t.ID)
	}

	// clear out old schedules
	tx.Where("schedule_id = ?", s.ID).Not("id", ids).Delete(ScheduleTime{})
	return
}

// MarshalJSON handles the proper date formatting for schedules
func (s *Schedule) MarshalJSON() ([]byte, error) {
	type Alias Schedule
	if s.NotAvail == nil {
		s.NotAvail = make(pq.StringArray, 0)
	}
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

func GetSoldTickets(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		type result struct {
			Stamp time.Time `json:"stamp"`
			Qty   uint      `json:"qty"`
			Pid   uint      `json:"pid"`
		}

		sub := db.Model(&PurchaseItem{}).
			Select([]string{"checkout_id",
				`(regexp_matches(sku, '^\d+'))[1]::integer as pid`,
				"TO_TIMESTAMP(SUBSTRING(sku FROM '\\d[A-Z]+(\\d{10})\\d*')::INTEGER) as tm",
				"SUM(quantity) as q"}).Group("checkout_id, pid, tm").SubQuery()

		var out []result
		db.Table("purchase_units as pu").
			Select("pid, tm as stamp, sum(q) as qty").
			Joins("RIGHT JOIN ? as sub ON pu.checkout_id = sub.checkout_id", sub).
			Where("pu.payee_merchant_id = ? AND tm BETWEEN TO_TIMESTAMP(?) AND TO_TIMESTAMP(?)",
				c.Param("merchantid"), c.Param("from"), c.Param("to")).
			Group("pid, tm").Scan(&out)

		for idx, o := range out {
			out[idx].Stamp = o.Stamp.In(timeloc)
		}
		c.JSON(http.StatusOK, out)
	}
}
