package internal

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var twilioMsgFrom = os.Getenv("TWILIO_MSG_FROM")

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

func (t *twilio) Send(to, body string) error {
	msgData := url.Values{}
	msgData.Set("To", to)
	msgData.Set("From", t.from)
	msgData.Set("Body", body)

	twilioApiUrl := "https://api.twilio.com/2010-04-01/Accounts/" + t.sid + "/Messages.json"

	client := &http.Client{}
	req, _ := http.NewRequest("POST", twilioApiUrl, strings.NewReader(msgData.Encode()))
	req.SetBasicAuth(t.sid, t.token)
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
