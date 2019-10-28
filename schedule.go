package main

import (
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

type ScheduleTime struct {
	ID         uint   `json:"id"`
	ScheduleID uint   `json:"-"`
	Time       string `json:"time"`
	Price      string `json:"price"`
}

type NotAvail struct {
	ID         uint   `json:"id"`
	ScheduleID uint   `json:"-"`
	Day        string `json:"day"`
}

type Schedule struct {
	ProductID    uint           `json:"-"`
	ID           uint           `json:"id" gorm:"primary_key"`
	TicketsAvail uint           `json:"ticketsAvail"`
	Start        time.Time      `json:"-" gorm:"type:date"`
	End          time.Time      `json:"-" gorm:"type:date"`
	TimeArray    []ScheduleTime `json:"timeArray"`
	Days         pq.Int64Array  `json:"selectedDays" gorm:"type:integer[]"`
	NotAvail     []NotAvail     `json:"notAvailArray"`
}

func (s *Schedule) MarshalJSON() ([]byte, error) {
	type Alias Schedule
	return json.Marshal(&struct {
		*Alias
		StartDate string `json:"start"`
		EndDate   string `json:"end"`
	}{
		Alias:     (*Alias)(s),
		StartDate: s.Start.Format("2006-01-02"),
		EndDate:   s.End.Format("2006-01-02"),
	})
}

func (s *Schedule) UnmarshalJSON(data []byte) error {
	type Alias Schedule
	aux := &struct {
		*Alias
		StartDate string `json:"start"`
		EndDate   string `json:"end"`
	}{
		Alias: (*Alias)(s),
	}
	err := json.Unmarshal(data, &aux)
	if err != nil {
		return err
	}

	s.Start, err = time.Parse("2006-01-02", aux.StartDate)
	s.End, err = time.Parse("2006-01-02", aux.EndDate)
	return err
}
