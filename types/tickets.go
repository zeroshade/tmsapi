package types

type PassItem interface {
	GetName() string
	GetSku() string
	GetDesc() string
	GetQuantity() uint
	GetID() string
}

type TransferReq struct {
	LineItemID string `json:"id" gorm:"primary_key"`
	NewSKU     string `json:"newsku"`
	NewName    string `json:"newname"`
}

type GiftCard struct {
	ID        string  `json:"id" gorm:"primary_key"`
	Initial   string  `json:"initial" gorm:"type:money"`
	Balance   float64 `json:"balance"`
	PaymentID string  `json:"-"`
	Status    string  `json:"-"`
}
