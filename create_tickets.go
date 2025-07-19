package main

import (
	_ "image/jpeg"
	_ "image/png"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/paypal"
	"github.com/zeroshade/tmsapi/stripe"
	"github.com/zeroshade/tmsapi/types"
)

func GetBoardingPasses(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config types.MerchantConfig
		db.Find(&config, "id = ? OR sandbox_id = ?", c.Param("merchantid"), c.Param("merchantid"))

		var handler PaymentHandler

		switch config.PaymentType {
		case "stripe":
			handler = &stripe.Handler{}
		case "paypal":
			handler = &paypal.Handler{}
		}

		items, name, email := handler.GetPassItems(&config, db, c.Param("checkoutid"))
		c.Header("Content-Type", "application/pdf")
		// c.Header("Content-Disposition", `attachment; filename="boardingpasses_`+c.Param("checkoutid")+`.pdf"`)
		c.Status(http.StatusOK)
		internal.GeneratePdf(db, items, config.PassTitle, name, email, c.Writer)
	}
}
