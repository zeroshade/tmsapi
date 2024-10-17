package types

import (
	"strings"
	"time"
)

type Show struct {
	MerchantID       string     `json:"-"`
	ID               int        `json:"id" gorm:"primary_key;auto_increment;"`
	CreatedAt        time.Time  `json:"-"`
	UpdatedAt        time.Time  `json:"-"`
	DeletedAt        *time.Time `json:"-"`
	Name             string     `json:"name"`
	Publish          bool       `json:"publish"`
	Desc             string     `json:"desc"`
	Price            string     `json:"price"`
	Dates            string     `json:"dates" gorm:"type:daterange"`
	Logo             string     `json:"logoData"`
	TicketsAvailable int        `json:"ticketsAvailable"`
}

func (s *Show) GetDates() (start, end time.Time, err error) {
	var prefix, suffix bool
	if s.Dates[0] == '(' {
		prefix = true
	}
	if s.Dates[len(s.Dates)-1] == ')' {
		suffix = true
	}

	d := strings.Split(s.Dates[1:len(s.Dates)-1], ",")
	start, err = time.Parse("2006-01-02", d[0])
	if err != nil {
		return
	}
	end, err = time.Parse("2006-01-02", d[1])
	if err != nil {
		return
	}

	if prefix {
		start = start.AddDate(0, 0, 1)
	}
	if suffix {
		end = end.AddDate(0, 0, -1)
	}
	return
}

type TicketUsage struct {
	TicketID string `json:"ticket_id" gorm:"primary_key;type:varchar"`
	Used     bool   `json:"used"`
}
