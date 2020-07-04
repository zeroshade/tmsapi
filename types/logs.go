package types

import (
	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
)

type LogAction struct {
	gorm.Model
	MerchantID string `gorm:"index"`
	UserID     string
	Method     string
	Url        string
	Payload    postgres.Jsonb
}
