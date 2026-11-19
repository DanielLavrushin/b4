package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func geoipBytesWith(t *testing.T, tags ...string) []byte {
	t.Helper()
	list := &v2data.GeoIPList{}
	for i, tag := range tags {
		list.Entry = append(list.Entry, &v2data.GeoIP{
			CountryCode: strings.ToUpper(tag),
			Cidr:        []*v2data.CIDR{{Ip: []byte{10, byte(i), 0, 0}, Prefix: 16}},
		})
	}
	b, err := proto.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func uploadGeodat(t *testing.T, api *API, kind, dir string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", kind+".dat")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(body)
	_ = mw.WriteField("type", kind)
	_ = mw.WriteField("destination_path", dir)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/geodat/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	return rec
}

func geoAPI(t *testing.T) *API {
	t.Helper()
	useAsnStore(t)
	countRefreshes(t)
	cdn := asnSet("cdn")
	cdn.Targets.GeoIpCategories = []string{"cloudflare", "amazon"}
	sites := asnSet("sites")
	sites.Targets.GeoSiteCategories = []string{"youtube", "netflix"}
	api, _ := asnAPI(t, cdn, sites)
	api.RegisterGeodatApi()
	sitePath, _ := existingGeosite(t)
	api.getCfg().System.Geo.GeoSitePath = sitePath
	return api
}

func TestGeodatUploadListsTheCategoriesTheNewFileLacks(t *testing.T) {
	api := geoAPI(t)
	dir := t.TempDir()

	var reply struct {
		Unmatched []GeodatUnmatched `json:"unmatched"`
	}
	rec := uploadGeodat(t, api, "geoip", dir, geoipBytesWith(t, "cloudflare", "ru"))
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	want := []GeodatUnmatched{{SetID: "cdn", SetName: "Set cdn", GeoIP: []string{"amazon"}}}
	if !reflect.DeepEqual(reply.Unmatched, want) {
		t.Fatalf("the reply names the set and the category the file lacks, and leaves out the geosite set because that file was not replaced: %+v", reply.Unmatched)
	}

	rec = uploadGeodat(t, api, "geoip", dir, geoipBytesWith(t, "cloudflare", "amazon"))
	reply.Unmatched = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Unmatched) != 0 {
		t.Fatalf("a file holding every category lists nothing: %+v", reply.Unmatched)
	}
}

func TestGeodatDownloadListsTheCategoriesTheNewFileLacks(t *testing.T) {
	api := geoAPI(t)
	body := geoipBytesWith(t, "amazon")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	payload, _ := json.Marshal(GeodatDownloadRequest{GeoipURL: srv.URL + "/geoip.dat", DestinationPath: t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/geodat/download", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	var reply GeodatDownloadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	want := []GeodatUnmatched{{SetID: "cdn", SetName: "Set cdn", GeoIP: []string{"cloudflare"}}}
	if !reflect.DeepEqual(reply.Unmatched, want) {
		t.Fatalf("the download reply names the category the new file lacks: %+v", reply.Unmatched)
	}
}

func TestGeodatUploadListsAPresentCategoryWithNoEntries(t *testing.T) {
	api := geoAPI(t)
	list := &v2data.GeoIPList{Entry: []*v2data.GeoIP{
		{CountryCode: "CLOUDFLARE", Cidr: []*v2data.CIDR{{Ip: []byte{10, 0, 0, 0}, Prefix: 16}}},
		{CountryCode: "AMAZON"},
	}}
	body, err := proto.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	var reply struct {
		Unmatched []GeodatUnmatched `json:"unmatched"`
	}
	rec := uploadGeodat(t, api, "geoip", dir, body)
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	tags, err := api.geodataManager.ListCategories(filepath.Join(dir, "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tags, "amazon") {
		t.Fatalf("the uploaded file's inventory lists the tag with no entries: %v", tags)
	}
	want := []GeodatUnmatched{{SetID: "cdn", SetName: "Set cdn", GeoIP: []string{"amazon"}}}
	if !reflect.DeepEqual(reply.Unmatched, want) {
		t.Fatalf("a tag present in the file with no entries matches nothing and is listed: %+v", reply.Unmatched)
	}
}
