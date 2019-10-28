package main

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"hash/crc32"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const WEBHOOK_ID = ""

func HandlePaypalWebhook() gin.HandlerFunc {
	return func(c *gin.Context) {
		sig := c.GetHeader("PAYPAL-TRANSMISSION-SIG")
		certurl := c.GetHeader("PAYPAL-CERT-URL")

		transmissionid := c.GetHeader("PAYPAL-TRANSMISSION-ID")
		timestamp := c.GetHeader("PAYPAL-TRANSMISSION-TIME")
		webhookid := WEBHOOK_ID

		defer c.Request.Body.Close()
		body, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
			c.Abort()
			return
		}

		cert, err := GetCert(certurl)
		if err != nil {
			c.Abort()
			return
		}

		if !VerifySig(cert, transmissionid, timestamp, webhookid, sig, body) {
			c.Abort()
			return
		}

		c.Status(http.StatusOK)
	}
}

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

func VerifySig(cert *x509.Certificate, transid, timestamp, webhookid, sig string, body []byte) bool {
	crc := crc32.ChecksumIEEE(body)
	expectsig := strings.Join([]string{transid, timestamp, webhookid, strconv.Itoa(int(crc))}, "|")

	data, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return false
	}

	return cert.CheckSignature(cert.SignatureAlgorithm, []byte(expectsig), data) == nil
}
