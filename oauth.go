package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/zeroshade/tmsapi/internal"
)

var auth0Client *internal.Auth0Client

func init() {
	auth0Client = internal.NewAuth0Client()
}

func addUserRoutes(router *gin.RouterGroup, db *gorm.DB) {
	router.GET("/users", checkJWT(), getUsers())
	router.POST("/user", checkJWT(), createUser())
	router.DELETE("/user/:userid", checkJWT(), deleteUser())
}

func getUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		admins := auth0Client.GetUsersByRole("admin")
		users := auth0Client.GetUsers(fmt.Sprintf(`app_metadata.merchant_id:"%s"`, c.Param("merchantid")))

		c.JSON(http.StatusOK, append(admins, users...))
	}
}

func createUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		var u internal.User
		if err := c.ShouldBindJSON(&u); err != nil {
			log.Println(err.Error())
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		u.AppMetadata["merchant_id"] = json.RawMessage([]byte(`"` + c.Param("merchantid") + `"`))
		auth0Client.CreateUser(&u)

		auth0Client.AssignRoles(u.UserID, "captain")
	}
}

func deleteUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0Client.DeleteUser(c.Param("userid"))
		c.Status(http.StatusOK)
	}
}
