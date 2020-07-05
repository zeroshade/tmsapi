package types

import (
	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
)

type LogAction struct {
	gorm.Model
	MerchantID string         `gorm:"index" json:"-"`
	UserID     string         `json:"userId"`
	Method     string         `json:"method"`
	Url        string         `json:"path"`
	Payload    postgres.Jsonb `json:"message"`
}
