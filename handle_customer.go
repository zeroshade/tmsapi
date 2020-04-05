package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var twilioAccountSid = os.Getenv("TWILIO_ACCOUNT_SID")
var twilioAuthToken = os.Getenv("TWILIO_AUTH_TOKEN")
var twilioMsgingService = os.Getenv("TWILIO_MSGING_SERVICE")
var twilioMsgFrom = os.Getenv("TWILIO_MSG_FROM")

func sendTwilio(to, body string) error {
	msgData := url.Values{}
	msgData.Set("To", to)
	msgData.Set("From", twilioMsgFrom)
	msgData.Set("Body", body)

	twilioApiUrl := "https://api.twilio.com/2010-04-01/Accounts/" + twilioAccountSid + "/Messages.json"

	client := &http.Client{}
	req, _ := http.NewRequest("POST", twilioApiUrl, strings.NewReader(msgData.Encode()))
	req.SetBasicAuth(twilioAccountSid, twilioAuthToken)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, _ := client.Do(req)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var data map[string]interface{}
		defer resp.Body.Close()
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&data); err != nil {
			return err
		}
		log.Println("Twilio Notification set to: ", to, " sid: ", data["sid"])
	} else {
		log.Println("Twilio SMS: ", resp.Status)
	}
	return nil
}

func sendNotifyEmail(apiKey string, conf *MerchantConfig, order *CheckoutOrder) error {
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

		db.Save(&order)

		re := regexp.MustCompile(`(\d+)[A-Z]+(\d{10})`)

		for _, pu := range order.PurchaseUnits {
			for _, item := range pu.Items {
				res := re.FindStringSubmatch(item.Sku)
				pid, _ := strconv.Atoi(res[1])
				timestamp, _ := strconv.ParseInt(res[2], 10, 64)

				tm := time.Unix(timestamp, 0).In(timeloc)

				db.Model(ManualOverride{}).Where("product_id = ? AND time = ?", pid, tm).
					UpdateColumn("avail", gorm.Expr("avail - ?", item.Quantity))
			}
		}

		var conf MerchantConfig
		mid := order.PurchaseUnits[0].Payee.MerchantID
		db.Find(&conf, "id = ?", mid)

		if len(conf.ID) <= 0 {
			db.Table("sandbox_infos").Select("id").Where("? = ANY (sandbox_ids)", mid).Scan(&conf)
			db.Find(&conf)
		}

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

		sendNotifyEmail(apiKey, &conf, &order)

		if conf.SendSMS {
			sendTwilio(conf.NotifyNumber, "Tickets Purchased by "+order.Payer.Name.GivenName+" "+order.Payer.Name.Surname)
		}

		c.Status(response.StatusCode)
	}
}
