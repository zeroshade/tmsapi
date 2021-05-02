package main

import (
	"bytes"
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/zeroshade/tmsapi/stripe"
	"github.com/zeroshade/tmsapi/types"

	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

var loc *time.Location

func init() {
	var err error
	loc, err = time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
}

func logActionMiddle(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userid, ok := c.Get("user_id")
		if !ok {
			return
		}

		var data []byte
		if c.Request.Body != nil {
			data, _ = ioutil.ReadAll(c.Request.Body)
			c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(data))
		}

		l := types.LogAction{
			MerchantID: c.Param("merchantid"),
			UserID:     userid.(string),
			Url:        c.Request.URL.Path,
			Method:     c.Request.Method,
			Payload:    postgres.Jsonb{data},
		}

		db.Create(&l)
	}
}

func getLogActions(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var logs []types.LogAction
		db.Order("created_at DESC").Find(&logs, "merchant_id = ?", c.Param("merchantid"))

		c.JSON(http.StatusOK, logs)
	}
}

func getStripeAcct(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var conf types.MerchantConfig
		db.Find(&conf, "id = ?", c.Param("merchantid"))

		c.Set("stripe_acct", conf.StripeKey)
		c.Set("fee_pct", conf.FeePercent)
		c.Next()
	}
}

func main() {
	URI := os.Getenv("DATABASE_URL")
	if URI == "" {
		log.Fatal("must set $DATABASE_URL")
	}

	db, err := gorm.Open("postgres", URI+"?timezone=America/New_York")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.AutoMigrate(&types.Product{}, &types.Schedule{}, &types.ScheduleTime{}, &TicketCategory{}, &Report{},
		&types.Transaction{}, &types.Payment{}, &types.Sale{}, &types.PayerInfo{}, &types.WebHookEvent{}, &types.Item{}, &types.SandboxInfo{},
		&types.CheckoutOrder{}, &types.Payer{}, &types.PurchaseItem{}, &types.PurchaseUnit{}, &types.Capture{}, &types.MerchantConfig{},
		&ManualOverride{}, &types.Refund{}, &types.Boat{}, &types.LogAction{}, &stripe.PaymentIntent{}, &stripe.LineItem{}, &types.TransferReq{},
		&types.GiftCard{}, &stripe.ManualPayerInfo{})
	db.Model(&types.Schedule{}).Association("TimeArray")
	db.Model(&types.Schedule{}).Association("NotAvail")
	db.Model(&types.Payment{}).Association("Payer.PayerInfo")
	db.Model(&types.Item{}).AddForeignKey("transaction", "transactions(payment_id)", "CASCADE", "RESTRICT")
	db.Model(&types.Transaction{}).AddForeignKey("payment_id", "payments(id)", "CASCADE", "RESTRICT")
	db.Table("transaction_related").AddForeignKey("transaction_payment_id", "payments(id)", "CASCADE", "RESTRICT")
	db.Table("transaction_related").AddForeignKey("sale_id", "sales(id)", "CASCADE", "RESTRICT")
	db.Model(&types.PurchaseUnit{}).AddForeignKey("checkout_id", "checkout_orders(id)", "CASCADE", "RESTRICT")
	db.Model(&types.PurchaseItem{}).AddForeignKey("checkout_id", "checkout_orders(id)", "CASCADE", "RESTRICT")

	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS hstore").Error; err != nil {
		log.Fatal(err)
	}

	// db.Exec("SET TIME ZONE 'America/New_York'")

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("must set $PORT")
	}

	config := cors.DefaultConfig()
	config.AllowHeaders = append(config.AllowHeaders, "Authorization", "x-calendar-origin")
	config.AllowOrigins = []string{"*"}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(cors.New(config))

	merchant := router.Group("/info/:merchantid")

	addTicketRoutes(merchant, db)
	addScheduleRoutes(merchant, db)
	addReportRoutes(merchant, db)
	addProductRoutes(merchant, db)
	addUserRoutes(merchant, db)
	addMerchantConfigRoutes(merchant, db)
	stripe.AddStripeRoutes(merchant, getStripeAcct(db), db)
	merchant.GET("/passes/:checkoutid", GetBoardingPasses(db))
	merchant.GET("/logactions", checkJWT(), getLogActions(db))

	router.POST("/stripehook", stripe.StripeWebhook(db))
	router.POST("/paypal", HandlePaypalWebhook(db))
	router.POST("/confirmed", ConfirmAndSend(db))
	router.POST("/sendmail", Resend(db))
	router.POST("/sendtext", SendText(db))
	router.POST("/capture", CaptureOrder(db))
	router.GET("/transaction/:transaction", GetItems(db))
	// router.POST("/sendrefund", RefundReq(db))

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 20 seconds
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but you can't catch that
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown: ", err)
	}
	log.Println("Server Exiting")
}
