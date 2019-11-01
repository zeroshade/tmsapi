package main

import (
	"log"
	"net/http"
	"os"

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
	db.AutoMigrate(&Product{}, &Schedule{}, &ScheduleTime{}, &NotAvail{}, &TicketCategory{},
		&Transaction{}, &Payment{}, &Sale{})
	db.Model(&Schedule{}).Association("TimeArray")
	db.Model(&Schedule{}).Association("NotAvail")

	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS hstore").Error; err != nil {
		log.Fatal(err)
	}

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
	router.POST("/paypal", HandlePaypalWebhook(db))
	router.Run(":" + port)
}
