package stripe

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/paymentintent"
	"github.com/stripe/stripe-go/v72/refund"
	"github.com/stripe/stripe-go/v72/reversal"
	"github.com/stripe/stripe-go/v72/transfer"
	"github.com/zeroshade/tmsapi/internal"
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
		OrigSku   string    `json:"origSku"`
		OrigProd  string    `json:"origName"`
	}

	var ret []Ret
	db.Table("line_items AS li").
		Joins("LEFT JOIN payment_intents AS pi ON (pi.id = li.payment_id)").
		Joins("LEFT JOIN transfer_reqs AS tr ON (li.id = tr.line_item_id)").
		Joins("LEFT JOIN manual_payer_infos AS mpi ON (li.id = mpi.id)").
		Where("li.acct = ? AND SUBSTRING(coalesce(new_sku, sku) FROM '\\d+[A-Z]+(\\d{10})\\d*') = ?", config.StripeKey, timestamp).
		Select([]string{"li.id", "payment_id", "li.acct", "quantity",
			"coalesce(new_sku, sku) as sku",
			"coalesce(new_name, li.name) AS prod", "mpi.phone",
			"coalesce(pi.name, mpi.name) AS name", "coalesce(pi.email, mpi.email) AS email", "created_at",
			"coalesce(li.status, pi.status) AS status", "sku AS orig_sku", "li.name AS orig_prod"}).
		Scan(&ret)

	piMap := make(map[string]*stripe.PaymentIntent)
	params := &stripe.PaymentIntentParams{}
	params.AddExpand("customer")
	for idx := range ret {
		r := &ret[idx]
		var (
			cus *stripe.Customer
			ok  bool
		)

		if strings.HasPrefix(r.Status, "manual entry") || r.PaymentID == "-" {
			continue
		}

		pi, ok := piMap[r.PaymentID]
		if !ok {
			var err error
			pi, err = paymentintent.Get(r.PaymentID, params)
			if err != nil {
				log.Println("PI:", err)
				continue
			}
			piMap[r.PaymentID] = pi
		}

		if r.CreatedAt.IsZero() {
			r.CreatedAt = time.Unix(pi.Created, 0)
		}

		cus = pi.Customer
		if card, ok := pi.Metadata["giftcard"]; ok && card != "" {
			r.PaymentID = "-"
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

	fromSku := "TO_TIMESTAMP(SUBSTRING(coalesce(new_sku, sku) FROM '\\d[A-Z]+(\\d{10})\\d*')::INTEGER)"

	var out []result
	db.Table("line_items AS li").
		Joins("LEFT JOIN transfer_reqs AS tr ON (li.id = tr.line_item_id)").
		Select(`(regexp_matches(coalesce(new_sku, sku), '^\d+'))[1]::integer as pid, `+fromSku+` as stamp, SUM(quantity) AS qty`).
		Where("acct = ? AND (status = 'succeeded' OR status like 'manual%') AND "+fromSku+" BETWEEN TO_TIMESTAMP(?) AND TO_TIMESTAMP(?)",
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

		feeAcct := config.StripeAcctMap.Map["feeacct"].String

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
			case feeAcct:
				continue
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

	db.Table("line_items AS li").
		Joins("left join transfer_reqs AS tr ON (li.id = tr.line_item_id)").
		Where("payment_id = ? AND coalesce(new_sku, sku) != ''", id).
		Select([]string{"payment_id", "id", "quantity", "coalesce(new_sku, sku) AS sku", "coalesce(new_name, name)", "amount",
			`SUBSTRING(name from '\w* Ticket, [^,]*, (.*)') as description`}).
		Scan(&items)

	var name string
	var email string

	db.Model(PaymentIntent{}).
		Where("id = ?", id).
		Select("name, email").
		Row().Scan(&name, &email)

	ret := make([]types.PassItem, len(items))
	for idx := range items {
		ret[idx] = &items[idx]
	}

	return ret, name
}

func (h Handler) TransferTickets(_ *types.MerchantConfig, db *gorm.DB, data []types.TransferReq) (interface{}, error) {
	for idx := range data {
		type req struct {
			Quantity uint
			Sku      string
		}
		var r []req
		db.Table("line_items AS li").
			Joins("left join transfer_reqs AS tr ON (li.id = tr.line_item_id)").
			Where("li.id = ?", data[idx].LineItemID).
			Select("quantity, coalesce(new_sku, sku) AS sku").Scan(&r)

		re := regexp.MustCompile(`(\d+)[A-Z]+(\d{10})`)
		result := re.FindStringSubmatch(r[0].Sku)
		oldPid, _ := strconv.Atoi(result[1])
		oldTm, _ := strconv.ParseInt(result[2], 10, 64)

		result = re.FindStringSubmatch(data[idx].NewSKU)
		newPid, _ := strconv.Atoi(result[1])
		newTm, _ := strconv.ParseInt(result[2], 10, 64)
		db.Table("manual_overrides").Where("product_id = ? AND time = TO_TIMESTAMP(?::INTEGER)", oldPid, oldTm).
			UpdateColumn("avail", gorm.Expr("avail + ?", r[0].Quantity))

		db.Table("manual_overrides").Where("product_id = ? AND time = TO_TIMESTAMP(?::INTEGER)", newPid, newTm).
			UpdateColumn("avail", gorm.Expr("avail - ?", r[0].Quantity))

		db.Save(&data[idx])
	}
	return nil, nil
}

type ManualPayerInfo struct {
	ID    string `gorm:"primary_key"`
	Name  string
	Phone string
	Email string
}

func (h Handler) ManualEntry(config *types.MerchantConfig, db *gorm.DB, entry types.Manual) (interface{}, error) {
	li := &LineItem{
		ID:        uuid.New().String(),
		PaymentID: "-",
		Acct:      config.StripeKey,
		Quantity:  entry.Quantity,
		Status:    "manual entry - " + entry.EntryType,
		Name:      entry.Desc,
		Sku:       fmt.Sprintf("%d%s%s", entry.ProductID, strings.ToUpper(entry.TicketType), entry.Timestamp),
	}

	db.Create(li)
	db.Create(&ManualPayerInfo{
		ID:    li.ID,
		Name:  entry.Name,
		Phone: entry.Phone,
		Email: entry.Email,
	})

	pid := entry.ProductID
	timestamp := entry.Timestamp

	db.Table("manual_overrides").Where("product_id = ? AND time = TO_TIMESTAMP(?::INTEGER)", pid, timestamp).
		UpdateColumn("avail", gorm.Expr("avail - ?", li.Quantity))

	return nil, nil
}

type TicketRedemption struct {
	GiftCard string `json:"giftcard"`
	Items    []Item `json:"items"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

func (h Handler) RedeemTickets(config *types.MerchantConfig, db *gorm.DB, data json.RawMessage) (interface{}, error) {
	apiKey := os.Getenv("SENDGRID_API_KEY")

	var redeem TicketRedemption
	json.Unmarshal(data, &redeem)

	var gc types.GiftCard
	db.Find(&gc, "id = ?", redeem.GiftCard)
	if gc.Status != "success" {
		return nil, errors.New("Invalid Gift Card")
	}

	if redeem.Name == "" || redeem.Email == "" || redeem.Phone == "" {
		return nil, errors.New("must include necessary info")
	}

	if len(redeem.Items) == 0 {
		return nil, errors.New("cannot redeem for no items")
	}

	var notifyList []notifyItem

	for _, item := range redeem.Items {
		li := &LineItem{
			ID:        uuid.New().String(),
			PaymentID: "-",
			Acct:      config.StripeKey,
			Quantity:  item.Quantity,
			Status:    "success",
			Name:      item.Name,
			Sku:       item.Sku,
			UnitPrice: fmt.Sprintf("%.02f", item.UnitAmount.Value),
			Amount:    fmt.Sprintf("%.02f", float32(item.Quantity)*item.UnitAmount.Value),
		}

		notifyList = append(notifyList, notifyItem{Name: li.Name, Quantity: li.Quantity})

		db.Create(li)
		db.Create(&ManualPayerInfo{
			ID:    li.ID,
			Name:  redeem.Name,
			Phone: redeem.Phone,
			Email: redeem.Email,
		})
	}

	db.Model(&gc).Where("id = ?", gc.ID).Update("status", "used")
	sendNotifyEmail(apiKey, config, &stripe.PaymentIntent{
		Charges: &stripe.ChargeList{
			Data: []*stripe.Charge{
				{
					BillingDetails: &stripe.BillingDetails{
						Name:  redeem.Name,
						Email: redeem.Email,
					},
				},
			},
		},
	}, notifyList)

	if config.SendSMS {
		t := internal.NewTwilio(config.TwilioAcctSID, config.TwilioAcctToken, config.TwilioFromNumber)
		t.Send(config.NotifyNumber, "Tickets Purchased by "+redeem.Name)
	}

	return nil, nil
}
