package paypal

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
	"github.com/zeroshade/tmsapi/types"
)

var timeloc *time.Location

func init() {
	timeloc, _ = time.LoadLocation("America/New_York")
}

type Handler struct{}

func (h Handler) OrdersTimestamp(config *types.MerchantConfig, db *gorm.DB, timestamp string) (interface{}, error) {
	type Ret struct {
		Name        string `json:"name"`
		Description string `json:"desc"`
		Value       string `json:"value"`
		Payer       string `json:"payer"`
		PayerID     string `json:"payerId"`
		Email       string `json:"email"`
		PhoneNumber string `json:"phone"`
		Quantity    uint   `json:"qty"`
		Coid        string `json:"coid"`
		Sku         string `json:"sku"`
		Status      string `json:"status"`
	}

	var sids pq.StringArray
	row := db.Table("sandbox_infos").Select("sandbox_ids").Where("id = ?", config.ID).Row()
	row.Scan(&sids)

	var ret []Ret
	db.Table("purchase_items as pi").
		Joins("LEFT JOIN purchase_units as pu USING(checkout_id)").
		Joins("LEFT JOIN checkout_orders as co ON pi.checkout_id = co.id").
		Joins("LEFT JOIN captures as cap USING(checkout_id)").
		Joins("LEFT JOIN payers as pa ON co.payer_id = pa.id").
		Where("(pu.payee_merchant_id = ? OR pu.payee_merchant_id = ANY (?)) AND SUBSTRING(sku FROM '\\d+[A-Z]+(\\d{10})\\d*') = ?",
			config.ID, sids, timestamp).
		Select("pi.name, co.payer_id, pi.checkout_id as coid, sku, pi.description, pi.value, given_name || ' ' || surname as payer, email, phone_number, quantity, cap.status").
		Scan(&ret)

	return ret, nil
}

func (h Handler) GetSoldTickets(config *types.MerchantConfig, db *gorm.DB, from, to string) (interface{}, error) {
	type result struct {
		Stamp time.Time `json:"stamp"`
		Qty   uint      `json:"qty"`
		Pid   uint      `json:"pid"`
	}

	si := types.SandboxInfo{ID: config.ID}
	db.Find(&si)

	ids := []string{config.ID}
	ids = append(ids, si.SandboxIDs...)

	sub := db.Model(&types.PurchaseItem{}).
		Select([]string{"checkout_id",
			`(regexp_matches(sku, '^\d+'))[1]::integer as pid`,
			"TO_TIMESTAMP(SUBSTRING(sku FROM '\\d[A-Z]+(\\d{10})\\d*')::INTEGER) as tm",
			"SUM(quantity) as q"}).Group("checkout_id, pid, tm").SubQuery()

	var out []result
	db.Table("purchase_units as pu").
		Select("pid, tm as stamp, sum(q) as qty").
		Joins("RIGHT JOIN ? as sub ON pu.checkout_id = sub.checkout_id", sub).
		Joins("LEFT JOIN checkout_orders AS co ON pu.checkout_id = co.id").
		Where("pu.payee_merchant_id IN (?) AND tm BETWEEN TO_TIMESTAMP(?) AND TO_TIMESTAMP(?) AND co.status != 'REFUNDED'",
			ids, from, to).
		Group("pid, tm").Scan(&out)

	for idx, o := range out {
		out[idx].Stamp = o.Stamp.In(timeloc)
	}

	return out, nil
}

func (h Handler) GetPassItems(conf *types.MerchantConfig, db *gorm.DB, id string) ([]types.PassItem, string) {
	var items []types.PurchaseItem
	var name string
	var email string
	var payerId string

	db.Where("checkout_id = ?", id).
		Select([]string{"checkout_id", "sku", "name", "value", "quantity",
			`COALESCE(NULLIF(description, ''), SUBSTRING(name from '\w* Ticket, [^,]*, (.*)')) as description`}).
		Find(&items)

	db.Table("checkout_orders as co").
		Joins("LEFT JOIN payers as p ON co.payer_id = p.id").
		Where("co.id = ?", id).
		Select("given_name || ' ' || surname as name, email, payer_id").
		Row().Scan(&name, &email, &payerId)

	ret := make([]types.PassItem, len(items))
	for idx := range items {
		ret[idx] = &items[idx]
	}
	return ret, name
}
