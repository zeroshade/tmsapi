package stripe

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/checkout/session"
	"github.com/stripe/stripe-go/v72/paymentintent"
	"github.com/stripe/stripe-go/v72/price"
	"github.com/stripe/stripe-go/v72/product"
)

type DepositSchedule struct {
	Days     []int    `json:"days"`
	NotAvail []string `json:"notAvail"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
	Price    string   `json:"price"`
	Minimum  int      `json:"minimum"`
	Times    []string `json:"times"`
}

type DepositProduct struct {
	ID        string            `json:"stripeId"`
	Name      string            `json:"name"`
	Desc      string            `json:"desc"`
	Color     string            `json:"color"`
	Publish   bool              `json:"publish"`
	BoatID    uint              `json:"boatId"`
	Type      string            `json:"type"`
	Prices    []DepositPrice    `json:"prices"`
	Schedules []DepositSchedule `json:"schedules"`
}

type DepositPrice struct {
	ID         string `json:"id"`
	Product    string `json:"product"`
	NickName   string `json:"name"`
	UnitAmount uint   `json:"amount"`
}

func GetProducts(c *gin.Context) {
	key := stripe.Key
	sk := c.GetString("stripe_acct")
	if !strings.HasPrefix(sk, "acct_") {
		key = sk
		sk = ""
	}

	priceClient := price.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
	priceParams := &stripe.PriceListParams{}
	priceParams.SetStripeAccount(sk)
	priceParams.Context = c.Request.Context()

	pclient := product.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
	params := &stripe.ProductListParams{}
	params.SetStripeAccount(sk)
	params.Context = c.Request.Context()

	type prodmeta struct {
		Days  [][]int `json:"days_list"`
		Dates []struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"dates_list"`
		Prices   []string   `json:"prices_list"`
		Minimums []int      `json:"minimum_list"`
		Times    [][]string `json:"times_list"`
	}

	var tmpmeta prodmeta

	pitr := pclient.List(params)
	prods := []DepositProduct{}
	for pitr.Next() {
		cur := pitr.Product()
		meta := cur.Metadata

		if _, ok := meta["istms"]; !ok {
			continue
		}

		priceParams.Product = &cur.ID
		priceList := []DepositPrice{}
		priceItr := priceClient.List(priceParams)
		for priceItr.Next() {
			p := priceItr.Price()
			priceList = append(priceList, DepositPrice{
				ID:         p.ID,
				NickName:   p.Nickname,
				UnitAmount: uint(p.UnitAmount),
				Product:    cur.ID,
			})
		}

		json.Unmarshal([]byte(meta["dates_list"]), &tmpmeta.Dates)
		json.Unmarshal([]byte(meta["days_list"]), &tmpmeta.Days)
		json.Unmarshal([]byte(meta["prices_list"]), &tmpmeta.Prices)
		json.Unmarshal([]byte(meta["times_list"]), &tmpmeta.Times)
		json.Unmarshal([]byte(meta["minimum_list"]), &tmpmeta.Minimums)

		scheds := make([]DepositSchedule, 0)
		for i := range tmpmeta.Days {
			scheds = append(scheds, DepositSchedule{
				Days:     tmpmeta.Days[i],
				NotAvail: []string{},
				Start:    tmpmeta.Dates[i].Start,
				End:      tmpmeta.Dates[i].End,
				Price:    tmpmeta.Prices[i],
				Times:    tmpmeta.Times[i],
				Minimum:  tmpmeta.Minimums[i],
			})
		}

		bid, _ := strconv.Atoi(meta["boat_id"])
		prods = append(prods, DepositProduct{
			ID:        cur.ID,
			Name:      cur.Name,
			Desc:      cur.Description,
			Publish:   cur.Active,
			BoatID:    uint(bid),
			Type:      "stripe",
			Color:     meta["color"],
			Prices:    priceList,
			Schedules: scheds,
		})
	}

	c.JSON(http.StatusOK, prods)
}

type CreateDepositCheckout struct {
	Date       string `json:"date"`
	Time       string `json:"time"`
	PriceID    string `json:"priceId"`
	TripLength int    `json:"tripLength"`
	TripType   string `json:"tripType"`
}

func CheckoutDeposit(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateDepositCheckout
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		key := stripe.Key
		sk := c.GetString("stripe_acct")
		if !strings.HasPrefix(sk, "acct_") {
			key = sk
			sk = ""
		}

		pcl := price.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
		priceParams := &stripe.PriceParams{}
		priceParams.SetStripeAccount(sk)
		p, err := pcl.Get(req.PriceID, priceParams)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		params := &stripe.CheckoutSessionParams{
			PhoneNumberCollection: &stripe.CheckoutSessionPhoneNumberCollectionParams{
				Enabled: stripe.Bool(true),
			},
			SubmitType: stripe.String("book"),
			Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
			LineItems: []*stripe.CheckoutSessionLineItemParams{
				{
					Price:    &req.PriceID,
					Quantity: stripe.Int64(1),
				},
			},
			SuccessURL: stripe.String(c.Request.Header.Get("x-calendar-origin") + "?status=success&stripe_session_id={CHECKOUT_SESSION_ID}"),
			CancelURL:  stripe.String(c.Request.Header.Get("x-calendar-origin") + "?status=cancelled&stripe_session_id={CHECKOUT_SESSION_ID}"),
		}

		feePct := c.GetFloat64("fee_pct")
		fee := int64(p.UnitAmountDecimal * feePct)

		if fee > 0 {
			params.LineItems = append(params.LineItems, &stripe.CheckoutSessionLineItemParams{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(stripe.CurrencyUSD)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(feeItemName),
					},
					UnitAmount: stripe.Int64(fee),
				},
			})
		}

		params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
			Description: stripe.String(fmt.Sprintf("Deposit for %d hour %s trip, %s %s", req.TripLength, req.TripType, req.Date, req.Time)),
			Metadata: map[string]string{
				"yearmonth": req.Date[:len(req.Date)-3],
				"date":      req.Date, "time": req.Time,
				"length": strconv.Itoa(req.TripLength),
			},
		}

		stripeFee := int64(math.Ceil(float64(p.UnitAmount+fee)*0.029)) + 30
		if fee > stripeFee {
			params.PaymentIntentData.ApplicationFeeAmount = stripe.Int64(fee - stripeFee)
		}
		params.SetStripeAccount(sk)

		sessClient := session.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
		sess, err := sessClient.New(params)
		if err != nil {
			c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
			return
		}

		db.Save(&PaymentIntent{
			ID:   sess.PaymentIntent.ID,
			Acct: sk,
		})

		data := createCheckoutSessionResponse{SessionID: sess.ID}
		c.JSON(http.StatusOK, data)
	}
}

type ManualDeposit struct {
	MerchantID string
	Date       string
	Time       string
	Length     uint
	Name       string
	Email      string
	Phone      string
}

type DepositSearchResult struct {
	ID       string            `json:"id"`
	Desc     string            `json:"description"`
	Metadata map[string]string `json:"metadata"`
}

func GetDeposits(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := stripe.Key
		sk := c.GetString("stripe_acct")
		if !strings.HasPrefix(sk, "acct_") {
			key = sk
			sk = ""
		}

		piclient := paymentintent.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
		params := &stripe.PaymentIntentSearchParams{}
		params.SetStripeAccount(sk)
		params.Context = c.Request.Context()
		params.Query = `metadata["yearmonth"]:"` + c.Param("yearmonth") + `"`
		sitr := piclient.Search(params)
		res := []DepositSearchResult{}

		for sitr.Next() {
			pi := sitr.PaymentIntent()
			res = append(res, DepositSearchResult{
				ID: pi.ID, Desc: pi.Description,
				Metadata: pi.Metadata,
			})
		}

		var deps []ManualDeposit
		db.Find(&deps, `to_date(date, 'YYYY-MM-DD') BETWEEN ? AND (?::date + '1 month'::interval)`, c.Param("yearmonth")+"-01", c.Param("yearmonth")+"-01")

		for _, d := range deps {
			res = append(res, DepositSearchResult{
				ID: "manual", Desc: "",
				Metadata: map[string]string{
					"yearmonth": c.Param("yearmonth"),
					"date":      d.Date,
					"time":      d.Time,
					"length":    strconv.Itoa(int(d.Length)),
				},
			})
		}
		c.JSON(http.StatusOK, res)
	}
}

func GetDepositOrders(c *gin.Context) {
	key := stripe.Key
	sk := c.GetString("stripe_acct")
	if !strings.HasPrefix(sk, "acct_") {
		key = sk
		sk = ""
	}

	piclient := paymentintent.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}
	params := &stripe.PaymentIntentSearchParams{}
	params.SetStripeAccount(sk)
	params.AddExpand("data.customer")
	params.Context = c.Request.Context()
	params.Query = `status:"succeeded"`
	sitr := piclient.Search(params)
	res := []*stripe.PaymentIntent{}
	for sitr.Next() {
		if strings.HasPrefix(sitr.PaymentIntent().Description, "Deposit") {
			res = append(res, sitr.PaymentIntent())
		}
	}
	c.JSON(http.StatusOK, res)
}