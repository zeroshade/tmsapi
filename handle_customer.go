package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

func ConfirmAndSend(db *gorm.DB) gin.HandlerFunc {
	type ConfReq struct {
		CheckoutId string `json:"checkoutId"`
	}

	apiKey := os.Getenv("SENDGRID_API_KEY")

	return func(c *gin.Context) {
		var r ConfReq
		if err := c.ShouldBindJSON(&r); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(internal.SANDBOX)
		data, err := paypalClient.GetCheckoutOrder(r.CheckoutId)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var order CheckoutOrder
		if err := json.Unmarshal(data, &order); err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var conf MerchantConfig
		db.Find(&conf, "id = ?", order.PurchaseUnits[0].Payee.MerchantID)

		from := mail.NewEmail(conf.EmailName, conf.EmailFrom)
		subject := "Tickets Purchased"
		to := mail.NewEmail(order.Payer.Name.GivenName+" "+order.Payer.Name.Surname, order.Payer.Email)
		content := mail.NewContent("text/html", conf.EmailContent+
			fmt.Sprintf(`<br /><br />You can download your Boarding Passes Here: <a href='https://%s/info/%s/passes/%s'>Click Here</a>`,
				c.Request.Host, order.PurchaseUnits[0].Payee.MerchantID, r.CheckoutId))

		m := mail.NewV3MailInit(from, subject, to, content)
		request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
		request.Method = "POST"
		request.Body = mail.GetRequestBody(m)
		response, err := sendgrid.API(request)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		c.Status(response.StatusCode)
	}
}
