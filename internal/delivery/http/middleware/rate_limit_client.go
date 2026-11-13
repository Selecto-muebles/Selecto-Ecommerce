package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/netip"

	"github.com/gin-gonic/gin"
)

const rateLimitClientKey = "rate_limit_client"

// RateLimitClient accepts an address only when our load balancer overwrites
// both headers with its private token and the observed client IP. Forwarded
// headers supplied by the browser alone must never create a new quota.
func RateLimitClient(edgeSecret string) gin.HandlerFunc {
	expected := sha256.Sum256([]byte(edgeSecret))
	return func(c *gin.Context) {
		token := c.GetHeader("X-Selecto-Edge-Token")
		address := c.GetHeader("X-Selecto-Client-IP")
		c.Request.Header.Del("X-Selecto-Edge-Token")
		c.Request.Header.Del("X-Selecto-Client-IP")
		provided := sha256.Sum256([]byte(token))
		if len(edgeSecret) >= 32 && subtle.ConstantTimeCompare(expected[:], provided[:]) == 1 {
			if ip, err := netip.ParseAddr(address); err == nil && ip.Zone() == "" {
				c.Set(rateLimitClientKey, ip.Unmap().String())
			}
		}
		c.Next()
	}
}

func rateLimitClient(c *gin.Context) string {
	if address, ok := c.Get(rateLimitClientKey); ok {
		return "edge:" + address.(string)
	}
	// Prefixes prevent direct gateway traffic from spending a verified client's
	// quota, even when a gateway address coincides with a visitor's address.
	return "peer:" + c.ClientIP()
}
