package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
	"github.com/zeroshade/tmsapi/paypal"
	"github.com/zeroshade/tmsapi/stripe"
	"github.com/zeroshade/tmsapi/types"
)

const passHeight = 65
const left = 5
const spaceBetween = 15

var skuRe = regexp.MustCompile(`(\d+)([A-Z]+)(\d{10})\d*`)

func drawPass(f *gofpdf.Fpdf, item types.PassItem, passTitle string, boat *types.Boat, name, tkt, qrname string) {
	fmt.Println(item, passTitle, *boat, name, tkt, qrname)

	var opt gofpdf.ImageOptions
	opt.ImageType = "png"

	_, _, mtop, mbottom := f.GetMargins()
	starty := f.GetY() - mtop + 10
	_, pageh := f.GetPageSize()

	if starty+passHeight > pageh-mbottom {
		f.AddPage()
		starty = f.GetY()
	}

	colorBytes, _ := hex.DecodeString(boat.Color)
	red := int(colorBytes[0])
	green := int(colorBytes[1])
	blue := int(colorBytes[2])

	f.SetFillColor(red, green, blue)
	f.SetDrawColor(red, green, blue)
	f.Rect(left, starty, 205, passHeight, "D")
	f.SetX(left)
	f.SetFont("Courier", "B", 18)
	f.SetTextColor(255, 255, 255)
	f.CellFormat(205, 7, passTitle, "B", 1, "C", true, 0, "")

	f.SetTextColor(0, 0, 0)
	f.SetFont("Courier", "B", 16)
	f.SetX(left)
	f.Cell(40, 7, "Boarding Pass")
	f.SetX(-53)
	f.Cell(40, 7, tkt+" Ticket")

	f.Ln(-1)
	f.SetFont("Courier", "B", 16)
	f.SetTextColor(red, green, blue)
	f.SetX(left)
	f.Cell(40, 7, boat.Name)
	f.SetTextColor(0, 0, 0)

	f.Ln(-1)
	f.SetFont("Courier", "B", 14)
	f.SetX(left)
	f.Cell(40, 7, "Trip:")
	f.SetFont("Courier", "", 14)
	f.Cell(100, 7, item.GetDesc())

	f.Ln(15)
	f.SetX(left)
	f.SetFont("Courier", "B", 14)
	f.Cell(40, 7, "Purchased By:")
	f.SetFont("Courier", "", 14)
	f.Cell(50, 7, name)

	f.Ln(20)
	f.SetFont("Courier", "I", 8)
	f.Cell(40, 8, qrname)

	f.ImageOptions(qrname, 205-40, starty+18, 40, 0, false, opt, 0, "")

	f.SetXY(0, starty+passHeight+spaceBetween)
}

func generatePdf(db *gorm.DB, items []types.PassItem, passTitle, name, email string, w io.Writer) {
	var opt gofpdf.ImageOptions
	opt.ImageType = "png"

	pdf := gofpdf.New("P", "mm", "Letter", ".")
	pdf.SetTitle("Boarding Passes", false)

	for _, i := range items {
		skuPieces := skuRe.FindAllStringSubmatch(i.GetSku(), -1)

		pid := skuPieces[0][1]

		var prod types.Product
		db.Find(&prod, "id = ?", pid)
		var boat types.Boat
		db.Find(&boat, "id = ?", prod.BoatID)

		prod.Boat = &boat
		tkt := strings.Title(strings.ToLower(skuPieces[0][2]))

		pdf.AddPage()
		for n := uint(1); n <= i.GetQuantity(); n++ {
			qrname := fmt.Sprintf("%s-%s-%d", i.GetID(), i.GetSku(), n)
			data, _ := qrcode.Encode(qrname, qrcode.High, 50)
			pdf.RegisterImageOptionsReader(qrname, opt, bytes.NewReader(data))
			drawPass(pdf, i, passTitle, prod.Boat, name, tkt, qrname)
		}
	}
	pdf.Output(w)
}

func GetBoardingPasses(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var config types.MerchantConfig
		db.Find(&config, "id = ?", c.Param("merchantid"))

		var handler PaymentHandler

		switch config.PaymentType {
		case "stripe":
			handler = &stripe.Handler{}
		case "paypal":
			handler = &paypal.Handler{}
		}

		items, name, email := handler.GetPassItems(&config, db, c.Param("checkoutid"))
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", `attachment; filename="boardingpasses_`+c.Param("checkoutid")+`.pdf"`)
		c.Status(http.StatusOK)
		generatePdf(db, items, config.PassTitle, name, email, c.Writer)
	}
}
