package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/lithammer/shortuuid/v3"
	"github.com/mailgun/mailgun-go/v4"
	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/checkout/session"
	"github.com/stripe/stripe-go/v72/coupon"
	"github.com/stripe/stripe-go/v72/customer"
	"github.com/stripe/stripe-go/v72/paymentintent"
	"github.com/stripe/stripe-go/v72/transfer"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

func AddStripeRoutes(router *gin.RouterGroup, acctHandler gin.HandlerFunc, db *gorm.DB) {
	router.GET("/stripe/:stripe_session", acctHandler, GetSession(db))
	router.POST("/stripe", acctHandler, CreateSession(db))
	router.GET("/giftcard/:id", acctHandler, CheckGiftcard(db))
	router.POST("/deposit/stripe", acctHandler, CheckoutDeposit(db))
	router.GET("/deposits/:yearmonth", acctHandler, GetDeposits)
	router.GET("/deposits", acctHandler, GetDepositOrders)
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
	Type        string `json:"type,omitempty"`
	Items       []Item `json:"items"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	UseGiftCard string `json:"useGift"`
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

func CheckGiftcard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var gift types.GiftCard
		db.Find(&gift, "id = ? AND status = 'success'", c.Param("id"))

		if gift.ID == c.Param("id") {
			c.JSON(http.StatusOK, &gift)
		} else {
			c.Status(http.StatusNotFound)
		}
	}
}

func GetSession(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		params := &stripe.CheckoutSessionParams{}
		params.AddExpand("payment_intent.charges")
		params.AddExpand("payment_intent.payment_method")
		params.AddExpand("line_items")
		// params.AddExpand("payment_intent.customer")
		// params.SetStripeAccount(c.GetString("stripe_acct"))

		key := stripe.Key
		sk := c.GetString("stripe_acct")
		if !strings.HasPrefix(sk, "acct_") {
			key = sk
		} else {
			params.SetStripeAccount(sk)
		}
		sess := session.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
		session, err := sess.Get(c.Param("stripe_session"), params)
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

		key := stripe.Key
		sk := c.GetString("stripe_acct")
		isSubAcct := strings.HasPrefix(sk, "acct_")
		if !isSubAcct {
			key = sk
		}

		cusClient := customer.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
		cusParams := &stripe.CustomerListParams{Email: &cart.Email}
		if isSubAcct {
			cusParams.SetStripeAccount(sk)
		}

		iter := cusClient.List(cusParams)
		if iter.Next() {
			cus = iter.Customer()
			if cus.Phone == "" {
				p := &stripe.CustomerParams{
					Name:  &cus.Name,
					Email: &cus.Email,
					Phone: &cart.Phone,
				}
				if isSubAcct {
					p.SetStripeAccount(sk)
				}
				cus, err = cusClient.Update(cus.ID, p)
				if err != nil {
					log.Println("Create customer error:", err)
				}
			}
		} else {
			p := &stripe.CustomerParams{
				Name:  &cart.Name,
				Email: &cart.Email,
				Phone: &cart.Phone,
			}
			if isSubAcct {
				p.SetStripeAccount(sk)
			}
			cus, err = cusClient.New(p)
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

		metadata := map[string]string{"type": cart.Type}
		var discount *stripe.Coupon

		if cart.UseGiftCard != "" {
			var gift types.GiftCard
			db.Find(&gift, "id = ? AND status = 'success'", cart.UseGiftCard)

			if gift.Balance > 0 {
				metadata["giftcard"] = cart.UseGiftCard

				amount := int64(gift.Balance * 100)
				metadata["amount"] = strconv.Itoa(int(amount))

				couponParams := &stripe.CouponParams{
					Name:           stripe.String("Gift Certificate"),
					AmountOff:      &amount,
					Currency:       stripe.String(string(stripe.CurrencyUSD)),
					Duration:       stripe.String("once"),
					MaxRedemptions: stripe.Int64(1),
				}
				couponParams.Metadata = map[string]string{"giftid": cart.UseGiftCard}
				discount, err = coupon.New(couponParams)

				if err != nil {
					log.Println(err)
				}

				params.Discounts = []*stripe.CheckoutSessionDiscountParams{
					{Coupon: &discount.ID},
				}
			}
		}

		var giftCards []*types.GiftCard

		total := int64(0)
		for _, item := range cart.Items {
			unit := int64(item.UnitAmount.Value * 100)
			quant := int64(item.Quantity)
			total += (unit * quant)

			metadata := map[string]string{"sku": item.Sku}
			if strings.HasPrefix(item.Sku, "GIFT") {
				for i := 0; i < item.Quantity; i++ {
					giftCards = append(giftCards, &types.GiftCard{
						ID:      shortuuid.New(),
						Initial: fmt.Sprintf("%.02f", item.UnitAmount.Value),
						Balance: float64(item.UnitAmount.Value),
						Status:  "pending",
					})
				}
			}

			var desc *string
			if item.Desc != "" {
				desc = stripe.String(item.Desc)
			}
			params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(stripe.CurrencyUSD)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String(item.Name),
						Description: desc,
						Metadata:    metadata,
					},
					UnitAmount: &unit,
				},
				Quantity: &quant,
			})
		}

		if discount != nil {
			total -= discount.AmountOff
		}
		feePct := c.GetFloat64("fee_pct")
		fee := int64(float64(total) * feePct)

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
			Metadata:    metadata,
		}

		stripeFee := int64(math.Ceil(float64(total+fee)*0.029)) + 30
		if fee > stripeFee {
			params.PaymentIntentData.ApplicationFeeAmount = stripe.Int64(fee - stripeFee)
		}

		if isSubAcct {
			// params.PaymentIntentData.OnBehalfOf = stripe.String(c.GetString("stripe_acct"))
			params.SetStripeAccount(c.GetString("stripe_acct"))
		}

		// params.SetStripeAccount(c.GetString("stripe_acct"))
		sessClient := session.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
		sess, err := sessClient.New(params)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		for _, c := range giftCards {
			c.PaymentID = sess.PaymentIntent.ID
			db.Create(c)
		}

		db.Save(&PaymentIntent{
			ID:   sess.PaymentIntent.ID,
			Acct: c.GetString("stripe_acct"),
		})

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

var (
	mailgunPublicKey = os.Getenv("MAILGUN_PUBLIC_KEY")
	mailgunDomain    = os.Getenv("MAILGUN_DOMAIN")
)

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

	mg := mailgun.NewMailgun(mailgunDomain, apiKey)

	// from := mail.NewEmail("Fishing Reservation System", "donotreply@fishingreservationsystem.com")
	// to := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	subject := "Tickets Purchased"
	var tpl bytes.Buffer

	if err := t.Execute(&tpl, gin.H{
		"Payer":      details.Name,
		"PayerEmail": details.Email,
		"Items":      itemList}); err != nil {
		return err
	}

	to := fmt.Sprintf("%s <%s>", conf.EmailName, conf.EmailFrom)
	m := mg.NewMessage("donotreply@fishingreservationsystem.com", subject, tpl.String(), to)
	m.SetHtml(tpl.String())

	resp, id, err := mg.Send(context.Background(), m)
	log.Println("Send Email: ", subject, to)
	log.Println("Response: ", resp, id)

	// content := mail.NewContent("text/html", tpl.String())
	// log.Println("Send Email:", from, subject, to, content)
	// m := mail.NewV3MailInit(from, subject, to, content)
	// request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	// request.Method = "POST"
	// request.Body = mail.GetRequestBody(m)
	// _, err := sendgrid.API(request)
	// if err != nil {
	// 	return err
	// }
	return err
}

func sendCustomerEmail(db *gorm.DB, apiKey, host string, conf *types.MerchantConfig, payment *stripe.PaymentIntent) error {
	details := payment.Customer

	const tickettmpl = `
	<br /><br />
	Your boarding passes and receipt are included as an attachment to this email as a PDF file
	for easy printing. You can also access your receipt via Stripe by clicking <a href='{{.Receipt}}'>here</a>.
	<br /><br />
	If clicking on that doesn't work, you can copy and paste the following URL into
	your browser to access your receipt: {{ .Receipt }}.	
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

	mg := mailgun.NewMailgun(mailgunDomain, apiKey)

	// from := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	// from := mail.NewEmail(conf.EmailName, "donotreply@fishingreservationsystem.com")

	// to := mail.NewEmail(details.Name, details.Email)

	t := template.Must(template.New("notify").Parse(tmpl))
	var tpl bytes.Buffer
	if err := t.Execute(&tpl, gin.H{
		"Receipt": payment.Charges.Data[0].ReceiptURL,
		"Host":    host, "MerchantID": conf.ID, "PaymentID": payment.ID}); err != nil {
		return err
	}

	to := fmt.Sprintf("%s <%s>", details.Name, details.Email)
	m := mg.NewMessage("donotreply@fishingreservationsystem.com", subject, tpl.String(), to)
	m.SetHtml(tpl.String())

	var pdf bytes.Buffer

	items, _, _ := (Handler{}).GetPassItems(conf, db, payment.ID)
	generatePdf(db, conf, items, "Boarding Passes", details.Name, details.Email, payment.ID, &pdf)
	m.AddBufferAttachment("boardingpasses.pdf", pdf.Bytes())

	resp, id, err := mg.Send(context.Background(), m)
	log.Println("Send Email: ", subject, to)
	log.Println("Response: ", resp, id)

	// content := mail.NewContent("text/html", conf.EmailContent+tpl.String())
	// log.Println("Send Email:", from, subject, to, content)
	// m := mail.NewV3MailInit(from, subject, to, content)
	// request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	// request.Method = "POST"
	// request.Body = mail.GetRequestBody(m)
	// _, err := sendgrid.API(request)
	// if err != nil {
	// 	return err
	// }
	return err
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

	mg := mailgun.NewMailgun("mg.fishingreservationsystem.com", apiKey)

	// from := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	// from := mail.NewEmail(conf.EmailName, "donotreply@fishingreservationsystem.com")
	// to := mail.NewEmail(payment.Customer.Name, payment.Customer.Email)
	subject := "Gift Card Codes"
	t := template.Must(template.New("codes").Parse(tmpl))
	var tpl bytes.Buffer
	if err := t.Execute(&tpl, gin.H{"GiftCards": giftCards}); err != nil {
		return err
	}

	m := mg.NewMessage("donotreply@fishingreservationsystem.com", subject, tpl.String(), fmt.Sprintf("%s <%s>", payment.Customer.Name, payment.Customer.Email))
	m.SetHtml(tpl.String())

	resp, id, err := mg.Send(context.Background(), m)
	log.Println("Send Email: ", subject, payment.Customer.Name, payment.Customer.Email)
	log.Println("response: ", resp, id)

	// content := mail.NewContent("text/html", tpl.String())
	// log.Println("Send Email:", from, subject, to, content)
	// m := mail.NewV3MailInit(from, subject, to, content)
	// request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	// request.Method = "POST"
	// request.Body = mail.GetRequestBody(m)
	// _, err := sendgrid.API(request)
	// if err != nil {
	// 	return err
	// }

	return err
}

func getTransfer(acct string, transfers map[string]*stripe.TransferParams) *stripe.TransferParams {
	t, ok := transfers[acct]
	if !ok {
		t = &stripe.TransferParams{
			Destination: stripe.String(acct),
			Currency:    stripe.String(string(stripe.CurrencyUSD)),
			Amount:      stripe.Int64(0),
		}
		transfers[acct] = t
	}
	return t
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
	apiKey := os.Getenv("MAILGUN_API_KEY")

	return func(c *gin.Context) {
		event := stripe.Event{}
		if err := c.BindJSON(&event); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fmt.Println(event.Type, event.ID)

		switch event.Type {
		case "payment_intent.succeeded":
			var paymentIntent stripe.PaymentIntent
			if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			if paymentIntent.Charges.Data[0].CalculatedStatementDescriptor == "WWW.CAPTREEISLANDSPIRI" {
				mg := mailgun.NewMailgun(mailgunDomain, apiKey)
				subject := "Captree Island Spirit Purchase"
				m := mg.NewMessage("donotreply@fishingreservationsystem.com", subject, string(event.Data.Raw), "jjtape@optonline.net")
				resp, id, err := mg.Send(context.Background(), m)
				log.Println("Sent Captree ISLAND response: ", id, subject, resp)
				if err != nil {
					log.Println("got error: ", err)
				}
				return
			}

			var conf types.MerchantConfig
			db.Find(&conf, "stripe_key = (SELECT acct FROM payment_intents WHERE id = ?)", paymentIntent.ID)

			key := stripe.Key
			if !strings.HasPrefix(conf.StripeKey, "acct_") {
				key = conf.StripeKey
			}
			// details := paymentIntent.Charges.Data[0].BillingDetails
			if paymentIntent.Customer.Name == "" {
				custClient := customer.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
				params := &stripe.CustomerParams{}
				if strings.HasPrefix(conf.StripeKey, "acct_") {
					params.SetStripeAccount(conf.StripeKey)
				}
				cus, err := custClient.Get(paymentIntent.Customer.ID, params)
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

			if gift, ok := paymentIntent.Metadata["giftcard"]; ok {
				amount, _ := strconv.Atoi(paymentIntent.Metadata["amount"])

				db.Model(&types.GiftCard{}).Where("id = ?", gift).UpdateColumn("balance", gorm.Expr("balance - ?", float64(amount)/100.0))
				db.Model(&types.GiftCard{}).Where("id = ?", gift).Update("status", "used")
			}

			// err := sendCustomerEmail(db, apiKey, c.Request.Host, &conf, &paymentIntent)
			// if err != nil {
			// 	c.JSON(http.StatusFailedDependency, gin.H{"err": err.Error()})
			// 	return
			// }

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

			var pi PaymentIntent
			db.Find(&pi, "id = ?", sess.PaymentIntent.ID)

			var conf types.MerchantConfig
			db.Find(&conf, "stripe_key = ?", pi.Acct)

			key := stripe.Key
			if !strings.HasPrefix(pi.Acct, "acct_") {
				key = pi.Acct
			} else {
				paymentParams.SetStripeAccount(pi.Acct)
			}

			piClient := paymentintent.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}

			pm, err := piClient.Get(sess.PaymentIntent.ID, paymentParams)
			if err != nil {
				log.Println(err)
			}

			// var conf types.MerchantConfig
			// db.Find(&conf, "stripe_key = ?", pm.OnBehalfOf.ID)

			itemList := make([]notifyItem, 0)
			transfers := make(map[string]*stripe.TransferParams)

			var giftcardAmount int
			if _, ok := pm.Metadata["giftcard"]; ok {
				giftcardAmount, _ = strconv.Atoi(pm.Metadata["amount"])
			}

			params := &stripe.CheckoutSessionListLineItemsParams{}
			params.AddExpand("data.price")
			params.AddExpand("data.price.product")
			if strings.HasPrefix(pi.Acct, "acct_") {
				params.SetStripeAccount(event.Account)
			}

			// var feeAmount int64
			sessClient := session.Client{B: piClient.B, Key: key}
			i := sessClient.ListLineItems(sess.ID, params)
			for i.Next() {
				li := i.LineItem()
				// if li.Price.Product.Name == feeItemName {
				// 	feeAmount = li.Price.UnitAmount
				// }

				itemList = append(itemList, notifyItem{
					Name:     li.Price.Product.Name,
					Quantity: int(li.Quantity),
				})

				sku := li.Price.Product.Metadata["sku"]

				if strings.HasPrefix(pi.Acct, "acct_") && !strings.HasPrefix(sku, "GIFT") && li.Price.Product.Name != feeItemName {
					pid, _ := strconv.Atoi(sku[:strings.IndexFunc(sku, func(c rune) bool { return !unicode.IsNumber(c) })])

					var prod types.Product
					db.Find(&prod, "id = ?", pid)

					tinfo := strings.Split(conf.StripeAcctMap.Map[strconv.Itoa(int(prod.BoatID))].String, "|")
					if len(tinfo) == 3 {
						amt, _ := strconv.Atoi(tinfo[0])
						pri := tinfo[1]
						sec := tinfo[2]

						pt := getTransfer(pri, transfers)
						ps := getTransfer(sec, transfers)

						amt *= 100
						s := li.Quantity * int64(amt)

						*pt.Amount += (li.Price.UnitAmount * li.Quantity) - s
						*ps.Amount += s
					} else if len(tinfo) == 1 {
						if tinfo[0] != "" {
							t := getTransfer(tinfo[0], transfers)
							*t.Amount += (li.Price.UnitAmount * li.Quantity)
						}
					}
				}

				db.Save(&LineItem{
					ID:        li.ID,
					PaymentID: sess.PaymentIntent.ID,
					Acct:      conf.StripeKey,
					Quantity:  int(li.Quantity),
					Name:      li.Price.Product.Name,
					Sku:       sku,
					Amount:    fmt.Sprintf("%0.2f", float64(li.Price.UnitAmount*li.Quantity)/100.0),
					UnitPrice: fmt.Sprintf("%0.2f", float64(li.Price.UnitAmount)/100.0),
					Status:    string(pm.Status),
				})

				if !strings.HasPrefix(sku, "GIFT") && li.Price.Product.Name != feeItemName {
					re := regexp.MustCompile(`(\d+)[A-Z]+(\d{10})`)
					res := re.FindStringSubmatch(sku)
					pid, _ := strconv.Atoi(res[1])
					timestamp, _ := strconv.ParseInt(res[2], 10, 64)

					tm := time.Unix(timestamp, 0).In(timeloc)
					db.Table("manual_overrides").Where("product_id = ? AND time = ?", pid, tm).
						UpdateColumn("avail", gorm.Expr("avail - ?", li.Quantity))
				}
			}

			// stripeFee := int64(math.Ceil(float64(pm.Amount)*0.029)) + 30
			amtTransferred := int64(0)
			for _, v := range transfers {
				if giftcardAmount == 0 {
					v.SourceTransaction = &pm.Charges.Data[0].ID
				} else {
					v.TransferGroup = &pm.ID
				}
				t, err := transfer.New(v)
				if err != nil {
					c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
					return
				}
				fmt.Println("Transfer:", t.ID, t.Amount, t.Destination)
				amtTransferred += t.Amount
			}

			// feeTransfer := feeAmount - stripeFee
			// if strings.HasPrefix(conf.StripeKey, "acct_") && feeTransfer > 0 {
			// 	log.Println("WTFLOG:", feeAmount, stripeFee)
			// 	transferParams := &stripe.TransferParams{
			// 		Destination:       stripe.String(conf.StripeAcctMap.Map["feeacct"].String),
			// 		SourceTransaction: &pm.Charges.Data[0].ID,
			// 		Amount:            &feeTransfer,
			// 		Currency:          stripe.String(string(stripe.CurrencyUSD)),
			// 	}
			// 	transferParams.SetStripeAccount(conf.StripeKey)
			// 	t, err := transfer.New(transferParams)
			// 	log.Println("fee transfer:", t.ID, t.Amount, err)
			// }

			err = sendCustomerEmail(db, apiKey, c.Request.Host, &conf, pm)
			if err != nil {
				log.Println("customer email error: ", err)
			}

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

			db.Model(&PaymentIntent{}).
				Where("id = ?", charge.PaymentIntent.ID).
				UpdateColumn("status", "refunded")

			db.Model(&types.GiftCard{}).
				Where("payment_id = ?", charge.PaymentIntent.ID).
				UpdateColumn("status", "refunded")

			type pidfind struct {
				Quantity int `gorm:"quantity"`
				Pid      int `gorm:"pid"`
				Tm       int `gorm:"tm"`
			}
			var values []pidfind

			db.Table("line_items").
				Where("payment_id = ? AND sku NOT LIKE 'GIFT%' AND name != 'Fees'", charge.PaymentIntent.ID).
				Select("quantity", `(REGEXP_MATCHES(sku, '(\d+)[A-Z]+(\d{10})\d*'))[1]::INTEGER as pid`, `(REGEXP_MATCHES(sku, '(\d+)[A-Z]+(\d{10})\d*'))[2]::INTEGER as tm`).Scan(&values)

			for _, v := range values {
				db.Table("manual_overrides").
					Where("product_id = ? AND TO_TIMESTAMP(?) = time", v.Pid, v.Tm).
					Update("avail", gorm.Expr("avail - ?", v.Quantity))
			}
		}

		c.Status(http.StatusOK)
	}
}
