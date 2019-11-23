package main

import (
	"encoding/json"

	"github.com/lib/pq"
)

// ScheduleTime represents a specific trip time for the schedule
type ScheduleTime struct {
	ID         uint   `json:"id"`
	ScheduleID uint   `json:"-"`
	Time       string `json:"time"`
	Price      string `json:"price"`
}

// Schedule represents a full schedule that a Product can have multiple of
type Schedule struct {
	ProductID    uint           `json:"-"`
	ID           uint           `json:"id" gorm:"primary_key"`
	TicketsAvail uint           `json:"ticketsAvail"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	TimeArray    []ScheduleTime `json:"timeArray"`
	Days         pq.Int64Array  `json:"selectedDays" gorm:"type:integer[]"`
	NotAvail     pq.StringArray `json:"notAvailArray,nilasempty" gorm:"type:text[]"`
}

// MarshalJSON handles the proper date formatting for schedules
func (s *Schedule) MarshalJSON() ([]byte, error) {
	type Alias Schedule
	if s.NotAvail == nil {
		s.NotAvail = make(pq.StringArray, 0)
	}
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}
