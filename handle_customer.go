package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var twilioAccountSid = os.Getenv("TWILIO_ACCOUNT_SID")
var twilioAuthToken = os.Getenv("TWILIO_AUTH_TOKEN")
var twilioMsgingService = os.Getenv("TWILIO_MSGING_SERVICE")

func sendNotifyEmail(apiKey string, conf *types.MerchantConfig, order *types.CheckoutOrder) error {
	log.Println("Send Notify Mail:", order.ID, conf.EmailFrom)
	const tmpl = `
	Tickets Purchased By: {{ .Payer.Name.GivenName }} {{ .Payer.Name.Surname }} <a href='mailto:{{ .Payer.Email }}'>{{ .Payer.Email }}</a>
	<br /><br />
	{{ range .PurchaseUnits -}}
	<ul>
	{{ range .Items -}}
	<li>{{ .Quantity }} {{ .Name }}, {{ .Description }}</li>
	{{- end }}
	</ul>
	{{- end }}`

	t := template.Must(template.New("notify").Parse(tmpl))

	from := mail.NewEmail("Do Not Reply", "donotreply@websbyjoe.org")
	to := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	subject := "Tickets Purchased"
	var tpl bytes.Buffer
	if err := t.Execute(&tpl, order); err != nil {
		return err
	}
	content := mail.NewContent("text/html", tpl.String())

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

func SendClientMail(apiKey, host, email string, order *types.CheckoutOrder, conf *types.MerchantConfig) (*rest.Response, error) {
	type TmplData struct {
		Host          string
		PurchaseUnits []types.PurchaseUnit
		MerchantID    string
		CheckoutID    string
	}

	const tmpl = `
	<br /><br />
	Tickets Ordered:<br/>
	{{ range .PurchaseUnits -}}
	<ul>
	{{ range .Items -}}
	<li>{{ .Quantity }} {{ .Name }}, {{ .Description }}</li>
	{{- end }}
	</ul>
	{{- end }}
	<br />
	You can download your boarding passes here: <a href='https://{{.Host}}/info/{{.MerchantID}}/passes/{{.CheckoutID}}'>Click Here</a>
	<br />`

	log.Println("Send Client Mail:", conf.EmailFrom, email, order.ID)

	from := mail.NewEmail(conf.EmailName, conf.EmailFrom)
	subject := "Tickets Purchased"
	to := mail.NewEmail(order.Payer.Name.GivenName+" "+order.Payer.Name.Surname, email)

	t := template.Must(template.New("notify").Parse(tmpl))
	var tpl bytes.Buffer
	if err := t.Execute(&tpl, TmplData{
		Host:          host,
		PurchaseUnits: order.PurchaseUnits,
		MerchantID:    order.PurchaseUnits[0].Payee.MerchantID,
		CheckoutID:    order.ID}); err != nil {
		return nil, err
	}
	content := mail.NewContent("text/html", conf.EmailContent+tpl.String())

	m := mail.NewV3MailInit(from, subject, to, content)
	request := sendgrid.GetRequest(apiKey, "/v3/mail/send", "https://api.sendgrid.com")
	request.Method = "POST"
	request.Body = mail.GetRequestBody(m)
	response, err := sendgrid.API(request)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func SendText(db *gorm.DB) gin.HandlerFunc {
	type Req struct {
		CheckoutID string `json:"checkoutId"`
		Phone      string `json:"phone"`
	}

	env := internal.SANDBOX
	if strings.ToLower(os.Getenv("PAYPAL_ENV")) == "live" {
		env = internal.LIVE
	}

	return func(c *gin.Context) {
		var r Req
		if err := c.ShouldBindJSON(&r); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(env)
		data, err := paypalClient.GetCheckoutOrder(r.CheckoutID)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var order types.CheckoutOrder
		if err := json.Unmarshal(data, &order); err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var conf types.MerchantConfig
		mid := order.PurchaseUnits[0].Payee.MerchantID
		db.Find(&conf, "id = ?", mid)

		if len(conf.ID) <= 0 {
			db.Table("sandbox_infos").Select("id").Where("? = ANY (sandbox_ids)", mid).Scan(&conf)
			db.Find(&conf)
		}

		t := internal.NewTwilio(conf.TwilioAcctSID, conf.TwilioAcctToken)
		t.Send(r.Phone, "Boarding Passes Link: https://"+c.Request.Host+"/info/"+order.PurchaseUnits[0].Payee.MerchantID+"/passes/"+order.ID)
	}
}

func Resend(db *gorm.DB) gin.HandlerFunc {
	type Req struct {
		CheckoutID string `json:"checkoutId"`
		Email      string `json:"email"`
	}

	apiKey := os.Getenv("SENDGRID_API_KEY")
	env := internal.SANDBOX
	if strings.ToLower(os.Getenv("PAYPAL_ENV")) == "live" {
		env = internal.LIVE
	}

	return func(c *gin.Context) {
		var r Req
		if err := c.ShouldBindJSON(&r); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(env)
		data, err := paypalClient.GetCheckoutOrder(r.CheckoutID)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var order types.CheckoutOrder
		if err := json.Unmarshal(data, &order); err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var conf types.MerchantConfig
		mid := order.PurchaseUnits[0].Payee.MerchantID
		db.Find(&conf, "id = ?", mid)

		if len(conf.ID) <= 0 {
			db.Table("sandbox_infos").Select("id").Where("? = ANY (sandbox_ids)", mid).Scan(&conf)
			db.Find(&conf)
		}

		response, err := SendClientMail(apiKey, c.Request.Host, r.Email, &order, &conf)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		c.Status(response.StatusCode)
	}
}

func ConfirmAndSend(db *gorm.DB) gin.HandlerFunc {
	type ConfReq struct {
		CheckoutId string `json:"checkoutId"`
	}

	apiKey := os.Getenv("SENDGRID_API_KEY")

	env := internal.SANDBOX
	if strings.ToLower(os.Getenv("PAYPAL_ENV")) == "live" {
		env = internal.LIVE
	}
	return func(c *gin.Context) {
		var r ConfReq
		if err := c.ShouldBindJSON(&r); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(env)
		data, err := paypalClient.GetCheckoutOrder(r.CheckoutId)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		var order types.CheckoutOrder
		if err := json.Unmarshal(data, &order); err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		db.Save(&order)

		// re := regexp.MustCompile(`(\d+)[A-Z]+(\d{10})`)

		// for _, pu := range order.PurchaseUnits {
		// 	for _, item := range pu.Items {
		// 		res := re.FindStringSubmatch(item.Sku)
		// 		pid, _ := strconv.Atoi(res[1])
		// 		timestamp, _ := strconv.ParseInt(res[2], 10, 64)

		// 		tm := time.Unix(timestamp, 0).In(timeloc)

		// 		db.Model(ManualOverride{}).Where("product_id = ? AND time = ?", pid, tm).
		// 			UpdateColumn("avail", gorm.Expr("avail - ?", item.Quantity))
		// 	}
		// }

		db.Model(order.Payer).Update(*order.Payer)

		var conf types.MerchantConfig
		mid := order.PurchaseUnits[0].Payee.MerchantID
		db.Find(&conf, "id = ?", mid)

		if len(conf.ID) <= 0 {
			db.Table("sandbox_infos").Select("id").Where("? = ANY (sandbox_ids)", mid).Scan(&conf)
			db.Find(&conf)
		}

		response, err := SendClientMail(apiKey, c.Request.Host, order.Payer.Email, &order, &conf)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		sendNotifyEmail(apiKey, &conf, &order)

		if conf.SendSMS {
			t := internal.NewTwilio(conf.TwilioAcctSID, conf.TwilioAcctToken)
			t.Send(conf.NotifyNumber, "Tickets Purchased by "+order.Payer.Name.GivenName+" "+order.Payer.Name.Surname)
		}

		c.Status(response.StatusCode)
	}
}

func RefundReq(db *gorm.DB) gin.HandlerFunc {
	type ConfReq struct {
		CaptureID string `json:"captureId"`
		Email     string `json:"email"`
	}

	return func(c *gin.Context) {
		var r ConfReq
		if err := c.ShouldBindJSON(&r); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(internal.SANDBOX)
		data, err := paypalClient.IssueRefund(r.CaptureID, r.Email)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		log.Println(string(data))
		c.Data(http.StatusOK, "text/plain", data)
	}
}
