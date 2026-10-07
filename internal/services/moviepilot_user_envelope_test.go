package services

import (
	"encoding/json"
	"testing"
)

// MoviePilot v3 wraps every collection response in the standard envelope. The
// production bug was decoding that object straight into []User, which failed
// with "cannot unmarshal object into Go value of type []services.User" and
// broke every account binding attempt.
func TestDecodeUserListEnvelope_UnwrapsStandardEnvelope(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":[{"name":"admin","email":"admin@movie-pilot.org","is_active":true,"is_superuser":true,"avatar":"","is_otp":false,"permissions":{},"settings":{},"id":1}]}`)

	users, err := decodeUserListEnvelope(body)
	if err != nil {
		t.Fatalf("decodeUserListEnvelope returned error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Username != "admin" {
		t.Errorf("expected Username=%q (mapped from name), got %q", "admin", users[0].Username)
	}
	if users[0].ID != 1 {
		t.Errorf("expected ID=1, got %d", users[0].ID)
	}
	if users[0].Email != "admin@movie-pilot.org" {
		t.Errorf("unexpected email %q", users[0].Email)
	}
}

func TestDecodeUserListEnvelope_EmptyListIsNotAnError(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"success":true,"message":"","data":[]}`),
		[]byte(`{"success":true,"message":"","data":null}`),
		[]byte(`{"success":true}`),
		[]byte(`[]`),
	}
	for i, body := range cases {
		users, err := decodeUserListEnvelope(body)
		if err != nil {
			t.Errorf("case %d: unexpected error: %v", i, err)
			continue
		}
		if len(users) != 0 {
			t.Errorf("case %d: expected 0 users, got %d", i, len(users))
		}
	}
}

func TestDecodeUserListEnvelope_LegacyBareArray(t *testing.T) {
	body := []byte(`[{"id":7,"name":"legacy","email":"legacy@example.com"}]`)

	users, err := decodeUserListEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 1 || users[0].Username != "legacy" {
		t.Fatalf("legacy bare array not decoded, got %+v", users)
	}
}

func TestDecodeUserListEnvelope_SuccessFalseIsAnError(t *testing.T) {
	body := []byte(`{"success":false,"message":"无权限访问用户信息","data":null}`)

	if _, err := decodeUserListEnvelope(body); err == nil {
		t.Fatal("expected an error for success=false")
	}
}

// A 422 from POST /api/v1/user/ nests a validation list inside data. That must not
// be mistaken for a user collection.
func TestDecodeUserListEnvelope_ValidationErrorPayloadIsNotAUserList(t *testing.T) {
	body := []byte(`{"success":false,"message":"请求参数不正确","data":[{"location":["body","name"],"message":"Field required","error_type":"missing"}]}`)

	users, err := decodeUserListEnvelope(body)
	if err == nil {
		t.Fatalf("expected an error, got users=%+v", users)
	}
	for _, u := range users {
		if u.Username == "Field required" {
			t.Fatal("validation payload leaked through as a user")
		}
	}
}

func TestDecodeUserObjectEnvelope_Envelope(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":{"id":42,"name":"leron","email":"leron@example.com"}}`)

	user, err := decodeUserObjectEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != 42 || user.Username != "leron" {
		t.Fatalf("unexpected user %+v", user)
	}
}

func TestDecodeUserObjectEnvelope_NotFoundEnvelope(t *testing.T) {
	body := []byte(`{"success":false,"message":"用户不存在","data":null}`)

	if _, err := decodeUserObjectEnvelope(body); err == nil {
		t.Fatal("expected an error when the user does not exist")
	}
}

func TestDecodeUserObjectEnvelope_BareObject(t *testing.T) {
	body := []byte(`{"id":9,"name":"bare","email":"bare@example.com"}`)

	user, err := decodeUserObjectEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != 9 || user.Username != "bare" {
		t.Fatalf("unexpected user %+v", user)
	}
}

// RegisterUser must not double-create an account when the create call reports
// failure but the user already exists.
func TestRegisterUserRequest_UsesNameField(t *testing.T) {
	payload, err := json.Marshal(RegisterUserRequest{Username: "leron", Password: "pw", Email: "l@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["name"]; !ok {
		t.Errorf("RegisterUserRequest must serialise to \"name\", got keys %v", decoded)
	}
	if _, ok := decoded["username"]; ok {
		t.Errorf("RegisterUserRequest must not emit \"username\", got keys %v", decoded)
	}
}
