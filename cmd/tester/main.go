package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"

	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
	tms "github.com/zeroshade/tmsapi/types"
)

func main() {
	URI := os.Getenv("DATABASE_URL")
	if URI == "" {
		log.Fatal("must set $DATABASE_URL")
	}

	db, err := gorm.Open("postgres", URI+"?timezone=America/New_York")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.Model(&tms.Schedule{}).Association("TimeArray")
	db.Model(&tms.Schedule{}).Association("NotAvail")
	db.Model(&tms.Payment{}).Association("Payer.PayerInfo")
	db.Model(&tms.Item{}).AddForeignKey("transaction", "transactions(payment_id)", "CASCADE", "RESTRICT")
	db.Model(&tms.Transaction{}).AddForeignKey("payment_id", "payments(id)", "CASCADE", "RESTRICT")
	db.Table("transaction_related").AddForeignKey("transaction_payment_id", "payments(id)", "CASCADE", "RESTRICT")
	db.Table("transaction_related").AddForeignKey("sale_id", "sales(id)", "CASCADE", "RESTRICT")
	db.Model(&tms.PurchaseUnit{}).AddForeignKey("checkout_id", "checkout_orders(id)", "CASCADE", "RESTRICT")
	db.Model(&tms.PurchaseItem{}).AddForeignKey("checkout_id", "checkout_orders(id)", "CASCADE", "RESTRICT")

	var caps []tms.Capture
	db.Find(&caps, "checkout_id = '' AND status = 'COMPLETED'")

	paypalClient := internal.NewClient(internal.LIVE)
	for _, c := range caps {
		data, err := paypalClient.GetPaymentCapture(c.ID)
		if err != nil {
			log.Fatal(err)
		}

		var capture tms.Capture
		err = json.Unmarshal(data, &capture)
		if err != nil {
			log.Fatal(err)
		}

		for _, l := range capture.Links {
			if l.Rel == "up" {
				req, _ := http.NewRequest("GET", l.Href, nil)
				resp, err := paypalClient.SendWithAuth(req)
				if err != nil {
					log.Fatal(err)
				}

				dec := json.NewDecoder(resp.Body)
				var checkout types.CheckoutOrder
				if err = dec.Decode(&checkout); err != nil {
					log.Fatal(err)
				}

				fmt.Println(c.ID, ":", checkout.ID)
				db.Save(&checkout)
				c.CheckoutID = checkout.ID
				db.Save(&c)
				break
			}
		}
	}

}
