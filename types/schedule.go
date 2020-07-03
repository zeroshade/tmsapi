package types

import (
	"encoding/json"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

var loc *time.Location

func init() {
	loc, _ = time.LoadLocation("America/New_York")
}

// ScheduleTime represents a specific trip time for the schedule
type ScheduleTime struct {
	ID         uint   `json:"id"`
	ScheduleID uint   `json:"-"`
	StartTime  string `json:"startTime"`
	EndTime    string `json:"endTime"`
	Price      string `json:"price"`
}

// Schedule represents a full schedule that a Product can have multiple of
type Schedule struct {
	ProductID    uint           `json:"-"`
	ID           uint           `json:"id" gorm:"primary_key"`
	TicketsAvail uint           `json:"ticketsAvail"`
	Start        time.Time      `json:"-"`
	End          time.Time      `json:"-"`
	TimeArray    []ScheduleTime `json:"timeArray"`
	Days         pq.Int64Array  `json:"selectedDays" gorm:"type:integer[]"`
	NotAvail     pq.StringArray `json:"notAvailArray,nilasempty" gorm:"type:text[]"`
}

func (s *Schedule) AfterUpdate(tx *gorm.DB) (err error) {
	ids := make([]uint, 0, len(s.TimeArray))
	for _, t := range s.TimeArray {
		ids = append(ids, t.ID)
	}

	// clear out old schedules
	tx.Where("schedule_id = ?", s.ID).Not("id", ids).Delete(ScheduleTime{})
	return
}

func (s *Schedule) UnmarshalJSON(data []byte) (err error) {
	type Alias Schedule
	aux := &struct {
		*Alias
		StartDay string `json:"start"`
		EndDay   string `json:"end"`
	}{
		Alias: (*Alias)(s),
	}

	if err = json.Unmarshal(data, &aux); err != nil {
		return
	}

	if s.Start, err = time.ParseInLocation("2006-01-02", aux.StartDay, loc); err != nil {
		return
	}
	if s.End, err = time.ParseInLocation("2006-01-02", aux.EndDay, loc); err != nil {
		return
	}

	return
}

// MarshalJSON handles the proper date formatting for schedules
func (s *Schedule) MarshalJSON() ([]byte, error) {
	type Alias Schedule
	if s.NotAvail == nil {
		s.NotAvail = make(pq.StringArray, 0)
	}
	return json.Marshal(&struct {
		*Alias
		StartDay string `json:"start"`
		EndDay   string `json:"end"`
	}{
		Alias:    (*Alias)(s),
		StartDay: s.Start.Format("2006-01-02"),
		EndDay:   s.End.Format("2006-01-02"),
	})
}
