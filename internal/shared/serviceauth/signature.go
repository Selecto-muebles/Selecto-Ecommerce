package serviceauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

func AddHeaders(req *http.Request, secret string, body []byte, requestID, correlationID string) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Service-Name", "selecto-ecommerce")
	req.Header.Set("X-Service-Timestamp", timestamp)
	req.Header.Set("X-Service-Signature", "sha256="+Signature(secret, timestamp, body))
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("X-Correlation-ID", correlationID)
}

func Signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
