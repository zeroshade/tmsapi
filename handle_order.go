package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

type CaptureResponse struct {
	ID            string               `json:"id"`
	Status        string               `json:"status"`
	Payer         types.Payer          `json:"payer"`
	PurchaseUnits []types.PurchaseUnit `json:"purchase_units"`
	Links         []types.Link         `json:"links"`
}

type FailedCapture struct {
	Name    string `json:"name"`
	Details []struct {
		Issue string `json:"issue"`
		Desc  string `json:"description"`
	} `json:"details"`
	Message string       `json:"message"`
	DebugID string       `json:"debug_id"`
	Links   []types.Link `json:"links"`
}

func CaptureOrder(db *gorm.DB) gin.HandlerFunc {
	env := internal.SANDBOX
	if strings.ToLower(os.Getenv("PAYPAL_ENV")) == "live" {
		env = internal.LIVE
	}

	type CaptureReq struct {
		OrderID string `json:"orderId"`
	}

	return func(c *gin.Context) {
		var cr CaptureReq
		if err := c.ShouldBindJSON(&cr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		paypalClient := internal.NewClient(env)
		resp, err := paypalClient.CaptureOrder(cr.OrderID)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		dec := json.NewDecoder(resp.Body)
		if resp.StatusCode == http.StatusOK {
			var r CaptureResponse
			if err = dec.Decode(&r); err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, r)
		} else {
			var f FailedCapture
			if err = dec.Decode(&f); err != nil {
				c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
				return
			}
			c.JSON(resp.StatusCode, f)
		}
	}
}
