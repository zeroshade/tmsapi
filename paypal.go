package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

// WebhookID is the constant id from PayPal for this webhook
var WebhookID string

func init() {
	WebhookID = os.Getenv("WEBHOOK_ID")
}

func GetItems(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t := types.Transaction{PaymentID: c.Param("transaction")}
		db.Find(&t)

		var items []types.Item
		db.Find(&items, "transaction = ?", t.PaymentID)
		c.JSON(http.StatusOK, items)
	}
}

// HandlePaypalWebhook returns a handler function that verifies a paypal webhook
// post request and then processes the event message
func HandlePaypalWebhook(db *gorm.DB) gin.HandlerFunc {
	env := internal.SANDBOX
	if strings.ToLower(os.Getenv("PAYPAL_ENV")) == "live" {
		env = internal.LIVE
	}
	return func(c *gin.Context) {
		paypalClient := internal.NewClient(env)
		verified := paypalClient.VerifyWebHookSig(c.Request, WebhookID)

		if !verified {
			log.Println("Didn't Verify")
			c.Status(http.StatusBadRequest)
			return
		}

		defer c.Request.Body.Close()
		body, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
			log.Println(err)
			c.Status(http.StatusBadRequest)
			return
		}

		var we types.WebHookEvent
		json.Unmarshal(body, &we)

		db.Save(&we)

		switch val := we.Resource.(type) {
		case *types.Payment:
			count := 0
			db.Model(&types.Payment{}).Where("id = ?", val.ID).Count(&count)
			if count <= 0 {
				db.Create(we.Resource)
				c.Status(http.StatusOK)
			}
			return
		case *types.Capture:
			count := 0
			db.Model(&types.Capture{}).Where("id = ?", val.ID).Count(&count)
			if count <= 0 {
				db.Create(we.Resource)

				if val.CheckoutID == "" {
					for _, l := range val.Links {
						if l.Rel != "up" {
							continue
						}

						req, _ := http.NewRequest(l.Method, l.Href, nil)
						resp, err := paypalClient.SendWithAuth(req)
						if err != nil {
							log.Println(err)
							c.Status(http.StatusFailedDependency)
							return
						}

						dec := json.NewDecoder(resp.Body)
						var checkout types.CheckoutOrder
						if err = dec.Decode(&checkout); err != nil {
							log.Println(err)
							c.Status(http.StatusFailedDependency)
							return
						}

						db.Save(&checkout)
						val.CheckoutID = checkout.ID
						db.Save(&val)

						var conf MerchantConfig
						mid := checkout.PurchaseUnits[0].Payee.MerchantID
						db.Find(&conf, "id = ?", mid)
						if len(conf.ID) <= 0 {
							db.Table("sandbox_infos").Select("id").Where("? = ANY (sandbox_ids)", mid).Scan(&conf)
							db.Find(&conf)
						}

						apiKey := os.Getenv("SENDGRID_API_KEY")
						sendNotifyEmail(apiKey, &conf, &checkout)
					}
				}

				c.Status(http.StatusOK)
			}
			return
		case *types.Refund:
			count := 0
			db.Model(&types.Refund{}).Where("id = ?", val.ID).Count(&count)
			if count > 0 {
				log.Println("Repeated Refund, already proccessed")
				c.Status(http.StatusOK)
				return
			}
			db.Create(we.Resource)
			for _, l := range val.Links {
				if l.Rel == "up" {
					req, err := http.NewRequest(l.Method, l.Href, nil)
					if err != nil {
						log.Println(err)
						c.Status(http.StatusFailedDependency)
						return
					}

					resp, err := paypalClient.SendWithAuth(req)
					if err != nil {
						log.Println(err)
						c.Status(http.StatusFailedDependency)
						return
					}
					defer resp.Body.Close()

					data, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						log.Println(err)
						c.Status(http.StatusFailedDependency)
						return
					}

					var capture types.Capture
					if err := json.Unmarshal(data, &capture); err != nil {
						log.Println(err)
						c.Status(http.StatusFailedDependency)
						return
					}

					db.Model(&capture).Update("status", "REFUNDED")
					db.Find(&capture)
					db.Model(&types.CheckoutOrder{}).Where("id = ?", capture.CheckoutID).Update("status", "REFUNDED")

					var items []types.PurchaseItem
					db.Find(&items, "checkout_id = ?", capture.CheckoutID)

					re := regexp.MustCompile(`(\d+)[A-Z]+(\d{10})`)
					for _, i := range items {
						res := re.FindStringSubmatch(i.Sku)
						pid, _ := strconv.Atoi(res[1])
						timestamp, _ := strconv.ParseInt(res[2], 10, 64)

						tm := time.Unix(timestamp, 0).In(timeloc)

						db.Model(ManualOverride{}).Where("product_id = ? AND time = ?", pid, tm).
							UpdateColumn("avail", gorm.Expr("avail + ?", i.Quantity))
					}
				}
			}
		}

		db.Save(we.Resource)
		c.Status(http.StatusOK)
	}
}
