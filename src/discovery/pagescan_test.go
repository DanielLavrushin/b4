package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

var jsSlash = `\` + "u002F"

func scanHosts(t *testing.T, page string) map[string]HostCandidate {
	t.Helper()
	scan := scanPage(strings.NewReader(page))
	cands, _ := scan.candidates()
	out := map[string]HostCandidate{}
	for _, c := range cands {
		out[c.Host] = c
	}
	return out
}

func wantHost(t *testing.T, hosts map[string]HostCandidate, host string, ctx HostContext, blocking bool) HostCandidate {
	t.Helper()
	c, ok := hosts[host]
	if !ok {
		t.Errorf("%s is not a candidate, want it as %s", host, ctx)
		return c
	}
	if c.Context != ctx || c.Blocking != blocking {
		t.Errorf("%s: context %q blocking %v, want %q blocking %v", host, c.Context, c.Blocking, ctx, blocking)
	}
	return c
}

func wantAbsent(t *testing.T, hosts map[string]HostCandidate, names ...string) {
	t.Helper()
	for _, name := range names {
		if c, ok := hosts[name]; ok {
			t.Errorf("%s must not be a candidate, got %+v", name, c)
		}
	}
}

func TestScanPageReadsLinkContextsWhateverTheAttributeOrder(t *testing.T) {
	hosts := scanHosts(t, `<html><head>
<link href="https://css.example.com/a.css" rel="stylesheet">
<link rel="canonical" href="https://canonical.example.com/">
<link rel="alternate" href="https://alternate.example.com/feed">
<link rel="alternate stylesheet" href="https://alt-style.example.com/b.css">
<link rel="preload" as="font" href="https://fonts.example.net/f.woff2" crossorigin>
<link rel="preconnect" href="https://api.example.org">
<link rel="dns-prefetch" href="//dns.example.org">
<link rel="shortcut icon" href="https://icons.example.net/favicon.ico">
<link rel="preload" as="image" imagesrcset="https://img1.example.com/a.jpg 1x,https://img2.example.com/b.jpg 2x">
</head></html>`)

	wantHost(t, hosts, "css.example.com", HostContextStylesheet, true)
	wantHost(t, hosts, "fonts.example.net", HostContextPreload, false)
	wantHost(t, hosts, "api.example.org", HostContextPreconnect, false)
	wantHost(t, hosts, "dns.example.org", HostContextPreconnect, false)
	wantHost(t, hosts, "icons.example.net", HostContextIcon, false)
	wantHost(t, hosts, "img1.example.com", HostContextPreload, false)
	wantHost(t, hosts, "img2.example.com", HostContextPreload, false)
	wantAbsent(t, hosts, "canonical.example.com", "alternate.example.com", "alt-style.example.com")
}

func TestScanPageTellsBlockingScriptsFromAsyncOnes(t *testing.T) {
	hosts := scanHosts(t, `<html><head>
<script src="https://plain.example.com/a.js"></script>
<script defer src="https://deferred.example.com/a.js"></script>
<script async src="https://async.example.com/a.js"></script>
<script type="module" src="https://module.example.com/a.js"></script>
<script type="module" async src="https://module-async.example.com/a.js"></script>
<script nomodule src="https://legacy.example.com/a.js"></script>
<script type="text/template" src="https://template.example.com/a.js"></script>
<SCRIPT SRC="https://upper.example.com/a.js"></SCRIPT>
</head></html>`)

	wantHost(t, hosts, "plain.example.com", HostContextScript, true)
	wantHost(t, hosts, "deferred.example.com", HostContextScript, true)
	wantHost(t, hosts, "module.example.com", HostContextScript, true)
	wantHost(t, hosts, "upper.example.com", HostContextScript, true)
	wantHost(t, hosts, "async.example.com", HostContextScript, false)
	wantHost(t, hosts, "module-async.example.com", HostContextScript, false)
	wantAbsent(t, hosts, "legacy.example.com", "template.example.com")
}

func TestScanPageReadsImagesAndMediaButNotNoscript(t *testing.T) {
	hosts := scanHosts(t, `<html><body>
<noscript><img src="https://noscript-only.example.com/a.png"></noscript>
<img src="https://img.example.com/a.png" srcset="https://srcset-a.example.com/a.png 1x,https://srcset-b.example.com/b.png 2x">
<img data-src="https://lazy.example.com/a.png" data-lazy-src="https://lazier.example.com/b.png" data-original="https://original.example.com/c.png">
<picture><source srcset="https://picture.example.com/a.webp"></picture>
<video poster="https://poster.example.com/a.jpg"><source src="https://video.example.com/a.mp4"></video>
<iframe src="https://frame.example.com/embed"><img src="https://inside-iframe.example.com/a.png"></iframe>
<input type="image" src="https://button.example.com/b.png">
<svg><image href="https://svg.example.com/a.png"></image></svg>
</body></html>`)

	for _, host := range []string{"img.example.com", "srcset-a.example.com", "srcset-b.example.com", "lazy.example.com", "lazier.example.com", "original.example.com", "picture.example.com", "button.example.com", "svg.example.com"} {
		wantHost(t, hosts, host, HostContextImage, false)
	}
	wantHost(t, hosts, "poster.example.com", HostContextMedia, false)
	wantHost(t, hosts, "video.example.com", HostContextMedia, false)
	wantHost(t, hosts, "frame.example.com", HostContextFrame, false)
	wantAbsent(t, hosts, "noscript-only.example.com", "inside-iframe.example.com")
}

func TestScanPageReadsCSSURLsAndImports(t *testing.T) {
	hosts := scanHosts(t, `<html><head><style>
@import url("https://import.example.com/a.css");
@import "https://import2.example.com/b.css";
.a { background: url(https://unquoted.example.com/a.png) }
.b { background: url("//quoted.example.com/b.png") }
.c { background: url('//single.example.com/c.png') }
</style></head><body>
<div style="background: url(&quot;https://attr.example.com/bg.png&quot;)"></div>
</body></html>`)

	wantHost(t, hosts, "import.example.com", HostContextStylesheet, true)
	wantHost(t, hosts, "import2.example.com", HostContextStylesheet, true)
	wantHost(t, hosts, "unquoted.example.com", HostContextCSS, false)
	wantHost(t, hosts, "quoted.example.com", HostContextCSS, false)
	wantHost(t, hosts, "single.example.com", HostContextCSS, false)
	attr := wantHost(t, hosts, "attr.example.com", HostContextCSS, false)
	if !reflect.DeepEqual(attr.URLs, []string{"https://attr.example.com/bg.png"}) {
		t.Errorf("entity-encoded style attribute sample %v", attr.URLs)
	}
}

func TestScanPageFoldsEscapedSlashesInInlineScripts(t *testing.T) {
	page := strings.ReplaceAll(`<html><body><script>
var a = "https:\/\/s3.example.net\/i\/a.png";
var b = "https:@@@@cdn.example.org@@lib.js";
var c = '//proto.example.com/a.js';
var d = `+"`https://cdn-template.example.com/${path}`"+`;
var e = "tcp:@@@@10.42.58.252:6379";
var f = "https://cdn-query.example.com/a.js?token=secret#x";
var g = "http:\x2F\x2Fhex.example.com/a.js";
var h = "http:@@@@gomdzstat:8081";
var i = "https://cdn-plus.example.com/" + name;
</script></body></html>`, "@@", jsSlash)
	hosts := scanHosts(t, page)

	samples := map[string]string{
		"s3.example.net":           "https://s3.example.net/i/a.png",
		"cdn.example.org":          "https://cdn.example.org/lib.js",
		"proto.example.com":        "https://proto.example.com/a.js",
		"cdn-template.example.com": "https://cdn-template.example.com/",
		"cdn-query.example.com":    "https://cdn-query.example.com/a.js",
		"hex.example.com":          "https://hex.example.com/a.js",
		"cdn-plus.example.com":     "https://cdn-plus.example.com/",
	}
	for host, sample := range samples {
		c := wantHost(t, hosts, host, HostContextInline, false)
		if len(c.URLs) > 0 && c.URLs[0] != sample {
			t.Errorf("%s: sample %v, want %s", host, c.URLs, sample)
		}
	}
	if len(hosts) != len(samples) {
		t.Errorf("hosts %v, want only %v: private addresses and single labels are not hosts", hosts, samples)
	}
}

func TestScanPageSkipsNavigationJSONCommentsAndText(t *testing.T) {
	hosts := scanHosts(t, `<html><head>
<title>https://title.example.com/</title>
<script type="application/ld+json">{"sameAs":["https://twitter.com/x","https://www.facebook.com/x"]}</script>
<script type="application/json">{"cdn":"https://json-data.example.com/a.js"}</script>
<!-- <script src="https://commented.example.com/a.js"></script> -->
<!--[if lt IE 9]><script src="https://ie.example.com/html5.js"></script><![endif]-->
</head><body>
<a href="https://www.instagram.com/x">insta</a>
<form action="https://form.example.com/submit"></form>
<meta content="https://meta.example.com/img.png" property="og:image">
<div data-href="https://data-nav.example.com/">https://text.example.com/</div>
<textarea>https://textarea.example.com/</textarea>
</body></html>`)

	if len(hosts) != 0 {
		t.Fatalf("navigation, data, comments and text name no subresource, got %v", hosts)
	}
}

func TestScanPageSkipsNamesThatOnlyIdentifySomething(t *testing.T) {
	hosts := scanHosts(t, `<html xmlns="http://www.w3.org/1999/xhtml" prefix="og: https://ogp.me/ns#"><head>
<link rel="profile" href="https://gmpg.org/xfn/11">
<style>@namespace svg url(http://www.w3.org/2000/svg);</style>
<script>
var icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
var meta = {"@context": "https://schema.org", "@type": "NewsArticle", "dc": "http://purl.org/dc/terms/", "foaf": "http://xmlns.com/foaf/0.1/"};
var shape = "http://ns.adobe.com/AdobeIllustrator/10.0/";
</script>
<script src="https://cdn.example.net/app.js"></script>
</head><body>
<img src="https://i.creativecommons.org/l/by/4.0/88x31.png">
</body></html>`)

	wantHost(t, hosts, "cdn.example.net", HostContextScript, true)
	wantHost(t, hosts, "i.creativecommons.org", HostContextImage, false)
	wantAbsent(t, hosts, "www.w3.org", "schema.org", "ogp.me", "purl.org", "xmlns.com", "ns.adobe.com", "gmpg.org")
	if len(hosts) != 2 {
		t.Errorf("hosts %v: namespaces, vocabularies and profiles name nothing the page loads", hosts)
	}
}

func TestScanPageHostRules(t *testing.T) {
	hosts := scanHosts(t, `<html><body>
<img src="&#x2F;&#x2F;entity.example.com/a.png">
<img src="https://UPPER.Example.COM./a.png">
<img src="https://port.example.com:8443/a.png">
<img src="https://default-port.example.com:443/a.png">
<img src="https://user:pw@userinfo.example.com/a.png">
<img src="https://xn--e1afmkfd.xn--p1ai/a.png">
<img src="https://пример.рф/a.png">
<img src="https://localhost/a.png">
<img src="https://printer.local/a.png">
<img src="https://10.0.0.1/a.png">
<img src="https://[2001:db8::1]/a.png">
<img src="https://bundle.min.js/a.png">
<img src="https://intranet/a.png">
<script>var v = "//1.2.3"; var w = 'https://user@sentry.example.com/1';</script>
</body></html>`)

	wantHost(t, hosts, "entity.example.com", HostContextImage, false)
	wantHost(t, hosts, "upper.example.com", HostContextImage, false)
	wantHost(t, hosts, "userinfo.example.com", HostContextImage, false)
	wantHost(t, hosts, "xn--e1afmkfd.xn--p1ai", HostContextImage, false)
	if c := wantHost(t, hosts, "port.example.com", HostContextImage, false); len(c.URLs) != 0 {
		t.Errorf("a set matches the name, not the port: the sample on port 8443 must be dropped, got %v", c.URLs)
	}
	if c := wantHost(t, hosts, "default-port.example.com", HostContextImage, false); !reflect.DeepEqual(c.URLs, []string{"https://default-port.example.com/a.png"}) {
		t.Errorf("the default port keeps its sample, got %v", c.URLs)
	}
	if len(hosts) != 6 {
		t.Errorf("hosts %v: Unicode names, local names, addresses, unknown suffixes and version strings are not hosts", hosts)
	}
}

func TestNormalizeHost(t *testing.T) {
	for raw, want := range map[string]string{
		"Example.COM.":                   "example.com",
		"a_b.example.com":                "a_b.example.com",
		"xn--e1afmkfd.xn--p1ai":          "xn--e1afmkfd.xn--p1ai",
		"cdn.blogspot.com":               "cdn.blogspot.com",
		"example":                        "",
		"localhost":                      "",
		"nas.local":                      "",
		"router.lan":                     "",
		"svc.internal":                   "",
		"box.home.arpa":                  "",
		"app.localhost":                  "",
		"10.0.0.1":                       "",
		"1.2.3":                          "",
		"foo.js":                         "",
		"bad host.com":                   "",
		"a..example.com":                 "",
		"пример.рф":                      "",
		strings.Repeat("a", 64) + ".com": "",
	} {
		got, ok := normalizeHost(raw)
		if (want != "") != ok || got != want {
			t.Errorf("normalizeHost(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
}

func TestScanPageKeepsBlockingTrackersOnly(t *testing.T) {
	scan := scanPage(strings.NewReader(`<html><head>
<base href="https://base.example.com/">
<script async src="https://www.google-analytics.com/analytics.js"></script>
<script defer src="https://static.cloudflareinsights.com/beacon.min.js"></script>
</head><body><img src="https://mc.yandex.ru/watch/1"></body></html>`))
	cands, trackers := scan.candidates()
	hosts := map[string]HostCandidate{}
	for _, c := range cands {
		hosts[c.Host] = c
	}

	wantHost(t, hosts, "base.example.com", HostContextBase, true)
	if beacon := wantHost(t, hosts, "static.cloudflareinsights.com", HostContextScript, true); !beacon.Tracker {
		t.Error("a tracker that holds the page is kept and marked as a tracker")
	}
	wantAbsent(t, hosts, "www.google-analytics.com", "mc.yandex.ru")
	if trackers != 2 {
		t.Errorf("trackers left out = %d, want the async script and the pixel", trackers)
	}
}

func TestScanPageKeepsThreeSamplesLargestFirst(t *testing.T) {
	hosts := scanHosts(t, `<html><head>
<link rel="icon" href="https://cdn.example.com/favicon.ico">
<link rel="preconnect" href="https://cdn.example.com">
<img src="https://cdn.example.com/a.png">
<link rel="stylesheet" href="https://cdn.example.com/a.css">
<script src="https://cdn.example.com/a.js"></script>
</head></html>`)

	c := wantHost(t, hosts, "cdn.example.com", HostContextStylesheet, true)
	want := []string{"https://cdn.example.com/a.js", "https://cdn.example.com/a.css", "https://cdn.example.com/a.png"}
	if !reflect.DeepEqual(c.URLs, want) {
		t.Errorf("samples %v, want script, stylesheet and image first", c.URLs)
	}
}

func TestScanPageNamesALoadedResourceBeforeTheBaseOrAFrame(t *testing.T) {
	hosts := scanHosts(t, `<html><head>
<base href="https://static.example.net/">
<script src="https://static.example.net/app.js"></script>
<link rel="preload" as="script" href="https://player.example.org/player.js">
</head><body>
<iframe src="https://player.example.org/embed/1"></iframe>
<iframe src="https://frame.example.org/embed/2"></iframe>
</body></html>`)

	wantHost(t, hosts, "static.example.net", HostContextScript, true)
	wantHost(t, hosts, "player.example.org", HostContextPreload, false)
	wantHost(t, hosts, "frame.example.org", HostContextFrame, false)
}

func TestScanPageDropsALongSample(t *testing.T) {
	long := "https://long.example.com/" + strings.Repeat("p", 2048) + ".js"
	hosts := scanHosts(t, `<html><head><script src="`+long+`"></script></head></html>`)

	if c := wantHost(t, hosts, "long.example.com", HostContextScript, true); len(c.URLs) != 0 {
		t.Errorf("a sample over %d bytes is not kept, got one of %d bytes", maxSampleURL, len(c.URLs[0]))
	}
}

func TestScanPageFindsEveryURLOfAHugeInlineScriptOnce(t *testing.T) {
	var script strings.Builder
	want := map[string]string{}
	for i := 0; script.Len() < 600<<10; i++ {
		host := fmt.Sprintf("h%d.example.com", i)
		sample := fmt.Sprintf("https://%s/%s.js", host, strings.Repeat("p", i*31%700))
		want[host] = sample
		script.WriteString(fmt.Sprintf(`{"u":"%s","pad":"%s"},`, strings.ReplaceAll(sample, "/", jsSlash), strings.Repeat("x", 1500+i*7919%6000)))
	}
	page := `<html><head><script>window.state = [` + script.String() + `];</script>
<script src="https://cdnjs.cloudflare.com/polyfill.min.js"></script>
<script defer="defer" src="https://static.cloudflareinsights.com/beacon.min.js"></script></head></html>`

	scan := scanPage(strings.NewReader(page))
	if scan.truncated {
		t.Fatalf("a %d KB page is read whole", len(page)>>10)
	}
	got := map[string]HostCandidate{}
	cands, _ := scan.candidates()
	for _, c := range cands {
		got[c.Host] = c
	}
	for host, sample := range want {
		c, ok := got[host]
		if !ok {
			t.Fatalf("%s is missing", host)
		}
		if !reflect.DeepEqual(c.URLs, []string{sample}) {
			t.Fatalf("%s samples %v, want only %s", host, c.URLs, sample)
		}
	}
	wantHost(t, got, "cdnjs.cloudflare.com", HostContextScript, true)
	wantHost(t, got, "static.cloudflareinsights.com", HostContextScript, true)
}

func TestScanPageGivesTheSameResultOneByteAtATime(t *testing.T) {
	pages := []string{
		`<html><head><link href="https://a.example.com/a.css" rel="stylesheet"><style>@import "https://b.example.com/b.css";</style>
<script>var x = "https:\/\/c.example.com\/x.js"; var y = '//d.example.com/y.png';</script>
<!-- <img src="https://e.example.com/"> --></head><body><img srcset="https://f.example.com/1.png 1x,https://g.example.com/2.png 2x"></body></html>`,
	}
	excerpts, _ := filepath.Glob(filepath.Join("testdata", "pagehosts", "*.html"))
	for _, path := range excerpts {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, string(data))
	}
	for i, page := range pages {
		whole := scanPage(strings.NewReader(page))
		bytewise := scanPage(iotest.OneByteReader(strings.NewReader(page)))
		a, _ := whole.candidates()
		b, _ := bytewise.candidates()
		if !reflect.DeepEqual(a, b) {
			t.Errorf("page %d: read whole %v, read a byte at a time %v", i, a, b)
		}
	}
}

func TestScanPageStopsAtTheReadCap(t *testing.T) {
	page := `<html><head><script src="https://early.example.com/a.js"></script></head><body><p>` +
		strings.Repeat("filler ", 3<<20/7) +
		`</p><script src="https://late.example.com/a.js"></script></body></html>`
	scan := scanPage(strings.NewReader(page))
	hosts := map[string]bool{}
	cands, _ := scan.candidates()
	for _, c := range cands {
		hosts[c.Host] = true
	}
	if !scan.truncated || scan.bytes != maxPageBytes {
		t.Fatalf("truncated = %v after %d bytes, want the read to stop at %d", scan.truncated, scan.bytes, maxPageBytes)
	}
	if !hosts["early.example.com"] || hosts["late.example.com"] {
		t.Fatalf("hosts %v, want the one before the cap only", hosts)
	}
}

func TestScanPageKeepsTheFirstHostsOnly(t *testing.T) {
	var page strings.Builder
	for i := 0; i < 300; i++ {
		page.WriteString(fmt.Sprintf(`<img src="https://h%d.example.com/a.png">`, i))
	}
	scan := scanPage(strings.NewReader(page.String()))
	if len(scan.hosts) != maxPageHosts || scan.overflow != 300-maxPageHosts {
		t.Fatalf("kept %d hosts with %d over, want %d and %d", len(scan.hosts), scan.overflow, maxPageHosts, 300-maxPageHosts)
	}
	if scan.hosts[0].name != "h0.example.com" || scan.hosts[maxPageHosts-1].name != fmt.Sprintf("h%d.example.com", maxPageHosts-1) {
		t.Fatalf("the hosts kept are the first ones seen, got %s .. %s", scan.hosts[0].name, scan.hosts[maxPageHosts-1].name)
	}
}

type fieldHost struct {
	host     string
	context  HostContext
	blocking bool
	tracker  bool
}

var fieldPages = map[string]struct {
	want   []fieldHost
	absent []string
}{
	"svoboda.html": {
		want: []fieldHost{
			{host: "docs.rferl.org", context: HostContextStylesheet, blocking: true},
			{host: "tags.svoboda.org", context: HostContextScript, blocking: true},
			{host: "www.youtube.com", context: HostContextScript},
			{host: "gdb.rferl.org", context: HostContextImage},
			{host: "cdn.onesignal.com", context: HostContextScript, blocking: true, tracker: true},
		},
		absent: []string{"twitter.com", "www.facebook.com", "www.instagram.com"},
	},
	"meduza.html": {
		want: []fieldHost{
			{host: "cdnjs.cloudflare.com", context: HostContextScript, blocking: true},
			{host: "static.cloudflareinsights.com", context: HostContextScript, blocking: true, tracker: true},
			{host: "t.me", context: HostContextInline},
			{host: "instagram.com", context: HostContextInline},
			{host: "magaz.global", context: HostContextInline},
		},
		absent: []string{"www.googletagmanager.com", "gomdzstat"},
	},
	"zona.html": {
		want: []fieldHost{
			{host: "s3.zona.media", context: HostContextImage},
			{host: "rsm.zona.media", context: HostContextInline},
		},
	},
	"soundcloud.html": {
		want: []fieldHost{
			{host: "i1.sndcdn.com", context: HostContextPreconnect},
		},
	},
	"holod.html": {
		want: []fieldHost{
			{host: "storage.ghost.io", context: HostContextImage},
			{host: "cdn.jsdelivr.net", context: HostContextScript, blocking: true},
		},
	},
}

func TestScanPageFieldPages(t *testing.T) {
	dirs := []string{filepath.Join("testdata", "pagehosts")}
	if dir := os.Getenv("B4_PAGEHOSTS_FIXTURES"); dir != "" {
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		for name, expect := range fieldPages {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			hosts := scanHosts(t, string(data))
			for _, w := range expect.want {
				c := wantHost(t, hosts, w.host, w.context, w.blocking)
				if c.Tracker != w.tracker {
					t.Errorf("%s/%s: %s tracker = %v, want %v", dir, name, w.host, c.Tracker, w.tracker)
				}
			}
			wantAbsent(t, hosts, expect.absent...)
			for host := range hosts {
				if strings.HasPrefix(host, "10.") {
					t.Errorf("%s/%s: a private address is never a host, got %s", dir, name, host)
				}
			}
			if a, ok := hosts["a-v2.sndcdn.com"]; name == "soundcloud.html" && (!ok || !a.Blocking) {
				t.Errorf("%s/%s: a-v2.sndcdn.com is loaded by a parser-blocking script, got %+v", dir, name, a)
			}
		}
	}
}
