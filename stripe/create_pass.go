package stripe

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
	"github.com/zeroshade/tmsapi/types"
)

const passHeight = 65
const left = 5
const spaceBetween = 15

var skuRe = regexp.MustCompile(`(\d+)([A-Z]+)(\d{10})\d*`)
var priceRe = regexp.MustCompile(`^\$?(\d+)\.(\d{2})$`)

type pdfitem struct {
	quant uint
	kind  string
	price string
}

type passInfo struct {
	prodname string
	boat     *types.Boat

	trip     time.Time
	duration time.Duration
	tkts     []pdfitem
	total    int
}

func (p *passInfo) String() string {
	var bld strings.Builder
	bld.WriteString(p.prodname)
	bld.WriteString(" - ")
	bld.WriteString(p.boat.Name)
	bld.WriteString(" - ")
	bld.WriteString(p.trip.String())
	bld.WriteString(p.duration.String())
	fmt.Fprint(&bld, p.tkts)
	return bld.String()
}

func newDrawPass(f *gofpdf.Fpdf, conf *types.MerchantConfig, passTitle string, info passInfo, name, email, orderid string) {
	var opt gofpdf.ImageOptions
	opt.ImageType = "png"

	_, _, mtop, mbottom := f.GetMargins()
	starty := f.GetY() - mtop + 10
	_, pageh := f.GetPageSize()

	if starty+passHeight > pageh-mbottom {
		f.AddPage()
		starty = f.GetY()
	}

	f.SetFillColor(0, 0, 0x88)
	f.SetDrawColor(0, 0, 0x88)
	f.Rect(left, starty, 205, 43, "F")
	f.Ln(5)
	if len(conf.LogoBytes) > 0 {
		f.ImageOptions("logo", left+45, starty, 205-left-90, 28, true, opt, 0, "")
	} else {
		f.SetFont("Courier", "B", 20)
		f.SetXY(left, starty)
		f.SetTextColor(0xFF, 0xFF, 0xFF)
		f.WriteAligned(205-left, 20, info.boat.Name, "C")
	}

	f.SetX(left + 5)
	f.SetFont("Courier", "B", 10)
	f.SetTextColor(0, 0xFF, 0xFF)
	f.WriteLinkString(10, conf.EmailFrom, "mailto:"+conf.EmailFrom)
	f.SetX(205 - 32)
	f.WriteLinkString(10, conf.NotifyNumber, "tel:"+conf.NotifyNumber)

	// f.Rect(left, f.GetY(), 205, passHeight, "D")
	f.SetXY(left, f.GetY()+10)
	f.SetTextColor(0, 0, 0)
	f.SetFontSize(14)
	f.WriteAligned(205-left, 8, info.prodname, "C")

	f.Ln(-1)
	f.SetFontSize(10)
	f.SetFontStyle("")
	f.WriteAligned(205-left, 6, "Bring this with you when you come to the boat", "C")

	f.Ln(-1)
	f.SetLineWidth(0.75)
	f.Line(left, f.GetY(), 205+left, f.GetY())

	f.Ln(3)
	f.SetFontSize(14)
	f.SetFontStyle("B")
	f.WriteAligned(205-left, 6, "Ticket Information", "C")

	f.Ln(-1)
	f.Ln(2)
	f.SetLineWidth(0.75)
	f.Line(left, f.GetY(), 205+left, f.GetY())

	f.Ln(2)
	f.SetFontSize(12)
	f.WriteAligned(205-left-15, 5, "Order #: "+orderid, "C")

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "Tickets:", "C")
	f.SetFontStyle("")
	for i, t := range info.tkts {
		next := " "
		if i != 0 {
			next = ", "
		}
		f.WriteAligned(205-left-15, 5, fmt.Sprintf("%s%s: %d", next, t.kind, t.quant), "C")
	}

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "Total Cost (including fees): ", "C")
	f.SetFontStyle("")
	f.WriteAligned(205-left-15, 5, fmt.Sprintf("$%.02f", float64(info.total)/100), "C")

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "Purchased By: ", "C")
	f.SetFontStyle("")
	f.WriteAligned(205-left-15, 5, name, "C")

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "Email: ", "C")
	f.SetFontStyle("")
	f.WriteAligned(205-left-15, 5, email, "C")

	f.Ln(-1)
	f.Ln(1)
	f.SetLineWidth(0.75)
	f.Line(left, f.GetY(), 205+left, f.GetY())

	f.Ln(3)
	f.SetFontSize(14)
	f.SetFontStyle("B")
	f.WriteAligned(205-left, 6, "Boat Information", "C")

	f.Ln(-1)
	f.Ln(2)
	f.SetLineWidth(0.75)
	f.Line(left, f.GetY(), 205+left, f.GetY())

	f.Ln(2)
	f.SetFontSize(12)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-30, 5, "Captain contact number:", "C")
	f.SetFontStyle("")
	f.WriteLinkString(5, conf.NotifyNumber, "tel:"+conf.NotifyNumber)

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "Date Of Trip: ", "C")
	f.SetFontStyle("")
	f.WriteAligned(205-left-15, 5, info.trip.Format("Jan _2, 2006"), "C")

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "Departing Time: ", "C")
	f.SetFontStyle("")
	f.WriteAligned(205-left-15, 5, info.trip.Format("3:04 PM"), "C")

	f.Ln(-1)
	f.SetFontStyle("B")
	f.WriteAligned(205-left-15, 5, "*Returning Time: ", "C")
	f.SetFontStyle("")
	f.WriteAligned(205-left-15, 5, info.trip.Add(info.duration).Format("3:04 PM"), "C")

	f.Ln(-1)
	f.Ln(1)
	f.SetFontSize(8)
	f.SetFontStyle("B")
	f.WriteAligned(205-left, 5, "*Return times may vary, either earlier or later, depending on how the fish are running!", "C")

	f.Ln(-1)
	f.Ln(2)

	qrname := fmt.Sprintf("%s-%d", orderid, info.trip.UTC().Unix())
	data, _ := qrcode.Encode(qrname, qrcode.High, 150)
	f.RegisterImageOptionsReader(qrname, opt, bytes.NewReader(data))
	f.ImageOptions(qrname, left+85, f.GetY(), 0, 0, true, opt, 0, "")

	f.Rect(left, starty, 205, f.GetY()-starty, "D")
}

func generatePdf(db *gorm.DB, config *types.MerchantConfig, items []types.PassItem, passTitle, name, email, orderid string, w io.Writer) {
	var opt gofpdf.ImageOptions
	opt.ImageType = "png"

	pdf := gofpdf.New("P", "mm", "Letter", ".")
	pdf.SetTitle("Boarding Passes", true)

	if len(config.LogoBytes) > 0 {
		pdf.RegisterImageOptionsReader("logo", opt, bytes.NewReader(config.LogoBytes))
	}

	tripPasses := make(map[int]passInfo)

	for _, i := range items {
		skuPieces := skuRe.FindAllStringSubmatch(i.GetSku(), -1)
		pid := skuPieces[0][1]

		var prod types.Product
		db.Preload("Schedules").Preload("Schedules.TimeArray").Find(&prod, "id = ?", pid)
		nsec, _ := strconv.Atoi(skuPieces[0][3])

		info, ok := tripPasses[nsec]
		if !ok {

			trip := time.Unix(int64(nsec), 0).In(timeloc)
			duration := findDuration(&prod, trip)

			var boat types.Boat
			db.Find(&boat, "id = ?", prod.BoatID)

			prod.Boat = &boat

			info = passInfo{
				prodname: prod.Name,
				boat:     &boat,
				trip:     trip,
				duration: duration,
			}
		}

		pricePieces := priceRe.FindAllStringSubmatch(i.GetAmount(), -1)
		dollars, _ := strconv.Atoi(pricePieces[0][1])
		cents, _ := strconv.Atoi(pricePieces[0][2])
		info.total += dollars*100 + cents

		tkt := strings.Title(strings.ToLower(skuPieces[0][2]))
		info.tkts = append(info.tkts, pdfitem{quant: i.GetQuantity(), kind: tkt, price: i.GetAmount()})
		tripPasses[nsec] = info
	}

	for _, v := range tripPasses {
		pdf.AddPage()
		v.total += int(float64(v.total) * config.FeePercent)
		newDrawPass(pdf, config, passTitle, v, name, email, orderid)
	}

	pdf.Output(w)
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
