package internal

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var twilioMsgFrom = os.Getenv("TWILIO_MSG_FROM")
var twilioMsgSvcSID = os.Getenv("TWILIO_MSGING_SERVICE")
var twilioSID = os.Getenv("TWILIO_ACCOUNT_SID")
var twilioToken = os.Getenv("TWILIO_AUTH_TOKEN")

type twilio struct {
	sid   string
	token string
	from  string
}

func NewTwilio(sid, token, from string) *twilio {
	return &twilio{
		sid:   sid,
		token: token,
		from:  from,
	}
}

func NewDefaultTwilio() *twilio {
	return &twilio{
		sid:   twilioSID,
		token: twilioToken,
		from:  twilioMsgFrom,
	}
}

func (t *twilio) Send(to, body string) error {
	msgData := url.Values{}
	msgData.Set("To", "+"+to)
	msgData.Set("MessagingServiceSid", twilioMsgSvcSID)
	msgData.Set("Body", body)

	twilioApiUrl := "https://api.twilio.com/2010-04-01/Accounts/" + t.sid + "/Messages.json"

	client := &http.Client{}
	req, _ := http.NewRequest("POST", twilioApiUrl, strings.NewReader(msgData.Encode()))
	req.SetBasicAuth(t.sid, t.token)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return err
	}
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
