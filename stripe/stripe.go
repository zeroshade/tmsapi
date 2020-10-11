package stripe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/stripe/stripe-go/v71"
	"github.com/stripe/stripe-go/v71/checkout/session"
)

func AddStripeRoutes(router *gin.RouterGroup, acctHandler gin.HandlerFunc, db *gorm.DB) {
	router.GET("/stripe/:stripe_session", acctHandler, GetSession(db))
	router.POST("/stripe", acctHandler, CreateSession(db))
}

type createCheckoutSessionResponse struct {
	SessionID string `json:"id"`
}

type Money struct {
	CurrencyCode string  `json:"currency_code"`
	Value        float32 `json:"value,string"`
}

type Item struct {
	Name       string `json:"name"`
	UnitAmount Money  `json:"unit_amount"`
	Quantity   int    `json:"quantity,string"`
	Sku        string `json:"sku"`
	Desc       string `json:"description"`
}

func init() {
	stripe.Key = os.Getenv("STRIPE_KEY")
}

func GetSession(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		params := &stripe.CheckoutSessionParams{}
		params.AddExpand("payment_intent.charges")
		params.AddExpand("payment_intent.payment_method")
		params.AddExpand("line_items")
		params.SetStripeAccount(c.GetString("stripe_acct"))
		session, err := session.Get(c.Param("stripe_session"), params)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, session)
	}
}

func CreateSession(db *gorm.DB) gin.HandlerFunc {
	// env := internal.SANDBOX
	// if strings.ToLower(os.Getenv("STRIPE_ENV")) == "live" {
	// 	env = internal.LIVE
	// }

	return func(c *gin.Context) {
		var cart []Item
		if err := c.ShouldBindJSON(&cart); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		params := &stripe.CheckoutSessionParams{
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
			Mode:               stripe.String(string(stripe.CheckoutSessionModePayment)),
			SuccessURL:         stripe.String(c.Request.Header.Get("x-calendar-origin") + "?status=success&stripe_session_id={CHECKOUT_SESSION_ID}"),
			CancelURL:          stripe.String(c.Request.Header.Get("x-calendar-origin") + "?status=cancelled&stripe_session_id={CHECKOUT_SESSION_ID}"),
			LineItems:          []*stripe.CheckoutSessionLineItemParams{},
		}

		total := int64(0)
		for _, item := range cart {
			unit := int64(item.UnitAmount.Value * 100)
			quant := int64(item.Quantity)
			total += (unit * quant)
			params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(stripe.CurrencyUSD)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: &item.Name,
						Metadata: map[string]string{
							"sku": item.Sku,
						},
					},
					UnitAmount: &unit,
				},
				Quantity: &quant,
			})
		}

		fee := (total / 5000) * 300
		if fee > 0 {
			params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(stripe.CurrencyUSD)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String("Fees"),
					},
					UnitAmount: stripe.Int64(fee),
				},
			})
		}

		params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
			ApplicationFeeAmount: stripe.Int64(int64(float64(total) * 0.02)),
			Description:          stripe.String("Ticket Purchase"),
		}

		params.SetStripeAccount(c.GetString("stripe_acct"))

		session, err := session.New(params)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		data := createCheckoutSessionResponse{SessionID: session.ID}
		c.JSON(http.StatusOK, data)
	}
}

type PaymentIntent struct {
	ID        string    `json:"id" gorm:"primary_key"`
	Acct      string    `json:"-" gorm:"primary_key"`
	CreatedAt time.Time `json:"createdAt"`
	Amount    string    `json:"amount" gorm:"type:money"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
}

type LineItem struct {
	ID        string `json:"id" gorm:"primary_key"`
	PaymentID string `json:"paymentId" gorm:"primary_key"`
	Acct      string `json:"-"`
	Quantity  int    `json:"quantity"`
	Sku       string `json:"sku"`
	Name      string `json:"name"`
	UnitPrice string `json:"unitPrice" gorm:"type:money"`
	Amount    string `json:"total" gorm:"type:money"`
}

func StripeWebhook(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		event := stripe.Event{}
		if err := c.BindJSON(&event); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		switch event.Type {
		case "payment_intent.succeeded":
			var paymentIntent stripe.PaymentIntent
			if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			db.Save(&PaymentIntent{
				ID:        paymentIntent.ID,
				Acct:      event.Account,
				CreatedAt: time.Unix(paymentIntent.Created, 0),
				Amount:    fmt.Sprintf("%0.2f", float64(paymentIntent.Amount)/100.0),
				Email:     paymentIntent.Charges.Data[0].BillingDetails.Email,
				Name:      paymentIntent.Charges.Data[0].BillingDetails.Name,
				Status:    string(paymentIntent.Status),
			})

		case "checkout.session.completed":
			var sess stripe.CheckoutSession
			if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			params := &stripe.CheckoutSessionListLineItemsParams{}
			params.AddExpand("data.price")
			params.AddExpand("data.price.product")
			params.SetStripeAccount(event.Account)
			i := session.ListLineItems(sess.ID, params)
			for i.Next() {
				li := i.LineItem()

				db.Save(&LineItem{
					ID:        li.ID,
					PaymentID: sess.PaymentIntent.ID,
					Acct:      event.Account,
					Quantity:  int(li.Quantity),
					Name:      li.Price.Product.Name,
					Sku:       li.Price.Product.Metadata["sku"],
					Amount:    fmt.Sprintf("%0.2f", float64(li.AmountTotal)/100.0),
					UnitPrice: fmt.Sprintf("%0.2f", float64(li.Price.UnitAmount)/100.0),
				})
			}
		}

		c.Status(http.StatusOK)
	}
}
