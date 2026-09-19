package mirror

import (
	_ "embed"
	"encoding/json"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/page.json
var pageStringsJSON []byte

var pageStrings = func() map[string]map[string]string {
	var out map[string]map[string]string
	if err := json.Unmarshal(pageStringsJSON, &out); err != nil {
		panic("mirror: templates/page.json: " + err.Error())
	}
	return out
}()

type pageView struct {
	Lang         string
	T            map[string]string
	Upstream     string
	UpstreamHost string
	PublicHost   string
	Catalogue    string
	Sets         string
	Built        string
	Expires      string
	LastRefresh  string
	Stale        bool
	Queued       string
	Announced    string
	Outcome      string
	Version      string
}

func pageLang(r *http.Request) string {
	best, bestQ := "en", -1.0
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		fields := strings.Split(part, ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		lang := ""
		switch {
		case tag == "ru" || strings.HasPrefix(tag, "ru-"):
			lang = "ru"
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			lang = "en"
		default:
			continue
		}
		q := 1.0
		for _, param := range fields[1:] {
			if kv := strings.SplitN(strings.TrimSpace(param), "=", 2); len(kv) == 2 && strings.EqualFold(strings.TrimSpace(kv[0]), "q") {
				if v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err == nil {
					q = v
				}
			}
		}
		if q > 0 && q > bestQ {
			best, bestQ = lang, q
		}
	}
	return best
}

func plural(lang string, n int) string {
	if lang != "ru" {
		if n == 1 {
			return "one"
		}
		return "other"
	}
	mod10, mod100 := n%10, n%100
	switch {
	case mod10 == 1 && mod100 != 11:
		return "one"
	case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
		return "few"
	default:
		return "many"
	}
}

func count(lang, noun string, n int) string {
	form := pageStrings[lang][noun+"_"+plural(lang, n)]
	if form == "" {
		form = pageStrings["en"][noun+"_other"]
	}
	return strings.Replace(form, "%d", strconv.Itoa(n), 1)
}

func hostOf(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.Path != "" && u.Path != "/" {
		return u.Host + u.Path
	}
	return u.Host
}

func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func stampRFC(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return stamp(t)
}

func newPageView(lang string, st Status) pageView {
	strs := pageStrings[lang]
	v := pageView{
		Lang:         lang,
		T:            strs,
		Upstream:     st.Upstream,
		UpstreamHost: hostOf(st.Upstream),
		Version:      st.Version,
		Stale:        st.LastError != "",
		LastRefresh:  strs["never"],
	}
	if st.PublicURL != "" {
		v.PublicHost = hostOf(st.PublicURL)
	}
	if st.Manifest != nil {
		v.Catalogue = strconv.FormatInt(st.Manifest.Epoch, 10) + "-" + strconv.FormatInt(st.Manifest.Seq, 10)
		v.Sets = count(lang, "sets", st.Sets)
		v.Built = stampRFC(st.Manifest.GeneratedAt)
		v.Expires = stampRFC(st.Manifest.ExpiresAt)
	}
	if !st.LastRefresh.IsZero() {
		v.LastRefresh = stamp(st.LastRefresh)
	}
	if st.Queued > 0 {
		v.Queued = count(lang, "records", st.Queued)
	}
	if !st.LastAnnounce.IsZero() {
		v.Announced = stamp(st.LastAnnounce)
		v.Outcome = strs[st.AnnounceState]
		if v.Outcome == "" {
			v.Outcome = strs["accepted"]
		}
	}
	return v
}
