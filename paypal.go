package main

import (
  "os"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"hash/crc32"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
  "log"
  "time"
  "encoding/json"
	"github.com/gin-gonic/gin"
)

type amount struct {
	Total    float32 `json:"total,string"`
	Currency string  `json:"currency"`
}

type link struct {
	Href    string `json:"href"`
	Rel     string `json:"rel"`
	Method  string `json:"method"`
	EncType string `json:"encType"`
}

type cutime struct {
	UpdateTime time.Time `json:"update_time"`
	CreateTime time.Time `json:"create_time"`
}

type PayerInfo struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	PayerId   string `json:"payer_id"`
	Phone     string `json:"phone"`
	Country   string `json:"country_code"`
}

type WebHookEvent struct {
	Id           string    `json:"id"`
	CreateTime   time.Time `json:"create_time"`
	ResourceType string    `json:"resource_type"`
	EventType    string    `json:"event_type"`
	Summary      string    `json:"summary"`
	Resource     struct {
		cutime
		Links        []link `json:"links"`
		Id           string `json:"id"`
		State        string `json:"state"`
		Transactions []struct {
			Amount amount `json:"amount"`
			Payee  struct {
				MerchantId string `json:"merchant_id"`
				Email      string `json:"email"`
			} `json:"payee"`
			Desc     string `json:"description"`
			SoftDesc string `json:"soft_descriptor"`
			ItemList []struct {
				Items []struct {
					Name     string  `json:"name"`
					Sku      string  `json:"sku"`
					Price    float32 `json:"price,string"`
					Currency string  `json:"currency"`
					Tax      float32 `json:"tax,string"`
					Qty      uint32  `json:"quantity"`
				} `json:"items"`
			} `json:"item_list"`
			RelatedResources []struct {
				Sale struct {
					cutime
					Id              string `json:"id"`
					State           string `json:"state"`
					Amount          amount `json:"amount"`
					PaymentMode     string `json:"payment_mode"`
					ProtectEligible string `json:"protection_eligibility"`
					TransactionFee  struct {
						Value    float32 `json:"value,string"`
						Currency string  `json:"currency"`
					} `json:"transaction_fee"`
					ParentPayment string `json:"parent_payment"`
					Links         []link `json:"links"`
					SoftDesc      string `json:"soft_descriptor"`
				} `json:"sale"`
			} `json:"related_resources"`
		} `json:"transactions"`
		Intent string `json:"intent"`
		Payer  struct {
			PaymentMethod string    `json:"payment_method"`
			Status        string    `json:"status"`
			PayerInfo     PayerInfo `json:"payer_info"`
		} `json:"payer"`
		Cart string `json:"cart"`
	} `json:"resource"`
	Status        string `json:"status"`
	Transmissions []struct {
		WebhookUrl     string `json:"webhook_url"`
		TransmissionId string `json:"transmission_id"`
		Status         string `json:"status"`
	} `json:"transmissions"`
	Links        []link `json:"links"`
	EventVersion string `json:"event_version"`
}

// WebhookID is the constant id from PayPal for this webhook
var WebhookID string

func init() {
  WebhookID = os.Getenv("WEBHOOK_ID")
}

// HandlePaypalWebhook returns a handler function that verifies a paypal webhook
// post request and then processes the event message
func HandlePaypalWebhook() gin.HandlerFunc {
	return func(c *gin.Context) {
		sig := c.GetHeader("PAYPAL-TRANSMISSION-SIG")
		certurl := c.GetHeader("PAYPAL-CERT-URL")

		transmissionid := c.GetHeader("PAYPAL-TRANSMISSION-ID")
		timestamp := c.GetHeader("PAYPAL-TRANSMISSION-TIME")
		webhookid := WebhookID

		defer c.Request.Body.Close()
		body, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
      log.Println(err)
      c.Status(http.StatusBadRequest)
			return
		}

		cert, err := GetCert(certurl)
		if err != nil {
      log.Println(err)
      c.Status(http.StatusBadRequest)
			return
		}

		if !VerifySig(cert, transmissionid, timestamp, webhookid, sig, body) {
      log.Println("Didn't Verify")
      c.Status(http.StatusBadRequest)
			return
		}

    var we WebHookEvent
    json.Unmarshal(body, &we)
    log.Println(we)

		c.Status(http.StatusOK)
	}
}

// GetCert retrieves the PEM certificate using the URL that was provided
func GetCert(certurl string) (*x509.Certificate, error) {
	resp, err := http.Get(certurl)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(body)
	return x509.ParseCertificate(block.Bytes)
}

// VerifySig takes in the certificate and necessary data to validate the signature that
// was provided for this webhook post request
func VerifySig(cert *x509.Certificate, transid, timestamp, webhookid, sig string, body []byte) bool {
	if cert == nil {
		return false
	}

	crc := crc32.ChecksumIEEE(body)
	expectsig := strings.Join([]string{transid, timestamp, webhookid, strconv.Itoa(int(crc))}, "|")

	data, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return false
	}

	return cert.CheckSignature(cert.SignatureAlgorithm, []byte(expectsig), data) == nil
}
