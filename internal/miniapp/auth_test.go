package miniapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidateInitDataAcceptsTelegramSignedPayload(t *testing.T) {
	botToken := "123456:TEST_TOKEN_FOR_UNIT_TEST_ONLY"
	values := url.Values{
		"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)},
		"query_id":  {"AAEAAAE"},
		"user":      {`{"id":5779291957,"first_name":"测试"}`},
	}
	values.Set("hash", signInitDataForTest(botToken, values))
	user, err := ValidateInitData(values.Encode(), botToken, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 5779291957 || user.FirstName != "测试" {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestValidateInitDataRejectsTamperedPayload(t *testing.T) {
	values := url.Values{"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)}, "user": {`{"id":1}`}, "hash": {"bad"}}
	if _, err := ValidateInitData(values.Encode(), "123456:TEST_TOKEN_FOR_UNIT_TEST_ONLY", time.Hour); err == nil {
		t.Fatal("tampered init data was accepted")
	}
}

func signInitDataForTest(botToken string, values url.Values) string {
	pairs := make([]string, 0, len(values))
	for key, vals := range values {
		if key == "hash" {
			continue
		}
		for _, value := range vals {
			pairs = append(pairs, key+"="+value)
		}
	}
	sort.Strings(pairs)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	check := hmac.New(sha256.New, secret.Sum(nil))
	check.Write([]byte(strings.Join(pairs, "\n")))
	return hex.EncodeToString(check.Sum(nil))
}
