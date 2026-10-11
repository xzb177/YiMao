package services

import (
	"testing"
)

// MoviePilot v3 returns genres as objects: [{"id":28,"name":"动作"}].
// Declaring the field as []string made Go report
// "cannot unmarshal object into Go struct field MediaInfo.data.genres of type string"
// and aborted the entire media detail response, so every detail page fell back to
// "状态暂未确认". The field must accept both shapes.
func TestDecodeMediaInfoEnvelope_GenresAsObjects(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":{"title":"流浪地球","tmdb_id":535167,"type":"电影",` +
		`"genres":[{"id":878,"name":"科幻"},{"id":28,"name":"动作"}]}}`)

	info, err := decodeMediaInfoEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.Genres) != 2 {
		t.Fatalf("genres=%v, want 2 names", info.Genres)
	}
	if info.Genres[0] != "科幻" || info.Genres[1] != "动作" {
		t.Errorf("genres=%v, want [科幻 动作]", info.Genres)
	}
}

func TestDecodeMediaInfoEnvelope_GenresAsStrings(t *testing.T) {
	body := []byte(`{"success":true,"data":{"title":"旧格式","tmdb_id":1,"genres":["剧情","悬疑"]}}`)

	info, err := decodeMediaInfoEnvelope(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.Genres) != 2 || info.Genres[0] != "剧情" || info.Genres[1] != "悬疑" {
		t.Errorf("genres=%v, want [剧情 悬疑]", info.Genres)
	}
}

// Mixed entries, nulls and blank names must not abort the whole response.
func TestDecodeMediaInfoEnvelope_GenresTolerantShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"null", `null`, nil},
		{"empty array", `[]`, []string{}},
		{"object with blank name", `[{"id":1,"name":"  "}]`, []string{}},
		{"single string", `"剧情"`, []string{"剧情"}},
		{"single object", `{"id":28,"name":"动作"}`, []string{"动作"}},
		{"mixed", `["剧情",{"id":28,"name":"动作"},null,5]`, []string{"剧情", "动作"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"success":true,"data":{"title":"x","tmdb_id":1,"genres":` + tc.raw + `}}`)
			info, err := decodeMediaInfoEnvelope(body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(info.Genres) != len(tc.want) {
				t.Fatalf("genres=%v, want %v", info.Genres, tc.want)
			}
			for i := range tc.want {
				if info.Genres[i] != tc.want[i] {
					t.Fatalf("genres=%v, want %v", info.Genres, tc.want)
				}
			}
		})
	}
}

// The wish-pool re-search aborted with
// "failed to decode sites response: json: cannot unmarshal object into Go value of
// type []services.SiteInfo" because /api/v1/site/ answers with the standard
// {success, message, data} envelope instead of a bare array.
func TestDecodeListEnvelope_UnwrapsStandardEnvelope(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":[{"id":1,"name":"馒头"},{"id":2,"name":"观众"}]}`)

	sites, err := decodeListEnvelope[SiteInfo](body, "sites response")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 2 || sites[0].Name != "馒头" || sites[1].Name != "观众" {
		t.Fatalf("sites=%v, want 馒头/观众", sites)
	}
}

func TestDecodeListEnvelope_LegacyBareArray(t *testing.T) {
	body := []byte(`[{"id":1,"name":"馒头"}]`)

	sites, err := decodeListEnvelope[SiteInfo](body, "sites response")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 1 || sites[0].Name != "馒头" {
		t.Fatalf("sites=%v, want 馒头", sites)
	}
}

func TestDecodeListEnvelope_EmptyDataIsNotAnError(t *testing.T) {
	for _, raw := range []string{`{"success":true,"message":"","data":null}`, `{"success":true,"data":[]}`} {
		sites, err := decodeListEnvelope[SiteInfo]([]byte(raw), "sites response")
		if err != nil {
			t.Fatalf("body=%s: unexpected error: %v", raw, err)
		}
		if len(sites) != 0 {
			t.Fatalf("body=%s: sites=%v, want empty", raw, sites)
		}
	}
}

func TestDecodeListEnvelope_SuccessFalseIsAnError(t *testing.T) {
	body := []byte(`{"success":false,"message":"请先配置站点","data":null}`)

	if _, err := decodeListEnvelope[SiteInfo](body, "sites response"); err == nil {
		t.Fatal("expected an error for success=false")
	}
}

// The same envelope wrapping applies to the per-site resource search, which
// previously fed the object straight into []TorrentResource.
func TestDecodeListEnvelope_UnwrapsResourceEnvelope(t *testing.T) {
	body := []byte(`{"success":true,"message":"","data":[{"site_name":"馒头","title":"示例资源","seeders":12}]}`)

	resources, err := decodeListEnvelope[TorrentResource](body, "resources response")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].Title != "示例资源" || resources[0].Seeders != 12 {
		t.Fatalf("resources=%v", resources)
	}
}
