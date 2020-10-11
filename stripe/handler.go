package stripe

import (
	"time"

	"github.com/jinzhu/gorm"
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
		CreatedAt time.Time `json:"created"`
		Sku       string    `json:"sku"`
		Status    string    `json:"status"`
	}

	var ret []Ret
	db.Table("line_items AS li").
		Joins("LEFT JOIN payment_intents AS pi ON (pi.id = li.payment_id)").
		Where("li.acct = ? AND SUBSTRING(li.sku FROM '\\d+[A-Z]+(\\d{10})\\d*') = ?", config.StripeKey, timestamp).
		Select("li.id, payment_id, li.acct, quantity, sku, li.name AS prod, pi.name, pi.email, created_at, status").
		Scan(&ret)

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
