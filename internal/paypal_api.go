package internal

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const SampleVerify = `{"auth_algo":"SHA256withRSA","cert_url":"https://api.sandbox.paypal.com/v1/notifications/certs/CERT-360caa42-fca2a594-1d93a270","transmission_id":"da342df0-feb1-11e9-988a-1d900e460095","transmission_sig":"JayJVkR+n1rx7v6EoZKqK+PcSnxWq2v6yZ1kD2Xr2+KfmiSVrOcP5DZzO0lRxdysnVxkGoC+tsik3T/tTha96Ksk9O0gD3NTJ+7gUZtRpGxbW/105Qc+LgOf6s31CvV+8rTYkoy+k3npFbd6PoLFsIlHrAYaEYDluKCKjtnkw5xzMcPrpOSv1XMSkwfjiLMxNxxPzTH7dg6PMn2XqyviNzibGosnrGpDR7JCeOI9Dr9c4S/m1YkKh8UMV5rroja6tVN0YAIE5qiCptyOQ0LhfZloqoMwOGlxck1tPcqSoUIJfrc6G0V/XlRm8K7lK4OBvdb18NbPLoK+UmWHc4V8NQ==","transmission_time":"2019-11-04T03:19:03Z","webhook_id":"7U044880SP1490548","webhook_event":{"id":"WH-4K046300055743843-1PK45233CS079245P","event_version":"1.0","create_time":"2019-11-04T03:18:37.097Z","resource_type":"sale","event_type":"PAYMENT.SALE.COMPLETED","summary":"Payment completed for $ 460.0 USD","resource":{"id":"8J065383AJ951564P","state":"completed","amount":{"total":"460.00","currency":"USD","details":{"subtotal":"460.00"}},"payment_mode":"INSTANT_TRANSFER","protection_eligibility":"ELIGIBLE","protection_eligibility_type":"ITEM_NOT_RECEIVED_ELIGIBLE,UNAUTHORIZED_PAYMENT_ELIGIBLE","transaction_fee":{"value":"13.64","currency":"USD"},"invoice_number":"","parent_payment":"PAYID-LW7ZQ3Y3NN02226HP022783H","create_time":"2019-11-04T03:18:32Z","update_time":"2019-11-04T03:18:32Z","links":[{"href":"https://api.sandbox.paypal.com/v1/payments/sale/8J065383AJ951564P","rel":"self","method":"GET"},{"href":"https://api.sandbox.paypal.com/v1/payments/sale/8J065383AJ951564P/refund","rel":"refund","method":"POST"},{"href":"https://api.sandbox.paypal.com/v1/payments/payment/PAYID-LW7ZQ3Y3NN02226HP022783H","rel":"parent_payment","method":"GET"}]},"links":[{"href":"https://api.sandbox.paypal.com/v1/notifications/webhooks-events/WH-4K046300055743843-1PK45233CS079245P","rel":"self","method":"GET"},{"href":"https://api.sandbox.paypal.com/v1/notifications/webhooks-events/WH-4K046300055743843-1PK45233CS079245P/resend","rel":"resend","method":"POST"}]}}`

const LIVE_URI = "https://api.paypal.com"
const SANDBOX_URI = "https://api.sandbox.paypal.com"

type TokenResponse struct {
	Scope       string `json:"scope"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	AppID       string `json:"app_id"`
	ExpiresIn   int    `json:"expires_in"`
	Nonce       string `json:"nonce"`
}

type Client struct {
	Client         *http.Client
	ClientID       string
	Secret         string
	APIBase        string
	Token          *TokenResponse
	tokenExpiresAt time.Time
}

type Env int

const (
	SANDBOX Env = iota
	LIVE
)

func NewClient(env Env) *Client {
	clientID := os.Getenv("PAYPAL_CLIENT_ID")
	clientSecret := os.Getenv("PAYPAL_CLIENT_SECRET")

	api := SANDBOX_URI
	if env == LIVE {
		api = LIVE_URI
	}

	return &Client{
		Client:   &http.Client{},
		ClientID: clientID,
		Secret:   clientSecret,
		APIBase:  api,
		Token:    &TokenResponse{},
	}
}

func (c *Client) getAccessToken() error {
	req, _ := http.NewRequest("POST", c.APIBase+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	req.SetBasicAuth(c.ClientID, c.Secret)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Accept-Language", "en_US")
	req.Header.Set("Content-type", "application/x-www-form-urlencoded")

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if err = dec.Decode(c.Token); err != nil {
		return err
	}

	c.tokenExpiresAt = time.Now().Add(time.Duration(c.Token.ExpiresIn) * time.Second)
	return nil
}

func (c *Client) SendWithAuth(req *http.Request) (*http.Response, error) {
	if c.tokenExpiresAt.IsZero() || time.Until(c.tokenExpiresAt) < 30*time.Second {
		if err := c.getAccessToken(); err != nil {
			return nil, err
		}
	}

	req.Header.Set("Authorization", "Bearer "+c.Token.AccessToken)
	return c.Client.Do(req)
}

func (c *Client) VerifyWebHookSig(req *http.Request, webhookID string) bool {
	type verifyWebhookRequest struct {
		AuthAlgo         string          `json:"auth_algo"`
		CertURL          string          `json:"cert_url"`
		TransmissionID   string          `json:"transmission_id"`
		TransmissionSig  string          `json:"transmission_sig"`
		TransmissionTime string          `json:"transmission_time"`
		WebhookID        string          `json:"webhook_id"`
		WebhookEvent     json.RawMessage `json:"webhook_event"`
	}

	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = ioutil.ReadAll(req.Body)
	}
	// Restore the io.ReadCloser to its original state
	req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

	verifyReq := verifyWebhookRequest{
		AuthAlgo:         req.Header.Get("PAYPAL-AUTH-ALGO"),
		CertURL:          req.Header.Get("PAYPAL-CERT-URL"),
		TransmissionID:   req.Header.Get("PAYPAL-TRANSMISSION-ID"),
		TransmissionSig:  req.Header.Get("PAYPAL-TRANSMISSION-SIG"),
		TransmissionTime: req.Header.Get("PAYPAL-TRANSMISSION-TIME"),
		WebhookID:        webhookID,
		WebhookEvent:     json.RawMessage(bodyBytes),
	}

	type VerifyWebhookResponse struct {
		Status string `json:"verification_status"`
	}

	b, err := json.Marshal(&verifyReq)
	if err != nil {
		log.Println(err)
		return false
	}

	log.Println(string(b))

	vreq, err := http.NewRequest("POST", c.APIBase+"/v1/notifications/verify-webhook-signature", bytes.NewBuffer(b))
	vreq.Header.Set("Content-Type", "application/json")
	resp, err := c.SendWithAuth(vreq)

	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	verifyResponse := VerifyWebhookResponse{}
	if err = dec.Decode(&verifyResponse); err != nil {
		log.Println(err)
		return false
	}

	log.Println(verifyResponse)
	return verifyResponse.Status == "SUCCESS"
}
