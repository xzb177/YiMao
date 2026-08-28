package miniapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidInitData = errors.New("invalid telegram init data")

type AuthUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name,omitempty"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
}

func ValidateInitData(raw, botToken string, maxAge time.Duration) (AuthUser, error) {
	var zero AuthUser
	if strings.TrimSpace(raw) == "" || botToken == "" || maxAge <= 0 {
		return zero, ErrInvalidInitData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return zero, fmt.Errorf("%w: parse", ErrInvalidInitData)
	}
	hashHex := values.Get("hash")
	if hashHex == "" {
		return zero, fmt.Errorf("%w: missing hash", ErrInvalidInitData)
	}
	values.Del("hash")
	pairs := make([]string, 0, len(values))
	for key, vals := range values {
		for _, value := range vals {
			pairs = append(pairs, key+"="+value)
		}
	}
	sort.Strings(pairs)
	dataCheckString := strings.Join(pairs, "\n")
	// Telegram Mini Apps: secret_key = HMAC_SHA256(key="WebAppData", data=bot_token).
	secretMac := hmac.New(sha256.New, []byte("WebAppData"))
	secretMac.Write([]byte(botToken))
	digest := hmac.New(sha256.New, secretMac.Sum(nil))
	digest.Write([]byte(dataCheckString))
	provided, err := hex.DecodeString(hashHex)
	if err != nil || !hmac.Equal(provided, digest.Sum(nil)) {
		return zero, fmt.Errorf("%w: signature", ErrInvalidInitData)
	}
	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || authDate <= 0 {
		return zero, fmt.Errorf("%w: auth date", ErrInvalidInitData)
	}
	age := time.Since(time.Unix(authDate, 0))
	if age < -5*time.Minute || age > maxAge {
		return zero, fmt.Errorf("%w: expired", ErrInvalidInitData)
	}
	var user AuthUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return zero, fmt.Errorf("%w: user", ErrInvalidInitData)
	}
	return user, nil
}
