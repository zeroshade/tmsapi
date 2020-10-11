package types

type PassItem interface {
	GetName() string
	GetSku() string
	GetDesc() string
	GetQuantity() uint
	GetID() string
}
