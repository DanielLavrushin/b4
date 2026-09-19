package mirror

import (
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

var pageStrings = map[string]map[string]string{
	"en": {
		"title":         "b4 hub mirror",
		"brand":         "Community hub mirror",
		"leadBefore":    "Mirror of",
		"leadAfter":     ". It serves the hub's signed catalogue unchanged and forwards what routers send to the hub.",
		"catalogue":     "Catalogue",
		"noCopy":        "no copy yet",
		"built":         "Built",
		"expires":       "Expires",
		"lastCheck":     "Last check",
		"never":         "not yet",
		"inSync":        "in sync with the hub",
		"stale":         "the hub did not answer, serving the last copy",
		"queued":        "Waiting for the hub",
		"announced":     "Announced",
		"pending":       "accepted, awaiting approval",
		"approved":      "approved",
		"rejected":      "rejected",
		"accepted":      "accepted",
		"refused":       "refused by the hub",
		"failed":        "the hub could not be reached",
		"footer":        "Routers verify the catalogue signature themselves; a mirror cannot change what they receive.",
		"sets_one":      "%d set",
		"sets_other":    "%d sets",
		"records_one":   "%d record",
		"records_other": "%d records",
	},
	"ru": {
		"title":        "Зеркало хаба b4",
		"brand":        "Зеркало хаба сообщества",
		"leadBefore":   "Зеркало хаба",
		"leadAfter":    ". Оно отдаёт подписанный каталог хаба без изменений и передаёт хабу всё, что присылают роутеры.",
		"catalogue":    "Каталог",
		"noCopy":       "копии ещё нет",
		"built":        "Собран",
		"expires":      "Действует до",
		"lastCheck":    "Последняя проверка",
		"never":        "ещё не было",
		"inSync":       "синхронизировано с хабом",
		"stale":        "хаб не ответил, отдаётся последняя копия",
		"queued":       "Ждут отправки в хаб",
		"announced":    "Объявлено хабу",
		"pending":      "принято, ждёт одобрения",
		"approved":     "одобрено",
		"rejected":     "отклонено",
		"accepted":     "принято",
		"refused":      "хаб отказал",
		"failed":       "хаб недоступен",
		"footer":       "Роутеры сами проверяют подпись каталога; зеркало не может изменить то, что они получают.",
		"sets_one":     "%d сет",
		"sets_few":     "%d сета",
		"sets_many":    "%d сетов",
		"records_one":  "%d запись",
		"records_few":  "%d записи",
		"records_many": "%d записей",
	},
}

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
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		switch {
		case tag == "ru" || strings.HasPrefix(tag, "ru-"):
			return "ru"
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			return "en"
		}
	}
	return "en"
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
