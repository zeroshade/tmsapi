package types

import (
	"github.com/lib/pq"
	"github.com/lib/pq/hstore"
)

type SandboxInfo struct {
	ID         string         `gorm:"primary_key"`
	SandboxIDs pq.StringArray `gorm:"type:text[]"`
}

type MerchantConfig struct {
	ID                 string        `json:"-" gorm:"primary_key"`
	PassTitle          string        `json:"passTitle"`
	NotifyNumber       string        `json:"notifyNumber"`
	EmailFrom          string        `json:"emailFrom"`
	EmailName          string        `json:"emailName"`
	EmailContent       string        `json:"emailContent"`
	SendSMS            bool          `json:"sendSMS" gorm:"default:false"`
	TermsConds         string        `json:"terms"`
	SandboxID          string        `json:"-"`
	TwilioAcctSID      string        `json:"-"`
	TwilioAcctToken    string        `json:"-"`
	TwilioFromNumber   string        `json:"-"`
	StripeKey          string        `json:"-"`
	StripeSecondary    string        `json:"-"`
	StripeAcctMap      hstore.Hstore `json:"-"`
	PaymentType        string        `json:"-"`
	FeePercent         float64       `json:"-"`
	FuelSurcharge      float64       `json:"-"`
	LogoBytes          []byte        `json:"-"`
	StripeManagedProds bool          `json:"-"`
}
