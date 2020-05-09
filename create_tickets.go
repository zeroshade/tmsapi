package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
)

const passHeight = 65
const left = 5
const spaceBetween = 15

func drawPass(f *gofpdf.Fpdf, item *PurchaseItem, passTitle, name, qrname string) {
	var opt gofpdf.ImageOptions
	opt.ImageType = "png"

	_, _, mtop, mbottom := f.GetMargins()
	starty := f.GetY() - mtop + 10
	_, pageh := f.GetPageSize()

	if starty+passHeight > pageh-mbottom {
		f.AddPage()
		starty = f.GetY()
	}

	f.SetFillColor(59, 0, 220)
	f.Rect(left, starty, 205, passHeight, "D")
	f.SetX(left)
	f.SetFont("Courier", "B", 18)
	f.SetTextColor(255, 255, 255)
	f.CellFormat(205, 7, passTitle, "B", 1, "C", true, 0, "")

	f.SetTextColor(0, 0, 0)
	f.SetFont("Courier", "", 18)
	f.SetX(left)
	f.Cell(40, 7, "Boarding Pass")
	f.SetX(-55)
	f.Cell(40, 7, item.Name)

	f.Ln(-1)
	f.SetX(left)
	x, y := f.GetXY()
	f.Line(x, y, 210, y)

	f.SetFont("Arial", "B", 14)
	f.Cell(40, 10, "Trip:")
	f.SetFont("Arial", "", 14)
	f.Cell(100, 10, item.Description)

	f.Ln(-1)
	f.SetX(left)
	f.SetFont("Arial", "B", 14)
	f.Cell(40, 8, "Purchased By:")
	f.SetFont("Arial", "", 14)
	f.Cell(50, 8, name)

	f.Ln(20)
	f.SetFont("Courier", "I", 8)
	f.Cell(40, 8, qrname)

	f.ImageOptions(qrname, 205-40, starty+18, 40, 0, false, opt, 0, "")

	f.SetXY(0, starty+passHeight+spaceBetween)
}

func generatePdf(items []PurchaseItem, passTitle, name string, w io.Writer) {
	var opt gofpdf.ImageOptions
	opt.ImageType = "png"

	pdf := gofpdf.New("P", "mm", "Letter", ".")
	pdf.SetTitle("Boarding Passes", false)
	for _, i := range items {
		pdf.AddPage()
		for n := uint(1); n <= i.Quantity; n++ {
			qrname := fmt.Sprintf("%s-%s-%d", i.CheckoutID, i.Sku, n)
			data, _ := qrcode.Encode(qrname, qrcode.High, 50)
			pdf.RegisterImageOptionsReader(qrname, opt, bytes.NewReader(data))
			drawPass(pdf, &i, passTitle, name, qrname)
		}
	}
	pdf.Output(w)
}

func GetBoardingPasses(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config MerchantConfig
		db.Find(&config, "id = ?", c.Param("merchantid"))

		var items []PurchaseItem
		var name string
		var email string
		var payerId string

		db.Where("checkout_id = ?", c.Param("checkoutid")).Find(&items)

		db.Table("checkout_orders as co").
			Joins("LEFT JOIN payers as p ON co.payer_id = p.id").
			Where("co.id = ?", c.Param("checkoutid")).
			Select("given_name || ' ' || surname as name, email, payer_id").
			Row().Scan(&name, &email, &payerId)

		// c.JSON(http.StatusOK, gin.H{"items": items, "name": name, "email": email, "payer": payerId})
		c.Status(http.StatusOK)
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", `attachment; filename="boardingpasses_`+c.Param("checkoutid")+`.pdf"`)
		generatePdf(items, config.PassTitle, name, c.Writer)
	}
}
