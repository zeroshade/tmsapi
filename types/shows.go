package types

import (
	"time"
)

type Show struct {
	MerchantID string     `json:"-"`
	ID         int        `json:"id" gorm:"primary_key;auto_increment;"`
	CreatedAt  time.Time  `json:"-"`
	UpdatedAt  time.Time  `json:"-"`
	DeletedAt  *time.Time `json:"-"`
	Name       string     `json:"name"`
	Publish    bool       `json:"publish"`
	Desc       string     `json:"desc"`
	Price      string     `json:"price"`
	Dates      string     `json:"dates" gorm:"type:daterange"`
	Logo       []byte     `json:"logo"`
}
