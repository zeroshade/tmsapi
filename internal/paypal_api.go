package internal

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

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

	req.Header.Set("Accept", "application/json")
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

	return verifyResponse.Status == "SUCCESS"
}

func (c *Client) GetPaymentCapture(id string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.APIBase+"/v2/payments/captures/"+id, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.SendWithAuth(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func (c *Client) GetCheckoutOrder(id string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.APIBase+"/v2/checkout/orders/"+id, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.SendWithAuth(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func (c *Client) IssueRefund(id string, email string) ([]byte, error) {
	type AuthAssert struct {
		Iss   string `json:"iss"`
		Payer string `json:"payer_id,omitempty"`
		Email string `json:"email,omitempty"`
	}

	auth := AuthAssert{Iss: c.ClientID, Email: email}
	data, err := json.Marshal(&auth)
	if err != nil {
		return nil, err
	}

	authAssert := base64.StdEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.StdEncoding.EncodeToString(data) + "."
	req, err := http.NewRequest(http.MethodPost, c.APIBase+"/v2/payments/captures/"+id+"/refund", bytes.NewReader([]byte{'{', '}'}))
	req.Header.Add("PayPal-Auth-Assertion", authAssert)
	req.Header.Add("Content-Type", "application/json")

	resp, err := c.SendWithAuth(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func (c *Client) CaptureOrder(id string) (*http.Response, error) {
	req, err := http.NewRequest("POST", c.APIBase+"/v2/checkout/orders/"+id+"/capture", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("PayPal-Mock-Response", `{"mock_application_codes": "INSTRUMENT_DECLINED"}`)
	req.Header.Set("Prefer", "return=representation")
	req.Header.Set("Content-Type", "application/json")
	return c.SendWithAuth(req)
}
