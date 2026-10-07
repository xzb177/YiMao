package services

import (
	"strings"
	"testing"
)

func TestBuildMediaDetailEndpoint_IncludesRequiredMediaSource(t *testing.T) {
	ep := buildMediaDetailEndpoint(204268, "电视剧")

	// MoviePilot rejects the request with 422 when media_source is missing, so the
	// parameter is a hard requirement, not an optimisation.
	if !strings.Contains(ep, "media_source=tmdb") {
		t.Errorf("endpoint must carry media_source=tmdb, got %q", ep)
	}
	if !strings.Contains(ep, "type_name=") {
		t.Errorf("endpoint must carry type_name, got %q", ep)
	}
	if !strings.Contains(ep, "tmdbid=204268") {
		t.Errorf("endpoint must carry tmdbid, got %q", ep)
	}
}

func TestBuildMediaDetailEndpoint_EscapesChineseTypeName(t *testing.T) {
	ep := buildMediaDetailEndpoint(1, "电影")
	if strings.Contains(ep, "电影") {
		t.Errorf("Chinese type name must be URL escaped, got %q", ep)
	}
}

// A bare-zero MediaInfo made every request render as "状态暂未确认" because the
// envelope was decoded straight into the struct.
func TestDecodeMediaInfoEnvelope_UnwrapsStandardEnvelope(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":{"title":"弑仙之战","tmdb_id":204268,"type":"电视剧"}}`)

	info, err := decodeMediaInfoEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Title != "弑仙之战" {
		t.Errorf("expected title %q, got %q", "弑仙之战", info.Title)
	}
}

func TestDecodeMediaInfoEnvelope_BareObject(t *testing.T) {
	body := []byte(`{"title":"裸对象","tmdb_id":1}`)

	info, err := decodeMediaInfoEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Title != "裸对象" {
		t.Errorf("expected title %q, got %q", "裸对象", info.Title)
	}
}

func TestDecodeMediaInfoEnvelope_SuccessFalse(t *testing.T) {
	body := []byte(`{"success":false,"message":"请求参数不正确","data":[{"location":["query","media_source"],"message":"Field required","error_type":"missing"}]}`)

	if _, err := decodeMediaInfoEnvelope(body); err == nil {
		t.Fatal("expected an error for success=false")
	}
}

func TestDecodeMediaInfoEnvelope_EmptyDataIsNotAnError(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":null}`)

	info, err := decodeMediaInfoEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected a non-nil MediaInfo so the caller can report not-found")
	}
}
