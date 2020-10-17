package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

func main() {
	env := internal.LIVE
	paypalClient := internal.NewClient(env)

	db, _ := gorm.Open("postgres", os.Getenv("DATABASE_URL")+"?timezone=America/New_York")
	defer db.Close()

	var caps []types.Capture
	db.Find(&caps, "checkout_id = '' AND create_time > '10-17-2020'")

	var cap types.Capture
	var order types.CheckoutOrder
	for _, c := range caps {
		resp, err := paypalClient.GetPaymentCapture(c.ID)
		if err != nil {
			log.Fatal(err)
		}

		if err = json.Unmarshal(resp, &cap); err != nil {
			log.Fatal(err)
		}

		for _, l := range cap.Links {
			if l.Rel == "up" {
				req, err := http.NewRequest("GET", l.Href, nil)
				if err != nil {
					log.Fatal(err)
				}

				resp, err := paypalClient.SendWithAuth(req)
				if err != nil {
					log.Fatal(err)
				}
				defer resp.Body.Close()

				data, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.Fatal(err)
				}

				if err = json.Unmarshal(data, &order); err != nil {
					log.Fatal(err)
				}

				db.Create(&order)
				break
			}
		}
	}
}
