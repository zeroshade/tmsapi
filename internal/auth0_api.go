package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const ClientID = "zeGsn01T7JsFjLTRMuSAwuxwZeGfZUE0"
const ClientSecret = "jC4KV7fjA33ZPLUTIZ1LSWXZLsZ4sAkOVYrPeMroGMz3T8Oor9zQDAu1Z-m3GGXu"
const Audience = "https://tmszero.auth0.com/api/v2/"
const OAuthURL = "https://tmszero.auth0.com/oauth/token"

type token struct {
	AccessToken string    `json:"access_token"`
	Scopes      string    `json:"scope"`
	Expires     int       `json:"expires_in"`
	ExpiresAt   time.Time `json:"-"`
	TokenType   string    `json:"token_type"`
}

func getToken() *token {
	payload := strings.NewReader(fmt.Sprintf(`{"client_id": "%s", "client_secret": "%s", "audience": "%s", "grant_type": "client_credentials"}`,
		ClientID, ClientSecret, Audience))

	req, _ := http.NewRequest("POST", OAuthURL, payload)
	req.Header.Add("content-type", "application/json")
	res, _ := http.DefaultClient.Do(req)
	defer res.Body.Close()

	dec := json.NewDecoder(res.Body)
	tok := &token{}
	if err := dec.Decode(tok); err != nil {
		log.Println("Failed to decode access token")
		return nil
	}
	return tok
}

type authtransport struct {
	http.Transport
	curToken *token
}

func (a *authtransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.curToken == nil || a.curToken.ExpiresAt.Before(time.Now()) {
		a.curToken = getToken()
		a.curToken.ExpiresAt = time.Now().Add(time.Duration(a.curToken.Expires) * time.Second)
	}

	req.Header.Add("Authorization", fmt.Sprintf("%s %s", a.curToken.TokenType, a.curToken.AccessToken))
	return a.Transport.RoundTrip(req)
}

type Auth0Client struct {
	client *http.Client
}

func NewAuth0Client() *Auth0Client {
	return &Auth0Client{&http.Client{Transport: &authtransport{}}}
}

func (a *Auth0Client) SendRequest(req *http.Request) (*http.Response, error) {
	return a.client.Do(req)
}

type User struct {
	Email       string                     `json:"email"`
	UserID      string                     `json:"user_id,omitempty"`
	Name        string                     `json:"name"`
	AppMetadata map[string]json.RawMessage `json:"app_metadata"`
	Password    string                     `json:"password,omitempty"`
	Connection  string                     `json:"connection,omitempty"`
	Username    string                     `json:"username"`
}

type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"description"`
}

func (a *Auth0Client) GetRoles(roles ...string) []*Role {
	u, _ := url.Parse(Audience)
	u.Path += "roles"

	res, _ := a.client.Get(u.String())
	defer res.Body.Close()
	ret := make([]*Role, 0)
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&ret); err != nil {
		log.Println("failed to decode roles: ", err)
		return nil
	}

	filtered := make([]*Role, 0, len(ret))
	for _, r := range ret {
		for _, s := range roles {
			if r.Name == s {
				filtered = append(filtered, r)
			}
		}
	}
	return filtered
}

func (a *Auth0Client) GetRole(role string) *Role {
	u, _ := url.Parse(Audience)
	u.Path += "roles"

	v := u.Query()
	v.Add("name_filter", role)
	u.RawQuery = v.Encode()

	res, _ := a.client.Get(u.String())
	defer res.Body.Close()
	roles := make([]Role, 0)
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&roles); err != nil {
		log.Println("failed to decode roles: ", err)
		return nil
	}
	return &roles[0]
}

func (a *Auth0Client) GetUserByID(userid string) *User {
	res, _ := a.client.Get(Audience + "users/" + userid)
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)

	u := &User{}
	if err := dec.Decode(u); err != nil {
		log.Println("Failed to decode user: ", err)
	}
	return u
}

func (a *Auth0Client) GetUsersByRole(role string) []*User {
	users := make([]User, 0)
	therole := a.GetRole(role)
	if therole == nil {
		return make([]*User, 0)
	}

	u, _ := url.Parse(Audience)
	u.Path += "roles/" + therole.ID + "/users"

	res, _ := a.client.Get(u.String())
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&users); err != nil {
		log.Println("Failed to decode users: ", err)
		return make([]*User, 0)
	}

	ret := make([]*User, 0, len(users))
	for _, i := range users {
		user := a.GetUserByID(i.UserID)
		if user.AppMetadata == nil {
			user.AppMetadata = make(map[string]json.RawMessage)
		}
		user.AppMetadata["role"] = json.RawMessage([]byte(`"` + role + `"`))
		ret = append(ret, user)
	}
	return ret
}

func (a *Auth0Client) GetUsers(query string) []*User {
	u, _ := url.Parse(Audience)
	u.Path += "users"

	v := u.Query()
	v.Add("q", query)
	v.Add("search_engine", "v3")
	u.RawQuery = v.Encode()

	res, _ := a.client.Get(u.String())
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	users := make([]*User, 0)
	if err := dec.Decode(&users); err != nil {
		log.Println("failed to decode users")
	}
	return users
}

func (a *Auth0Client) CreateUser(u *User) {
	u.Connection = "Username-Password-Authentication"
	body, err := json.Marshal(*u)
	if err != nil {
		log.Println(err)
		return
	}

	resp, _ := a.client.Post(Audience+"users", "application/json", bytes.NewReader(body))

	switch resp.StatusCode {
	case http.StatusCreated:
		log.Println("Created user")
	case http.StatusBadRequest:
		fallthrough
	case http.StatusUnauthorized:
		fallthrough
	case http.StatusForbidden:
		fallthrough
	case http.StatusConflict:
		fallthrough
	case http.StatusTooManyRequests:
		fallthrough
	default:
		log.Println(resp.Status)
		resp.Write(os.Stdout)
	}

	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(u); err != nil {
		log.Println("Failed to decode response: ", err)
	}
}

func (a *Auth0Client) AssignRoles(userid string, roles ...string) {
	type reqbody struct {
		Roles []string `json:"roles"`
	}

	rolelist := a.GetRoles(roles...)
	r := &reqbody{}
	for _, obj := range rolelist {
		r.Roles = append(r.Roles, obj.ID)
	}

	u, _ := url.Parse(Audience)
	u.Path += "users/" + userid + "/roles"

	body, _ := json.Marshal(r)
	resp, _ := a.client.Post(u.String(), "application/json", bytes.NewReader(body))
	switch resp.StatusCode {
	case http.StatusNoContent:
		log.Println("Assigned Roles")
	default:
		log.Println(resp.Status)
	}
}

func (a *Auth0Client) ResetPassword(userid string, passwd string) {
	type reqBody struct {
		Passwd string `json:"password"`
	}

	r := &reqBody{}
	r.Passwd = passwd

	u, _ := url.Parse(Audience)
	u.Path += "users/" + userid

	body, _ := json.Marshal(r)
	req, _ := http.NewRequest("PATCH", u.String(), bytes.NewReader(body))
	req.Header.Add("Content-Type", "application/json")

	resp, _ := a.client.Do(req)
	log.Println(resp.Status)
}

func (a *Auth0Client) DeleteUser(userid string) {
	req, _ := http.NewRequest("DELETE", Audience+"users/"+userid, nil)
	resp, _ := a.client.Do(req)
	switch resp.StatusCode {
	case http.StatusNoContent:
		log.Println("User Deleted")
	default:
		log.Println(resp.Status)
	}
}
