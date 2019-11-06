package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
	"github.com/zeroshade/tmsapi/internal"
)

type amount struct {
	Total    string `json:"total" gorm:"type:money"`
	Currency string `json:"currency" gorm:"-"`
}

type link struct {
	Href    string `json:"href"`
	Rel     string `json:"rel"`
	Method  string `json:"method"`
	EncType string `json:"encType"`
}

type CUTime struct {
	UpdateTime time.Time `json:"update_time"`
	CreateTime time.Time `json:"create_time"`
}

type PayerInfo struct {
	ID        string `json:"payer_id" gorm:"primary_key"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Country   string `json:"country_code"`
}

type WebHookEvent struct {
	ID            string      `json:"id" gorm:"primary_key"`
	CreateTime    time.Time   `json:"create_time"`
	UpdatedAt     time.Time   `json:"-"`
	ResourceType  string      `json:"resource_type"`
	EventType     string      `json:"event_type"`
	Summary       string      `json:"summary"`
	Resource      interface{} `gorm:"-"`
	Status        string      `json:"status"`
	Transmissions []struct {
		WebhookURL     string `json:"webhook_url"`
		TransmissionID string `json:"transmission_id"`
		Status         string `json:"status"`
	} `json:"transmissions" gorm:"-"`
	Links        []link         `json:"links" gorm:"-"`
	EventVersion float32        `json:"event_version,string"`
	RawMessage   postgres.Jsonb `json:"-"`
}

func (WebHookEvent) TableName() string {
	return "webhook_logs"
}

func (w *WebHookEvent) UnmarshalJSON(data []byte) error {
	type Alias WebHookEvent
	aux := &struct {
		*Alias
		RawResource *json.RawMessage `json:"resource"`
	}{
		Alias: (*Alias)(w),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	switch aux.ResourceType {
	case "sale":
		aux.Resource = new(Sale)
	case "payment":
		aux.Resource = new(Payment)
	}

	w.RawMessage = postgres.Jsonb{json.RawMessage(data)}
	return json.Unmarshal(*aux.RawResource, aux.Resource)
}

type Item struct {
	Transaction string `json:"-" gorm:"primary_key"`
	Name        string `json:"name"`
	Sku         string `json:"sku" gorm:"primary_key"`
	Price       string `json:"price" gorm:"type:money"`
	Currency    string `json:"currency"`
	Tax         string `json:"tax" gorm:"type:money"`
	Qty         uint32 `json:"quantity"`
}

type Transaction struct {
	PaymentID string `json:"-" gorm:"primary_key"`
	Amount    amount `json:"amount" gorm:"embedded"`
	Payee     struct {
		MerchantID string `json:"merchant_id"`
		Email      string `json:"email"`
	} `json:"payee" gorm:"embedded;embedded_prefix:payee_"`
	Desc     string `json:"description"`
	SoftDesc string `json:"soft_descriptor"`
	ItemList struct {
		Items []Item `json:"items"`
	} `json:"item_list" gorm:"-"`

	RelatedResources []interface{} `gorm:"-"`
	Sales            []*Sale       `json:"-" gorm:"many2many:transaction_related;"`
}

func (t *Transaction) UnmarshalJSON(data []byte) error {
	type Alias Transaction
	aux := &struct {
		*Alias
		Related []map[string]*json.RawMessage `json:"related_resources"`
	}{
		Alias: (*Alias)(t),
	}
	err := json.Unmarshal(data, &aux)
	if err != nil {
		return err
	}

	for _, m := range aux.Related {
		for k, v := range m {
			if k == "sale" {
				s := new(Sale)
				if err = json.Unmarshal(*v, s); err != nil {
					return err
				}

				aux.RelatedResources = append(aux.RelatedResources, s)
			}
		}
	}

	return nil
}

type Payment struct {
	CUTime
	ID           string        `json:"id" gorm:"primary_key"`
	Links        []link        `json:"links"`
	State        string        `json:"state"`
	Transactions []Transaction `json:"transactions"`
	Intent       string        `json:"intent"`
	Payer        struct {
		PaymentMethod string    `json:"payment_method"`
		Status        string    `json:"status"`
		PayerInfoID   string    `json:"-"`
		PayerInfo     PayerInfo `json:"payer_info"`
	} `json:"payer" gorm:"embedded"`
	CartID string `json:"cart"`
}

type Sale struct {
	CUTime
	ID             string `json:"id" gorm:"primary_key"`
	Amount         amount `json:"amount" gorm:"embedded"`
	PaymentMode    string `json:"payment_mode"`
	TransactionFee struct {
		Value    string `json:"value" gorm:"column:transaction_fee;type:money"`
		Currency string `json:"currency" gorm:"-"`
	} `json:"transaction_fee" gorm:"embedded"`
	ParentPayment   string         `json:"parent_payment"`
	SoftDesc        string         `json:"soft_descriptor"`
	ProtectEligible string         `json:"protection_eligibility"`
	Links           []link         `json:"links"`
	State           string         `json:"state"`
	InvoiceNum      string         `json:"invoice_number"`
	RelatedTrans    []*Transaction `json:"-" gorm:"many2many:transaction_related"`
}

// WebhookID is the constant id from PayPal for this webhook
var WebhookID string

func init() {
	WebhookID = os.Getenv("WEBHOOK_ID")
}

func GetItems(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t := Transaction{PaymentID: c.Param("transaction")}
		db.Find(&t)

		var items []Item
		db.Find(&items, "transaction = ?", t.PaymentID)
		c.JSON(http.StatusOK, items)
	}
}

// HandlePaypalWebhook returns a handler function that verifies a paypal webhook
// post request and then processes the event message
func HandlePaypalWebhook(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		paypalClient := internal.NewClient(internal.SANDBOX)
		verified := paypalClient.VerifyWebHookSig(c.Request, WebhookID)

		if !verified {
			log.Println("Didn't Verify")
			c.Status(http.StatusBadRequest)
			return
		}

		defer c.Request.Body.Close()
		body, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
			log.Println(err)
			c.Status(http.StatusBadRequest)
			return
		}

		var we WebHookEvent
		json.Unmarshal(body, &we)

		db.Save(&we)

		p, ok := we.Resource.(*Payment)
		if ok {
			count := 0
			db.Model(&Payment{}).Where("id = ?", p.ID).Count(&count)
			if count <= 0 {
				db.Create(we.Resource)
				c.Status(http.StatusOK)
				return
			}
		}
		db.Save(we.Resource)
		c.Status(http.StatusOK)
	}
}

func (t *Transaction) BeforeSave() error {
	for _, r := range t.RelatedResources {
		switch related := r.(type) {
		case *Sale:
			t.Sales = append(t.Sales, related)
		}
	}
	return nil
}

func (t *Transaction) AfterCreate(tx *gorm.DB) error {
	for idx := range t.ItemList.Items {
		t.ItemList.Items[idx].Transaction = t.PaymentID

		tx.Save(&t.ItemList.Items[idx])
	}
	return nil
}
