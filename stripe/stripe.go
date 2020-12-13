package stripe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/lithammer/shortuuid/v3"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"github.com/stripe/stripe-go/v71"
	"github.com/stripe/stripe-go/v71/checkout/session"
	"github.com/stripe/stripe-go/v71/customer"
	"github.com/stripe/stripe-go/v71/paymentintent"
	"github.com/stripe/stripe-go/v71/transfer"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

func AddStripeRoutes(router *gin.RouterGroup, acctHandler gin.HandlerFunc, db *gorm.DB) {
	router.GET("/stripe/:stripe_session", acctHandler, GetSession(db))
	router.POST("/stripe", acctHandler, CreateSession(db))
}

const feeItemName = "Fees"

type createCheckoutSessionResponse struct {
	SessionID string `json:"id"`
}

type Money struct {
	CurrencyCode string  `json:"currency_code"`
	Value        float32 `json:"value,string"`
}

type CreateSessionRequest struct {
	Type  string `json:"type,omitempty"`
	Items []Item `json:"items"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
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
		// params.SetStripeAccount(c.GetString("stripe_acct"))
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
		var cart CreateSessionRequest
		if err := c.ShouldBindJSON(&cart); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var cus *stripe.Customer
		var err error

		iter := customer.List(&stripe.CustomerListParams{Email: &cart.Email})
		if iter.Next() {
			cus = iter.Customer()
			if cus.Phone == "" {
				cus, err = customer.Update(cus.ID, &stripe.CustomerParams{
					Name:  &cus.Name,
					Email: &cus.Email,
					Phone: &cart.Phone,
				})
				if err != nil {
					log.Println("Create customer error:", err)
				}
			}
		} else {

			cus, err = customer.New(&stripe.CustomerParams{
				Name:  &cart.Name,
				Email: &cart.Email,
				Phone: &cart.Phone,
			})
			if err != nil {
				log.Println("Create Customer Error:", err)
			}
		}

		params := &stripe.CheckoutSessionParams{
			Customer: &cus.ID,
			// AllowPromotionCodes: stripe.Bool(true),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
			Mode:               stripe.String(string(stripe.CheckoutSessionModePayment)),
			SuccessURL:         stripe.String(c.Request.Header.Get("x-calendar-origin") + "?status=success&stripe_session_id={CHECKOUT_SESSION_ID}"),
			CancelURL:          stripe.String(c.Request.Header.Get("x-calendar-origin") + "?status=cancelled&stripe_session_id={CHECKOUT_SESSION_ID}"),
			LineItems:          []*stripe.CheckoutSessionLineItemParams{},
		}

		var giftCards []*types.GiftCard
		total := int64(0)
		for _, item := range cart.Items {
			unit := int64(item.UnitAmount.Value * 100)
			quant := int64(item.Quantity)
			total += (unit * quant)

			metadata := map[string]string{"sku": item.Sku}
			if strings.HasPrefix(item.Sku, "GIFT") {
				if giftCards == nil {
					giftCards = make([]*types.GiftCard, 0)
				}

				for i := int64(0); i < quant; i++ {
					giftCards = append(giftCards, &types.GiftCard{
						ID:      shortuuid.New(),
						Initial: fmt.Sprintf("%0.2f", float64(unit)/100.0),
						Balance: float64(unit) / 100.0,
						Status:  "pending",
					})
				}
			}

			params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(stripe.CurrencyUSD)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String(item.Name),
						Description: stripe.String(item.Desc),
						Metadata:    metadata,
					},
					UnitAmount: &unit,
				},
				Quantity: &quant,
			})
		}

		fee := int64(float64(total) * 0.06)
		if fee > 0 {
			params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(stripe.CurrencyUSD)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(feeItemName),
					},
					UnitAmount: stripe.Int64(fee),
				},
			})
		}

		desc := "Ticket Purchase"
		if cart.Type == "giftcards" {
			desc = "Gift Card Purchase"
		}

		params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
			// ApplicationFeeAmount: stripe.Int64(int64(float64(total) * 0.02)),
			Description: stripe.String(desc),
			OnBehalfOf:  stripe.String(c.GetString("stripe_acct")),
			Metadata:    map[string]string{"type": cart.Type},
		}

		// params.SetStripeAccount(c.GetString("stripe_acct"))

		sess, err := session.New(params)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		if giftCards != nil {
			for _, c := range giftCards {
				c.PaymentID = sess.PaymentIntent.ID
				db.Create(c)
			}
		}
		data := createCheckoutSessionResponse{SessionID: sess.ID}
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

type notifyItem struct {
	Name        string
	Description string
	Quantity    int
}

func sendNotifyEmail(apiKey string, conf *types.MerchantConfig, payment *stripe.PaymentIntent, itemList []notifyItem) error {
	details := payment.Charges.Data[0].BillingDetails

	log.Println("Send Notify Mail:", payment.ID, conf.EmailFrom)
	const tmpl = `
	Tickets Purchased By: {{ .Payer }} <a href='mailto:{{ .PayerEmail }}'>{{ .PayerEmail }}</a>
	<br /><br />
	<ul>
	{{ range .Items -}}
	<li>{{ .Quantity }} {{ .Name }} {{ .Description }}</li>
	</ul>
	{{- end }}`

	t := template.Must(template.New("notify").Parse(tmpl))

	from := mail.NewEmail("Do Not Reply", "donotreply@websbyjoe.org")
	to := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	subject := "Tickets Purchased"
	var tpl bytes.Buffer

	if err := t.Execute(&tpl, gin.H{
		"Payer":      details.Name,
		"PayerEmail": details.Email,
		"Items":      itemList}); err != nil {
		return err
	}

	content := mail.NewContent("text/html", tpl.String())
	log.Println("Send Email:", from, subject, to, content)
	m := mail.NewV3MailInit(from, subject, to, content)
	request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	request.Method = "POST"
	request.Body = mail.GetRequestBody(m)
	_, err := sendgrid.API(request)
	if err != nil {
		return err
	}
	return nil
}

func sendCustomerEmail(apiKey, host string, conf *types.MerchantConfig, payment *stripe.PaymentIntent) error {
	details := payment.Customer

	const tickettmpl = `
	<br /><br />
	Your receipt can be accessed <a href='{{ .Receipt }}'>here</a>.
	<br/>
	If clicking on that doesn't work, you can copy and paste the following URL into
	your browser to access your receipt: {{ .Receipt }}.
	<br /><br/>
	You can download your boarding passes here: <a href='https://{{.Host}}/info/{{.MerchantID}}/passes/{{.PaymentID}}'>Click Here</a>
	<br/>`

	const gifttmpl = `
	<br /><br />
	Your receipt can be accessed <a href='{{ .Receipt }}'>here</a>.
	<br />
	If clicking on that doesn't work, you can copy and pages the following URL into
	your browser to access your receipt: {{ .Receipt}}.
	<br /><br />
	You should receive another e-mail shortly with the Gift Codes for your purchased Gift Cards.
	<br />`

	tmpl := tickettmpl
	subject := "Tickets Purchased"

	typ, ok := payment.Metadata["type"]
	if ok && typ == "giftcards" {
		tmpl = gifttmpl
		subject = "Gift Cards Purchased"
	}

	from := mail.NewEmail(conf.EmailName, conf.EmailFrom)

	to := mail.NewEmail(details.Name, details.Email)

	t := template.Must(template.New("notify").Parse(tmpl))
	var tpl bytes.Buffer
	if err := t.Execute(&tpl, gin.H{
		"Receipt": payment.Charges.Data[0].ReceiptURL,
		"Host":    host, "MerchantID": conf.ID, "PaymentID": payment.ID}); err != nil {
		return err
	}

	content := mail.NewContent("text/html", conf.EmailContent+tpl.String())
	log.Println("Send Email:", from, subject, to, content)
	m := mail.NewV3MailInit(from, subject, to, content)
	request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	request.Method = "POST"
	request.Body = mail.GetRequestBody(m)
	_, err := sendgrid.API(request)
	if err != nil {
		return err
	}
	return nil
}

func sendGiftCardEmail(apiKey string, giftCards []types.GiftCard, conf *types.MerchantConfig, payment *stripe.PaymentIntent) error {
	const tmpl = `
	Thank you for your purchase of Gift Cards! Below you'll find the codes which can be entered
	at checkout which can be given to your desired recipients.
	<br />
	<strong>Gift Card Codes are Case Sensitive at checkout!</strong>
	<br /><br />
	<table>
		<thead>
			<tr>
				<th>Value</th>
				<th>Code</th>
			</tr>
		</thead>
		<tbody>
	{{ range .GiftCards }}
			<tr>
				<td>{{ .Initial }}</td>
				<td>{{ .ID }}</td>
			</tr>
	{{ end }}
		</tbody>
	</table>
	`

	from := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	to := mail.NewEmail(payment.Customer.Name, payment.Customer.Email)
	subject := "Gift Card Codes"
	t := template.Must(template.New("codes").Parse(tmpl))
	var tpl bytes.Buffer
	if err := t.Execute(&tpl, gin.H{"GiftCards": giftCards}); err != nil {
		return err
	}

	content := mail.NewContent("text/html", tpl.String())
	log.Println("Send Email:", from, subject, to, content)
	m := mail.NewV3MailInit(from, subject, to, content)
	request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	request.Method = "POST"
	request.Body = mail.GetRequestBody(m)
	_, err := sendgrid.API(request)
	if err != nil {
		return err
	}

	return nil
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
	Status    string `json:"status"`
}

func StripeWebhook(db *gorm.DB) gin.HandlerFunc {
	apiKey := os.Getenv("SENDGRID_API_KEY")

	return func(c *gin.Context) {
		event := stripe.Event{}
		if err := c.BindJSON(&event); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fmt.Println(event.Type)

		switch event.Type {
		case "payment_intent.succeeded":
			var paymentIntent stripe.PaymentIntent
			if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			var conf types.MerchantConfig
			db.Find(&conf, "stripe_key = ?", paymentIntent.OnBehalfOf.ID)

			// details := paymentIntent.Charges.Data[0].BillingDetails
			if paymentIntent.Customer.Name == "" {
				cus, err := customer.Get(paymentIntent.Customer.ID, &stripe.CustomerParams{})
				if err != nil {
					log.Println("Customer Fetch Error:", err)
				}
				paymentIntent.Customer = cus
			}

			db.Save(&PaymentIntent{
				ID:        paymentIntent.ID,
				Acct:      conf.StripeKey,
				CreatedAt: time.Unix(paymentIntent.Created, 0),
				Amount:    fmt.Sprintf("%0.2f", float64(paymentIntent.Amount)/100.0),
				Email:     paymentIntent.Customer.Email,
				Name:      paymentIntent.Customer.Name,
				Status:    string(paymentIntent.Status),
			})

			err := sendCustomerEmail(apiKey, c.Request.Host, &conf, &paymentIntent)
			if err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"err": err.Error()})
				return
			}

			var giftCards []types.GiftCard
			db.Find(&giftCards, "payment_id = ?", paymentIntent.ID)
			if len(giftCards) > 0 {
				db.Model(&types.GiftCard{}).Where("payment_id = ?", paymentIntent.ID).Update("status", "success")

				sendGiftCardEmail(apiKey, giftCards, &conf, &paymentIntent)
			}

			c.Status(http.StatusOK)

		case "checkout.session.completed":
			var sess stripe.CheckoutSession
			if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			paymentParams := &stripe.PaymentIntentParams{}
			paymentParams.AddExpand("customer")
			paymentParams.AddExpand("charges")
			paymentParams.AddExpand("payment_method")
			// paymentParams.SetStripeAccount(event.Account)
			pm, err := paymentintent.Get(sess.PaymentIntent.ID, paymentParams)
			if err != nil {
				log.Println(err)
			}

			var conf types.MerchantConfig
			db.Find(&conf, "stripe_key = ?", pm.OnBehalfOf.ID)

			itemList := make([]notifyItem, 0)
			primary := int64(0)
			secondary := int64(0)

			params := &stripe.CheckoutSessionListLineItemsParams{}
			params.AddExpand("data.price")
			params.AddExpand("data.price.product")
			// params.SetStripeAccount(event.Account)
			i := session.ListLineItems(sess.ID, params)
			for i.Next() {
				li := i.LineItem()

				itemList = append(itemList, notifyItem{
					Name:     li.Price.Product.Name,
					Quantity: int(li.Quantity),
				})

				if li.Price.Product.Name != feeItemName {
					s := li.Quantity * 500
					primary += li.AmountTotal - s
					secondary += s
				}

				db.Save(&LineItem{
					ID:        li.ID,
					PaymentID: sess.PaymentIntent.ID,
					Acct:      conf.StripeKey,
					Quantity:  int(li.Quantity),
					Name:      li.Price.Product.Name,
					Sku:       li.Price.Product.Metadata["sku"],
					Amount:    fmt.Sprintf("%0.2f", float64(li.AmountTotal)/100.0),
					UnitPrice: fmt.Sprintf("%0.2f", float64(li.Price.UnitAmount)/100.0),
					Status:    string(pm.Status),
				})
			}

			fmt.Printf("Total %d, Primary: %d, Secondary: %d\n", pm.Amount, primary, secondary)

			transferParams := &stripe.TransferParams{}
			transferParams.SourceTransaction = &pm.Charges.Data[0].ID
			transferParams.Currency = stripe.String(string(stripe.CurrencyUSD))
			transferParams.Destination = &conf.StripeKey
			transferParams.Amount = stripe.Int64(primary)
			t, err := transfer.New(transferParams)
			if err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}
			fmt.Println("Primary Transfer:", t.ID, t.Amount)

			transferParams.Destination = &conf.StripeSecondary
			transferParams.Amount = stripe.Int64(secondary)
			t, err = transfer.New(transferParams)
			if err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}

			log.Println("Secondary Transfer:", t.ID, t.Amount)

			if err := sendNotifyEmail(apiKey, &conf, pm, itemList); err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}

			if conf.SendSMS {
				t := internal.NewTwilio(conf.TwilioAcctSID, conf.TwilioAcctToken, conf.TwilioFromNumber)
				t.Send(conf.NotifyNumber, "Tickets Purchased by "+pm.Charges.Data[0].BillingDetails.Name)
			}

		case "charge.refunded":
			var charge stripe.Charge
			if err := json.Unmarshal(event.Data.Raw, &charge); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			db.Model(&PaymentIntent{}).Where("id = ?", charge.PaymentIntent.ID).UpdateColumn("status", "refunded")
		}

		c.Status(http.StatusOK)
	}
}
