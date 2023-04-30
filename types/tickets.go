package types

type PassItem interface {
	GetName() string
	GetSku() string
	GetDesc() string
	GetQuantity() uint
	GetID() string
	GetAmount() string
}

type TransferReq struct {
	LineItemID string `json:"id" gorm:"primary_key"`
	NewSKU     string `json:"newsku" gorm:"primary_key"`
	NewName    string `json:"newname"`
	OldSku     string `json:"oldsku"`
}

type GiftCard struct {
	ID        string  `json:"id" gorm:"primary_key"`
	Initial   string  `json:"initial" gorm:"type:money"`
	Balance   float64 `json:"balance"`
	PaymentID string  `json:"-"`
	Status    string  `json:"-"`
}

type Manual struct {
	ProductID  int    `json:"productId"`
	Timestamp  string `json:"timestamp"`
	EntryType  string `json:"entry"`
	TicketType string `json:"ticket"`
	Quantity   int    `json:"quantity"`
	Desc       string `json:"desc"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
}
