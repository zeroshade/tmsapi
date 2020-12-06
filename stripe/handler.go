package stripe

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/stripe/stripe-go/v71"
	"github.com/stripe/stripe-go/v71/paymentintent"
	"github.com/stripe/stripe-go/v71/refund"
	"github.com/stripe/stripe-go/v71/reversal"
	"github.com/stripe/stripe-go/v71/transfer"
	"github.com/zeroshade/tmsapi/types"
)

var timeloc *time.Location

func init() {
	timeloc, _ = time.LoadLocation("America/New_York")
}

type Handler struct{}

func (h Handler) OrdersTimestamp(config *types.MerchantConfig, db *gorm.DB, timestamp string) (interface{}, error) {
	type Ret struct {
		ID        string    `json:"id"`
		PaymentID string    `json:"paymentId"`
		Acct      string    `json:"-"`
		Quantity  uint      `json:"qty"`
		Prod      string    `json:"name"`
		Name      string    `json:"payer"`
		Email     string    `json:"email"`
		Phone     string    `json:"phone"`
		CreatedAt time.Time `json:"created"`
		Sku       string    `json:"sku"`
		Status    string    `json:"status"`
	}

	var ret []Ret
	db.Table("line_items AS li").
		Joins("LEFT JOIN payment_intents AS pi ON (pi.id = li.payment_id)").
		Where("li.acct = ? AND SUBSTRING(li.sku FROM '\\d+[A-Z]+(\\d{10})\\d*') = ?", config.StripeKey, timestamp).
		Select("li.id, payment_id, li.acct, quantity, sku, li.name AS prod, pi.name, pi.email, created_at, li.status").
		Scan(&ret)

	piCustomerMap := make(map[string]*stripe.Customer)
	params := &stripe.PaymentIntentParams{}
	params.AddExpand("customer")
	for idx := range ret {
		r := &ret[idx]
		var (
			cus *stripe.Customer
			ok  bool
		)
		if cus, ok = piCustomerMap[r.PaymentID]; !ok {
			pi, err := paymentintent.Get(r.PaymentID, params)
			if err != nil {
				log.Println("PI:", err)
				continue
			}
			cus = pi.Customer
			piCustomerMap[r.PaymentID] = cus
		}

		if cus.Email != "" {
			r.Email = cus.Email
		}
		if cus.Phone != "" {
			r.Phone = cus.Phone
		}
		if cus.Name != "" {
			r.Name = cus.Name
		}
	}
	return ret, nil
}

func (h Handler) GetSoldTickets(config *types.MerchantConfig, db *gorm.DB, from, to string) (interface{}, error) {
	type result struct {
		Stamp time.Time `json:"stamp"`
		Qty   uint      `json:"qty"`
		Pid   uint      `json:"pid"`
	}

	fromSku := "TO_TIMESTAMP(SUBSTRING(sku FROM '\\d[A-Z]+(\\d{10})\\d*')::INTEGER)"

	var out []result
	db.Model(&LineItem{}).
		Select(`(regexp_matches(sku, '^\d+'))[1]::integer as pid, `+fromSku+` as stamp, SUM(quantity) AS qty`).
		Where("acct = ? AND "+fromSku+" BETWEEN TO_TIMESTAMP(?) AND TO_TIMESTAMP(?)",
			config.StripeKey, from, to).
		Group("pid, stamp").
		Scan(&out)

	for idx, o := range out {
		out[idx].Stamp = o.Stamp.In(timeloc)
	}

	return out, nil
}

type RefundInfo struct {
	PaymentIntentID string `json:"paymentId"`
	LineItemID      string `json:"itemId"`
}

func (h Handler) RefundTickets(config *types.MerchantConfig, db *gorm.DB, data json.RawMessage) (interface{}, error) {
	info := make([]RefundInfo, 0)
	err := json.Unmarshal(data, &info)
	if err != nil {
		return nil, err
	}

	for _, i := range info {
		var item LineItem
		db.Find(&item, &LineItem{ID: i.LineItemID, PaymentID: i.PaymentIntentID, Acct: config.StripeKey})

		params := &stripe.PaymentIntentParams{}
		pi, err := paymentintent.Get(item.PaymentID, params)
		if err != nil {
			return nil, err
		}

		fmt.Printf("%+v\n", pi.Charges.Data[0])

		amt, err := strconv.ParseFloat(item.Amount[1:], 32)
		if err != nil {
			fmt.Println(err)
		}

		iter := transfer.List(&stripe.TransferListParams{TransferGroup: &pi.TransferGroup})
		for iter.Next() {
			acctID := iter.Transfer().Destination.ID

			// fmt.Println(amt, acctID, iter.Transfer().Amount)
			var reverseAmount int64

			switch acctID {
			case config.StripeKey:
				reverseAmount = int64(int(amt*100) - (item.Quantity * 500))
				// fmt.Println("Refund:", int(amt*100)-(item.Quantity*500), acctID)
			case config.StripeSecondary:
				reverseAmount = int64(item.Quantity * 500)
				// fmt.Println("Refund:", item.Quantity*500, acctID)
			}

			rev, err := reversal.New(&stripe.ReversalParams{
				Amount:   &reverseAmount,
				Transfer: &iter.Transfer().ID,
			})
			if err != nil {
				fmt.Println(err)
			}
			fmt.Printf("%+v\n", rev)
		}

		ref, err := refund.New(&stripe.RefundParams{
			Amount:        stripe.Int64(int64(amt * 100)),
			PaymentIntent: &pi.ID,
		})
		if err != nil {
			return nil, err
		}

		fmt.Printf("%+v\n", ref)
		item.Status = "refunded"
		db.Save(&item)
	}

	return gin.H{"status": "success"}, nil
}

type passitem struct {
	ID          string
	PaymentID   string
	Quantity    uint
	Sku         string
	Name        string
	Description string
	Amount      string `gorm:"type:money"`
}

func (p *passitem) GetName() string   { return p.Name }
func (p *passitem) GetSku() string    { return p.Sku }
func (p *passitem) GetDesc() string   { return p.Description }
func (p *passitem) GetQuantity() uint { return p.Quantity }
func (p *passitem) GetID() string     { return p.PaymentID }

func (h Handler) GetPassItems(config *types.MerchantConfig, db *gorm.DB, id string) ([]types.PassItem, string) {
	var items []passitem

	db.Model(&LineItem{}).
		Where("payment_id = ? AND sku != ''", id).
		Select([]string{"payment_id", "id", "quantity", "sku", "name", "amount",
			`SUBSTRING(name from '\w* Ticket, [^,]*, (.*)') as description`}).
		Scan(&items)

	var name string
	var email string

	db.Model(PaymentIntent{}).
		Where("id = ?", id).
		Select("name, email").
		Row().Scan(&name, &email)

	ret := make([]types.PassItem, len(items))
	for idx, i := range items {
		ret[idx] = &i
	}

	return ret, name
}
