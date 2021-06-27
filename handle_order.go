package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

type CaptureResponse struct {
	ID            string               `json:"id"`
	Status        string               `json:"status"`
	Payer         types.Payer          `json:"payer"`
	PurchaseUnits []types.PurchaseUnit `json:"purchase_units"`
	Links         []types.Link         `json:"links"`
}

type FailedCapture struct {
	Name    string `json:"name"`
	Details []struct {
		Issue string `json:"issue"`
		Desc  string `json:"description"`
	} `json:"details"`
	Message string       `json:"message"`
	DebugID string       `json:"debug_id"`
	Links   []types.Link `json:"links"`
}

var apiKey = os.Getenv("MAILGUN_API_KEY")

func AddOrderToDB(cr *CaptureResponse, tx *gorm.DB) *types.CheckoutOrder {
	var order types.CheckoutOrder
	order.ID = cr.ID
	order.Payer = &cr.Payer
	order.PayerID = cr.Payer.ID
	order.Status = cr.Status
	order.PurchaseUnits = cr.PurchaseUnits

	tx.Create(&order)

	tx.Model(order.Payer).Update(*order.Payer)

	return &order
}

func HashMerchantID(id string) int32 {
	const multiplier = 31

	ret := int32(0)
	for _, c := range []byte(id) {
		ret = multiplier*ret + int32(c)
	}
	return ret
}

func CaptureOrder(db *gorm.DB) gin.HandlerFunc {
	env := internal.SANDBOX
	if strings.ToLower(os.Getenv("PAYPAL_ENV")) == "live" {
		env = internal.LIVE
	}

	type CaptureReq struct {
		OrderID string `json:"orderId"`
	}

	return func(c *gin.Context) {
		var cr CaptureReq
		if err := c.ShouldBindJSON(&cr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(env)
		resp, err := paypalClient.CaptureOrder(cr.OrderID)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		dec := json.NewDecoder(resp.Body)
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			var r CaptureResponse
			if err = dec.Decode(&r); err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}

			order := AddOrderToDB(&r, db)

			var conf types.MerchantConfig
			mid := order.PurchaseUnits[0].Payee.MerchantID
			db.Find(&conf, "id = ?", mid)

			if len(conf.ID) <= 0 {
				db.Table("sandbox_infos").Select("id").Where("? = ANY (sandbox_ids)", mid).Scan(&conf)
				db.Find(&conf)
			}

			if err := sendNotifyEmail(apiKey, &conf, order); err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}

			if conf.SendSMS {
				t := internal.NewTwilio(conf.TwilioAcctSID, conf.TwilioAcctToken, conf.TwilioFromNumber)
				t.Send(conf.NotifyNumber, "Tickets Purchased by "+order.Payer.Name.GivenName+" "+order.Payer.Name.Surname)
			}

			_, err := SendClientMail(apiKey, c.Request.Host, order.Payer.Email, order, &conf)
			if err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, r)
		} else {
			var f FailedCapture
			if err = dec.Decode(&f); err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}
			c.JSON(resp.StatusCode, f)
		}
	}
}
