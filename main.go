package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

func main() {
	URI := os.Getenv("DATABASE_URL")
	if URI == "" {
		log.Fatal("must set $DATABASE_URL")
	}

	db, err := gorm.Open("postgres", URI)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.AutoMigrate(&Product{}, &Schedule{}, &ScheduleTime{}, &TicketCategory{},
		&Transaction{}, &Payment{}, &Sale{}, &PayerInfo{}, &WebHookEvent{}, &Item{},
		&CheckoutOrder{}, &Payer{}, &PurchaseItem{}, &PurchaseUnit{}, &Capture{})
	db.Model(&Schedule{}).Association("TimeArray")
	db.Model(&Schedule{}).Association("NotAvail")
	db.Model(&Payment{}).Association("Payer.PayerInfo")
	db.Model(&Item{}).AddForeignKey("transaction", "transactions(payment_id)", "CASCADE", "RESTRICT")
	db.Model(&Transaction{}).AddForeignKey("payment_id", "payments(id)", "CASCADE", "RESTRICT")
	db.Table("transaction_related").AddForeignKey("transaction_payment_id", "payments(id)", "CASCADE", "RESTRICT")
	db.Table("transaction_related").AddForeignKey("sale_id", "sales(id)", "CASCADE", "RESTRICT")
	db.Model(&PurchaseUnit{}).AddForeignKey("checkout_id", "checkout_orders(id)", "CASCADE", "RESTRICT")
	db.Model(&PurchaseItem{}).AddForeignKey("checkout_id", "checkout_orders(id)", "CASCADE", "RESTRICT")

	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS hstore").Error; err != nil {
		log.Fatal(err)
	}

	db.Exec("SET TIME ZONE 'America/New_York'")

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("must set $PORT")
	}

	config := cors.DefaultConfig()
	config.AllowHeaders = append(config.AllowHeaders, "Authorization")
	config.AllowOrigins = []string{"*"}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(cors.New(config))

	merchant := router.Group("/info/:merchantid")

	merchant.GET("/", func(c *gin.Context) {
		var prods []Product
		db.Preload("Schedules").Preload("Schedules.TimeArray").Find(&prods, "merchant_id = ?", c.Param("merchantid"))
		c.JSON(http.StatusOK, prods)
	})

	merchant.PUT("/product", SaveProduct(db))
	merchant.PUT("/tickets", SaveTicketCats(db))
	merchant.GET("/tickets", GetTicketCats(db))
	merchant.GET("/schedule/:from/:to", func(c *gin.Context) {
		type result struct {
			Stamp time.Time `json:"stamp"`
			Qty   uint      `json:"qty"`
		}

		sub := db.Model(&PurchaseItem{}).
			Select([]string{"checkout_id",
				"TO_TIMESTAMP(LEFT(RIGHT(sku, 13), -3)::INTEGER) as tm",
				"SUM(quantity) as q"}).Group("checkout_id, tm").SubQuery()

		var out []result
		db.Table("purchase_units as pu").
			Select("tm as stamp, sum(q) as qty").
			Joins("RIGHT JOIN ? as sub ON pu.checkout_id = sub.checkout_id", sub).
			Where("pu.payee_merchant_id = ? AND tm BETWEEN TO_TIMESTAMP(?) AND TO_TIMESTAMP(?)",
				c.Param("merchantid"), c.Param("from"), c.Param("to")).
			Group("tm").Scan(&out)

		c.JSON(http.StatusOK, out)
	})

	router.POST("/paypal", HandlePaypalWebhook(db))
	router.GET("/transaction/:transaction", GetItems(db))

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
