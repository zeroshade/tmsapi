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
	"github.com/gin-gonic/gin"
)

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

    log.Println(string(body))
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
