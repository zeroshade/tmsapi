package stripe

import (
	"fmt"
	"os"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
	"github.com/zeroshade/tmsapi/types"
)

func ExternalGeneratePDF(db *gorm.DB, config *types.MerchantConfig, payid, name, email string) {
	items, _, _ := (Handler{}).GetPassItems(config, db, payid)
	f, _ := os.Create("order_tmp.pdf")
	defer f.Close()
	internal.GeneratePdf(db, items, "Boarding Passes", name, email, f)
}

func findDuration(prod *types.Product, trip time.Time) time.Duration {
	startTime := trip.Format("15:04")

	truncatedTrip := trip.Truncate(time.Hour * 24)
	fmt.Println(truncatedTrip.String())
	for _, s := range prod.Schedules {
		if truncatedTrip.Before(s.Start.In(timeloc)) || truncatedTrip.After(s.End.In(timeloc)) {
			continue
		}

		fmt.Println(trip.Weekday())

		found := false
		for _, d := range s.Days {
			if time.Weekday(d) == trip.Weekday() {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		fmt.Println(found)

		for _, t := range s.TimeArray {
			if t.StartTime == startTime {
				yy, mm, dd := trip.Date()
				end, _ := time.Parse("15:04", t.EndTime)
				hh, min, _ := end.Clock()
				return time.Date(yy, mm, dd, hh, min, 0, 0, trip.Location()).Sub(trip)
			}
		}
	}

	return 0
}
