package paypal

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
	"github.com/zeroshade/tmsapi/types"
)

var timeloc *time.Location

func init() {
	timeloc, _ = time.LoadLocation("America/New_York")
}

type Handler struct{}

func (h Handler) RedeemTickets(config *types.MerchantConfig, db *gorm.DB, data json.RawMessage) (interface{}, error) {
	return nil, errors.New("Not Implemented")
}

func (h Handler) RefundTickets(*types.MerchantConfig, *gorm.DB, json.RawMessage) (interface{}, error) {
	return nil, errors.New("Not implemented")
}

func (h Handler) TransferTickets(conig *types.MerchantConfig, db *gorm.DB, data []types.TransferReq) (interface{}, error) {
	for idx := range data {
		type req struct {
			Quantity uint
			Sku      string
		}
		var r []req
		db.Debug().Table("purchase_items AS pi").
			Joins("left join transfer_reqs AS tr ON (pi.checkout_id = tr.line_item_id AND pi.sku = tr.old_sku)").
			Where("pi.checkout_id = ?", data[idx].LineItemID).
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
		OrigSku     string `json:"origSku"`
		OrigProd    string `json:"origName"`
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
		Joins("LEFT JOIN transfer_reqs AS tr ON (pi.checkout_id = tr.line_item_id AND pi.sku = tr.old_sku)").
		Where("(pu.payee_merchant_id = ? OR pu.payee_merchant_id = ANY (?)) AND SUBSTRING(COALESCE(new_sku, sku) FROM '\\d+[A-Z]+(\\d{10})\\d*') = ?",
			config.ID, sids, timestamp).
		Select("COALESCE(new_name, pi.name) as name, co.payer_id, pi.checkout_id as coid, COALESCE(new_sku, sku) AS sku, pi.description, pi.value, given_name || ' ' || surname as payer, email, phone_number, quantity, COALESCE(cap.status, co.status) AS status").
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
		Joins("LEFT JOIN transfer_reqs AS tr ON (checkout_id = tr.line_item_id AND sku = tr.old_sku)").
		Select([]string{"checkout_id",
			`(regexp_matches(COALESCE(new_sku, sku), '^\d+'))[1]::integer as pid`,
			"TO_TIMESTAMP(SUBSTRING(COALESCE(new_sku, sku) FROM '\\d[A-Z]+(\\d{10})\\d*')::INTEGER) as tm",
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

func (h Handler) GetPassItems(conf *types.MerchantConfig, db *gorm.DB, id string) ([]types.PassItem, string, string) {
	var items []types.PurchaseItem
	var name string
	var email string
	var payerId string

	db.Where("checkout_id = ?", id).
		Joins("left join transfer_reqs AS tr ON (checkout_id = tr.line_item_id AND sku = tr.old_sku)").
		Select([]string{"checkout_id", "COALESCE(new_sku, sku) AS sku", "COALESCE(new_name, name) AS name", "value", "quantity",
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
	return ret, name, email
}

func (h Handler) ManualEntry(config *types.MerchantConfig, db *gorm.DB, entry types.Manual) (interface{}, error) {
	coid := uuid.New().String()
	sku := fmt.Sprintf("%d%s%s", entry.ProductID, strings.ToUpper(entry.TicketType), entry.Timestamp)
	co := &types.CheckoutOrder{
		ID:     coid,
		Status: entry.EntryType,
		PurchaseUnits: []types.PurchaseUnit{
			{
				CheckoutID: coid,
				Items: []types.PurchaseItem{{
					CheckoutID: coid,
					Sku:        sku,
					Name:       entry.Desc,
					Quantity:   uint(entry.Quantity),
				}},
			},
		},
		Payer: &types.Payer{
			ID: uuid.New().String(),
		},
	}

	co.Payer.Email = entry.Email
	co.Payer.Name.GivenName = entry.Name
	co.Payer.Phone.PhoneNumber.NationalNumber = entry.Phone
	co.PurchaseUnits[0].Payee.MerchantID = config.ID
	co.PurchaseUnits[0].Payee.Email = config.EmailFrom

	db.Save(&co)
	return nil, nil
}
