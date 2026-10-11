package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Signed cookies keep session memory constant. Restarting invalidates sessions.
type Auth struct {
	secret   []byte
	password string
}

func New(password string) (*Auth, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Auth{b, password}, nil
}
func (a *Auth) mac(s string) string {
	h := hmac.New(sha256.New, a.secret)
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}
func (a *Auth) token(role string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	s := role + ":" + strconv.FormatInt(time.Now().Add(12*time.Hour).Unix(), 10) + ":" + hex.EncodeToString(b)
	return s + ":" + a.mac(s)
}
func (a *Auth) valid(s string) (string, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 4 {
		return "", false
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expiry < time.Now().Unix() {
		return "", false
	}
	body := strings.Join(parts[:3], ":")
	if !hmac.Equal([]byte(parts[3]), []byte(a.mac(body))) {
		return "", false
	}
	return parts[0], true
}
func (a *Auth) set(c fiber.Ctx, t string) {
	c.Cookie(&fiber.Cookie{Name: "fluxport_session", Value: t, Path: "/", HTTPOnly: true, Secure: c.Secure(), SameSite: "Strict", MaxAge: 43200})
}
func (a *Auth) Middleware(c fiber.Ctx) error {
	if strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/static/") || c.Path() == "/healthz" {
		return c.Next()
	}
	t := c.Cookies("fluxport_session")
	role, ok := a.valid(t)
	if !ok {
		t = a.token("guest")
		a.set(c, t)
	}
	c.Locals("csrf", a.mac("csrf:"+t))
	if c.Method() != "GET" && c.Method() != "HEAD" {
		got := c.Get("X-CSRF-Token")
		if got == "" {
			if token, ok := c.Locals("multipart_csrf").(string); ok {
				got = token
			} else {
				got = c.FormValue("csrf")
			}
		}
		if !hmac.Equal([]byte(got), []byte(a.mac("csrf:"+t))) {
			return fiber.NewError(403, "Invalid CSRF token. Reload the page and try again.")
		}
	}
	if a.password != "" && role != "admin" && c.Path() != "/login" {
		if c.Get("HX-Request") == "true" {
			c.Set("HX-Redirect", "/login")
			return c.SendStatus(401)
		}
		return c.Redirect().Status(303).To("/login")
	}
	return c.Next()
}
func (a *Auth) Login(c fiber.Ctx) error {
	want := sha256.Sum256([]byte(a.password))
	got := sha256.Sum256([]byte(c.FormValue("password")))
	if a.password != "" && subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		return fiber.NewError(401, "Incorrect dashboard password.")
	}
	a.set(c, a.token("admin"))
	return c.Redirect().Status(303).To("/")
}
func (a *Auth) Logout(c fiber.Ctx) error {
	a.set(c, a.token("guest"))
	return c.Redirect().Status(303).To("/login")
}
func API(key string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if key != "" {
			if !strings.HasPrefix(c.Get("Authorization"), "Bearer ") {
				c.Set("WWW-Authenticate", "Bearer")
				return fiber.NewError(401, "A bearer API key is required.")
			}
			got := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
			expected := sha256.Sum256([]byte(key))
			actual := sha256.Sum256([]byte(got))
			if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
				c.Set("WWW-Authenticate", "Bearer")
				return fiber.NewError(401, "A valid bearer API key is required.")
			}
		}
		return c.Next()
	}
}
func CSRF(c fiber.Ctx) string { return fmt.Sprint(c.Locals("csrf")) }
