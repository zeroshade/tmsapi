package types

import "time"

type Boat struct {
	ID         int    `json:"id" gorm:"primary_key;auto_increment;"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	MerchantID string `json:"-" gorm:"type:varchar;not null;primary_key;"`
}

// Product represents a specific Type of tickets sold
type Product struct {
	ID          uint       `json:"id" gorm:"primary_key"`
	MerchantID  string     `json:"-" gorm:"type:varchar;not null;primary_key;"`
	CreatedAt   time.Time  `json:"-"`
	UpdatedAt   time.Time  `json:"-"`
	DeletedAt   *time.Time `json:"-"`
	Name        string     `json:"name"`
	Desc        string     `json:"desc"`
	Color       string     `json:"color"`
	Publish     bool       `json:"publish"`
	ShowTickets bool       `json:"showTickets"`
	Schedules   []Schedule `json:"schedList"`
	Fish        string     `json:"fish"`
	Boat        *Boat      `json:"-"`
	BoatID      uint       `json:"boatId" gorm:"default:1"`
}
