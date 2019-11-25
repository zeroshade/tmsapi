package main

import (
	"log"
	"net/http"
	"sort"

	"github.com/auth0-community/go-auth0"
	"github.com/gin-gonic/gin"
)

const (
	// JWKURI URI for JWK Token
	JWKURI = "https://tmszero.auth0.com/.well-known/jwks.json"
	// AUDIENCE Our backend audience definition
	AUDIENCE = "http://tmszero.auth0.com/"
	// USERAPI the url for the user API communication
	USERAPI = "https://tmszero.auth0.com/api/v2/"
	// AUTH0DOMAIN the url for the auth0 domain
	AUTH0DOMAIN = "https://tmszero.auth0.com/"
)

var validator *auth0.JWTValidator

func init() {
	client := auth0.NewJWKClient(auth0.JWKClientOptions{URI: JWKURI}, nil)
	configuration := auth0.NewConfiguration(client, []string{USERAPI}, AUTH0DOMAIN, "RS256")
	validator = auth0.NewValidator(configuration, nil)
}

func checkJWT(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tok, err := validator.ValidateRequest(c.Request)
		if err != nil {
			log.Println("Token isn't valid:", tok)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		claims := map[string]interface{}{}
		custom := struct {
			Subject string           `json:"sub"`
			Perms   sort.StringSlice `json:"https://kithandkink.com/permissions"`
		}{}

		err = validator.Claims(c.Request, tok, &claims, &custom)
		if err != nil {
			log.Println("invalid Claims:", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
			c.Abort()
			return
		}

		custom.Perms.Sort()
		for _, p := range perms {
			find := custom.Perms.Search(p)
			if find == custom.Perms.Len() || custom.Perms[find] != p {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing permissions"})
				c.Abort()
				log.Println("MIssing permission: ", p)
				return
			}
		}

		c.Set("user_id", custom.Subject)
		c.Next()
	}
}