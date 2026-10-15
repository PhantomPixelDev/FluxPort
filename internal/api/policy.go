package api

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func collectionScope(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/collections/")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	if len(id) != 24 {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	return id
}
func (a *API) policy(c fiber.Ctx) error {
	if a.Policy == nil {
		return c.Next()
	}
	p := a.Policy.Global()
	origin := c.Get("Origin")
	if origin != "" {
		c.Vary("Origin")
		allowed := ""
		for _, value := range p.Origins {
			if value == "*" || value == origin {
				allowed = value
				break
			}
		}
		if allowed != "" {
			c.Set("Access-Control-Allow-Origin", allowed)
			c.Set("Access-Control-Expose-Headers", "X-Total-Count, X-Page, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, X-Collection-RateLimit-Limit, X-Collection-RateLimit-Remaining, X-Collection-RateLimit-Reset, Retry-After, Content-Disposition")
		}
		if c.Method() == "OPTIONS" && c.Get("Access-Control-Request-Method") != "" {
			c.Vary("Access-Control-Request-Method", "Access-Control-Request-Headers")
			if allowed == "" {
				return fiber.NewError(403, "This browser origin is not allowed.")
			}
			switch c.Get("Access-Control-Request-Method") {
			case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE":
			default:
				return fiber.NewError(403, "Unsupported API method.")
			}
			for _, name := range strings.Split(c.Get("Access-Control-Request-Headers"), ",") {
				switch strings.ToLower(strings.TrimSpace(name)) {
				case "", "accept", "authorization", "content-type":
				default:
					return fiber.NewError(403, "Unsupported cross-origin request header.")
				}
			}
			c.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE")
			c.Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
			c.Set("Access-Control-Max-Age", "600")
			return c.SendStatus(204)
		}
	}
	read := c.Method() == "GET" || c.Method() == "HEAD"
	decision := a.Policy.Check(a.Policy.ClientIP(c.RequestCtx().RemoteIP().String(), c.Get("X-Forwarded-For")), collectionScope(c.Path()), read)
	for i, b := range decision.Budgets {
		prefix := "X-RateLimit-"
		if b.Scope == "collection" && i > 0 {
			prefix = "X-Collection-RateLimit-"
		}
		c.Set(prefix+"Limit", strconv.Itoa(b.Limit))
		c.Set(prefix+"Remaining", strconv.Itoa(b.Remaining))
		c.Set(prefix+"Reset", strconv.FormatInt(b.Reset, 10))
	}
	if !decision.Allowed {
		c.Set("Retry-After", strconv.Itoa(decision.RetryAfter))
		if decision.Busy {
			return c.Status(503).JSON(fiber.Map{"error": "rate_limiter_busy", "message": "API limiter capacity is temporarily full.", "retry_after": decision.RetryAfter})
		}
		return c.Status(429).JSON(fiber.Map{"error": "rate_limited", "message": "API request limit exceeded. Retry after the indicated delay.", "retry_after": decision.RetryAfter})
	}
	return c.Next()
}
func (a *API) dataPageSize(c fiber.Ctx, fallback int) (int, error) {
	max := 100
	if a.Policy != nil {
		fallback, max = a.Policy.PageSize(c.Params("id"), fallback)
	}
	return integer(c, "limit", fallback, 1, max)
}
func (a *API) queryPageSize(id string, limit int) (int, error) {
	def, max := 20, 100
	if a.Policy != nil {
		def, max = a.Policy.PageSize(id, def)
	}
	if limit == 0 {
		return def, nil
	}
	if limit < 1 || limit > max {
		return 0, fiber.NewError(400, fmt.Sprintf("limit must be between 1 and %d", max))
	}
	return limit, nil
}
