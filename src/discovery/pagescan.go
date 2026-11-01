package discovery

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/daniellavrushin/b4/utils"
	"golang.org/x/net/publicsuffix"
)

//go:embed trackers.json
var trackersJSON []byte

var (
	trackerSuffixes []string
	trackersOnce    sync.Once
)

func isTracker(host string) bool {
	trackersOnce.Do(func() {
		if err := json.Unmarshal(trackersJSON, &trackerSuffixes); err != nil {
			trackerSuffixes = nil
		}
	})
	for _, suffix := range trackerSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

var identifierHosts = []string{
	"creativecommons.org",
	"data-vocabulary.org",
	"gmpg.org",
	"inkscape.sourceforge.net",
	"iptc.org",
	"json-schema.org",
	"microformats.org",
	"ns.adobe.com",
	"ogp.me",
	"opengraphprotocol.org",
	"purl.org",
	"rdf.data-vocabulary.org",
	"rdfs.org",
	"schema.org",
	"sodipodi.sourceforge.net",
	"w3.org",
	"web.resource.org",
	"www.bohemiancoding.com",
	"www.data-vocabulary.org",
	"www.inkscape.org",
	"www.opengraphprotocol.org",
	"www.schema.org",
	"www.serif.com",
	"www.w3.org",
	"xmlns.com",
}

func isIdentifierHost(host string) bool {
	return slices.Contains(identifierHosts, host)
}

const (
	maxPageBytes   = 2 << 20
	maxPageHosts   = 256
	maxHostSamples = 3
	maxSampleURL   = 1024
	maxTagName     = 16
	maxAttrName    = 32
	maxAttrValue   = 8 << 10
	maxTagAttrs    = 64
	rawTextChunk   = 16 << 10
	rawTextCarry   = 1536
	rawLookbehind  = 64
)

type pageScan struct {
	hosts     []*scannedHost
	byName    map[string]*scannedHost
	bytes     int64
	truncated bool
	err       error
	overflow  int
}

type scannedHost struct {
	name     string
	contexts uint32
	blocking bool
	samples  []scannedSample
}

type scannedSample struct {
	url  string
	rank int
}

func (p *pageScan) add(host string, ctx HostContext, blocking bool, sample string) {
	if isIdentifierHost(host) {
		return
	}
	h := p.byName[host]
	if h == nil {
		if len(p.hosts) >= maxPageHosts {
			p.overflow++
			return
		}
		h = &scannedHost{name: host}
		p.byName[host] = h
		p.hosts = append(p.hosts, h)
	}
	h.contexts |= 1 << hostContextRank(ctx)
	h.blocking = h.blocking || blocking
	if sample != "" {
		h.addSample(sample, sampleRank(ctx))
	}
}

func (h *scannedHost) addSample(url string, rank int) {
	for _, s := range h.samples {
		if s.url == url {
			return
		}
	}
	at := len(h.samples)
	for i, s := range h.samples {
		if rank < s.rank {
			at = i
			break
		}
	}
	if at >= maxHostSamples {
		return
	}
	h.samples = slices.Insert(h.samples, at, scannedSample{url: url, rank: rank})
	if len(h.samples) > maxHostSamples {
		h.samples = h.samples[:maxHostSamples]
	}
}

func (h *scannedHost) context() HostContext {
	for i, c := range hostContextOrder {
		if h.contexts&(1<<i) != 0 {
			return c
		}
	}
	return ""
}

func sampleRank(ctx HostContext) int {
	switch ctx {
	case HostContextScript:
		return 0
	case HostContextStylesheet, HostContextRedirect:
		return 1
	case HostContextImage, HostContextMedia, HostContextCSS:
		return 2
	case HostContextFrame, HostContextPreload, HostContextInline:
		return 3
	case HostContextIcon:
		return 4
	}
	return 5
}

func (p *pageScan) candidates() ([]HostCandidate, int) {
	var out []HostCandidate
	trackers := 0
	for _, h := range p.hosts {
		tracker := isTracker(h.name)
		if tracker && !h.blocking {
			trackers++
			continue
		}
		c := HostCandidate{
			Host:     h.name,
			Source:   HostFromPage,
			Context:  h.context(),
			Blocking: h.blocking,
			Tracker:  tracker,
		}
		for _, s := range h.samples {
			c.URLs = append(c.URLs, s.url)
		}
		out = append(out, c)
	}
	return out, trackers
}

func scanPage(r io.Reader) pageScan {
	s := newPageScanner()
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			take := n
			if s.out.bytes+int64(n) > maxPageBytes {
				take = int(maxPageBytes - s.out.bytes)
				s.out.truncated = true
			}
			s.feed(buf[:take])
			s.out.bytes += int64(take)
			if s.out.truncated {
				break
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			s.out.err = err
			break
		}
	}
	s.finish()
	return s.out
}

type scanState uint8

const (
	scanData scanState = iota
	scanTagOpen
	scanEndTagOpen
	scanTagName
	scanBeforeAttr
	scanAttrName
	scanAfterAttrName
	scanBeforeValue
	scanValueDouble
	scanValueSingle
	scanValueBare
	scanBang
	scanBangDash
	scanComment
	scanBogus
	scanRaw
)

type rawKind uint8

const (
	rawSkip rawKind = iota
	rawScript
	rawStyle
)

type pageScanner struct {
	out         pageScan
	state       scanState
	endTag      bool
	name        []byte
	nameLong    bool
	selfClosing bool
	attrs       map[string]string
	attrCount   int
	attrName    []byte
	attrLong    bool
	value       []byte
	dashes      int
	rawEnd      string
	rawMatch    int
	rawKind     rawKind
	text        []byte
	textDone    int
	fold        []byte
}

func newPageScanner() *pageScanner {
	return &pageScanner{
		out:   pageScan{byName: map[string]*scannedHost{}},
		attrs: map[string]string{},
		text:  make([]byte, 0, rawTextChunk+rawTextCarry),
	}
}

func (s *pageScanner) feed(data []byte) {
	for i := 0; i < len(data); i++ {
		switch s.state {
		case scanData:
			j := bytes.IndexByte(data[i:], '<')
			if j < 0 {
				return
			}
			i += j
			s.state = scanTagOpen
			continue
		case scanValueDouble, scanValueSingle:
			quote := byte('"')
			if s.state == scanValueSingle {
				quote = '\''
			}
			j := bytes.IndexByte(data[i:], quote)
			if j < 0 {
				s.appendValue(data[i:])
				return
			}
			s.appendValue(data[i : i+j])
			i += j
			s.commitAttr()
			s.state = scanBeforeAttr
			continue
		case scanBogus:
			j := bytes.IndexByte(data[i:], '>')
			if j < 0 {
				return
			}
			i += j
			s.state = scanData
			continue
		case scanRaw:
			if s.rawKind == rawSkip && s.rawMatch == 0 {
				j := bytes.IndexByte(data[i:], '<')
				if j < 0 {
					return
				}
				i += j
			}
			s.rawByte(data[i])
			continue
		}
		s.step(data[i])
	}
}

func (s *pageScanner) step(c byte) {
	switch s.state {
	case scanTagOpen:
		switch {
		case c == '!':
			s.state = scanBang
		case c == '/':
			s.state = scanEndTagOpen
		case c == '?':
			s.state = scanBogus
		case c == '<':
		case isASCIILetter(c):
			s.startTag(false, c)
		default:
			s.state = scanData
		}
	case scanEndTagOpen:
		switch {
		case isASCIILetter(c):
			s.startTag(true, c)
		case c == '>':
			s.state = scanData
		default:
			s.state = scanBogus
		}
	case scanTagName:
		switch {
		case isHTMLSpace(c):
			s.state = scanBeforeAttr
		case c == '/':
			s.selfClosing = true
			s.state = scanBeforeAttr
		case c == '>':
			s.emitTag()
		default:
			if len(s.name) < maxTagName {
				s.name = append(s.name, lowerASCII(c))
			} else {
				s.nameLong = true
			}
		}
	case scanBeforeAttr:
		switch {
		case isHTMLSpace(c):
		case c == '/':
			s.selfClosing = true
		case c == '>':
			s.emitTag()
		default:
			s.startAttr(c)
		}
	case scanAttrName:
		switch {
		case isHTMLSpace(c):
			s.state = scanAfterAttrName
		case c == '/':
			s.commitAttr()
			s.selfClosing = true
			s.state = scanBeforeAttr
		case c == '=':
			s.state = scanBeforeValue
		case c == '>':
			s.commitAttr()
			s.emitTag()
		default:
			if len(s.attrName) < maxAttrName {
				s.attrName = append(s.attrName, lowerASCII(c))
			} else {
				s.attrLong = true
			}
		}
	case scanAfterAttrName:
		switch {
		case isHTMLSpace(c):
		case c == '=':
			s.state = scanBeforeValue
		case c == '/':
			s.commitAttr()
			s.selfClosing = true
			s.state = scanBeforeAttr
		case c == '>':
			s.commitAttr()
			s.emitTag()
		default:
			s.commitAttr()
			s.startAttr(c)
		}
	case scanBeforeValue:
		switch {
		case isHTMLSpace(c):
		case c == '"':
			s.state = scanValueDouble
		case c == '\'':
			s.state = scanValueSingle
		case c == '>':
			s.commitAttr()
			s.emitTag()
		default:
			s.appendValue([]byte{c})
			s.state = scanValueBare
		}
	case scanValueBare:
		switch {
		case isHTMLSpace(c):
			s.commitAttr()
			s.state = scanBeforeAttr
		case c == '>':
			s.commitAttr()
			s.emitTag()
		default:
			s.appendValue([]byte{c})
		}
	case scanBang:
		switch c {
		case '-':
			s.state = scanBangDash
		case '>':
			s.state = scanData
		default:
			s.state = scanBogus
		}
	case scanBangDash:
		switch c {
		case '-':
			s.dashes = 2
			s.state = scanComment
		case '>':
			s.state = scanData
		default:
			s.state = scanBogus
		}
	case scanComment:
		switch c {
		case '-':
			s.dashes++
		case '>':
			if s.dashes >= 2 {
				s.state = scanData
			}
			s.dashes = 0
		default:
			s.dashes = 0
		}
	}
}

func (s *pageScanner) startTag(end bool, first byte) {
	s.state = scanTagName
	s.endTag = end
	s.name = append(s.name[:0], lowerASCII(first))
	s.nameLong = false
	s.selfClosing = false
	clear(s.attrs)
	s.attrCount = 0
}

func (s *pageScanner) startAttr(first byte) {
	s.state = scanAttrName
	s.selfClosing = false
	s.attrName = append(s.attrName[:0], lowerASCII(first))
	s.attrLong = false
	s.value = s.value[:0]
}

func (s *pageScanner) appendValue(b []byte) {
	if room := maxAttrValue - len(s.value); room > 0 {
		s.value = append(s.value, b[:min(len(b), room)]...)
	}
}

func (s *pageScanner) commitAttr() {
	defer func() {
		s.attrName = s.attrName[:0]
		s.value = s.value[:0]
	}()
	if s.endTag || s.attrLong || len(s.attrName) == 0 || s.attrCount >= maxTagAttrs {
		return
	}
	s.attrCount++
	if !wantedAttr(s.attrName) {
		return
	}
	name := string(s.attrName)
	if _, seen := s.attrs[name]; seen {
		return
	}
	s.attrs[name] = decodeEntities(s.value)
}

func wantedAttr(name []byte) bool {
	switch string(name) {
	case "src", "srcset", "href", "rel", "type", "async", "nomodule", "style", "poster", "data",
		"imagesrcset", "xlink:href", "data-src", "data-srcset", "data-lazy-src", "data-original":
		return true
	}
	return false
}

func (s *pageScanner) has(name string) bool {
	_, ok := s.attrs[name]
	return ok
}

func (s *pageScanner) emitTag() {
	s.state = scanData
	if s.endTag {
		return
	}
	if style, ok := s.attrs["style"]; ok && style != "" {
		text := []byte(style)
		s.textURLs(text, 0, len(text), true, rawStyle)
	}
	name := ""
	if !s.nameLong {
		name = string(s.name)
	}
	switch name {
	case "link":
		if ctx, blocking := linkContext(s.attrs["rel"]); ctx != "" {
			s.attrURL(s.attrs["href"], ctx, blocking)
			if ctx == HostContextPreload {
				s.srcset(s.attrs["imagesrcset"], ctx)
			}
		}
	case "script":
		kind := rawScript
		if src, ok := s.attrs["src"]; ok {
			kind = rawSkip
			if scriptRuns(s.attrs["type"]) && !s.has("nomodule") {
				s.attrURL(src, HostContextScript, !s.has("async"))
			}
		} else if jsonScript(s.attrs["type"]) {
			kind = rawSkip
		}
		s.enterRaw(name, kind)
	case "style":
		s.enterRaw(name, rawStyle)
	case "img", "image":
		s.imageAttrs()
		s.attrURL(s.attrs["href"], HostContextImage, false)
		s.attrURL(s.attrs["xlink:href"], HostContextImage, false)
	case "input":
		if strings.EqualFold(strings.TrimSpace(s.attrs["type"]), "image") {
			s.attrURL(s.attrs["src"], HostContextImage, false)
		}
	case "source":
		s.srcset(s.attrs["srcset"], HostContextImage)
		s.srcset(s.attrs["data-srcset"], HostContextImage)
		s.attrURL(s.attrs["src"], HostContextMedia, false)
	case "video", "audio", "track":
		s.attrURL(s.attrs["src"], HostContextMedia, false)
		s.attrURL(s.attrs["poster"], HostContextMedia, false)
	case "iframe":
		s.attrURL(s.attrs["src"], HostContextFrame, false)
		s.enterRaw(name, rawSkip)
	case "frame", "embed":
		s.attrURL(s.attrs["src"], HostContextFrame, false)
	case "object":
		s.attrURL(s.attrs["data"], HostContextFrame, false)
	case "use":
		s.attrURL(s.attrs["href"], HostContextImage, false)
		s.attrURL(s.attrs["xlink:href"], HostContextImage, false)
	case "base":
		s.attrURL(s.attrs["href"], HostContextBase, true)
	case "noscript", "noembed", "noframes", "textarea", "title", "xmp":
		s.enterRaw(name, rawSkip)
	}
}

func (s *pageScanner) imageAttrs() {
	for _, name := range []string{"src", "data-src", "data-lazy-src", "data-original"} {
		s.attrURL(s.attrs[name], HostContextImage, false)
	}
	s.srcset(s.attrs["srcset"], HostContextImage)
	s.srcset(s.attrs["data-srcset"], HostContextImage)
}

func linkContext(rel string) (HostContext, bool) {
	var stylesheet, alternate, preload, preconnect, icon bool
	for _, token := range strings.Fields(strings.ToLower(rel)) {
		switch token {
		case "stylesheet":
			stylesheet = true
		case "alternate":
			alternate = true
		case "preload", "modulepreload", "prefetch":
			preload = true
		case "preconnect", "dns-prefetch":
			preconnect = true
		case "icon", "apple-touch-icon", "apple-touch-icon-precomposed", "mask-icon", "manifest":
			icon = true
		}
	}
	switch {
	case stylesheet && !alternate:
		return HostContextStylesheet, true
	case preload:
		return HostContextPreload, false
	case icon:
		return HostContextIcon, false
	case preconnect:
		return HostContextPreconnect, false
	}
	return "", false
}

func scriptRuns(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	return kind == "" || kind == "module" || strings.Contains(kind, "javascript") || strings.Contains(kind, "ecmascript") || strings.Contains(kind, "jscript")
}

func jsonScript(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if i := strings.IndexByte(kind, ';'); i >= 0 {
		kind = strings.TrimSpace(kind[:i])
	}
	return kind == "application/json" || strings.HasSuffix(kind, "+json")
}

func (s *pageScanner) enterRaw(name string, kind rawKind) {
	if s.selfClosing {
		return
	}
	s.state = scanRaw
	s.rawEnd = "</" + name
	s.rawMatch = 0
	s.rawKind = kind
	s.text = s.text[:0]
	s.textDone = 0
	s.fold = s.fold[:0]
}

func (s *pageScanner) rawByte(c byte) {
	if s.rawMatch == len(s.rawEnd) {
		if isHTMLSpace(c) || c == '/' || c == '>' {
			s.endRaw()
			if c == '>' {
				s.state = scanData
			} else {
				s.state = scanBogus
			}
			return
		}
		s.rawMatch = 0
	}
	switch {
	case lowerASCII(c) == s.rawEnd[s.rawMatch]:
		s.rawMatch++
	case c == '<':
		s.rawMatch = 1
	default:
		s.rawMatch = 0
	}
	if s.rawKind != rawSkip {
		s.foldByte(c)
	}
}

func (s *pageScanner) endRaw() {
	if s.rawKind != rawSkip {
		for _, b := range s.fold {
			s.pushText(b)
		}
		s.fold = s.fold[:0]
		s.flushText(true)
	}
	s.rawKind = rawSkip
}

func (s *pageScanner) foldByte(c byte) {
	if len(s.fold) == 0 {
		if c == '\\' {
			s.fold = append(s.fold, c)
			return
		}
		s.pushText(c)
		return
	}
	s.fold = append(s.fold, c)
	done, more := foldEscape(s.fold)
	switch {
	case done:
		s.fold = s.fold[:0]
		s.pushText('/')
	case more:
	default:
		last := s.fold[len(s.fold)-1]
		for _, b := range s.fold[:len(s.fold)-1] {
			s.pushText(b)
		}
		s.fold = s.fold[:0]
		s.foldByte(last)
	}
}

var slashEscapes = []string{`\/`, `\` + "u002f", `\x2f`}

func foldEscape(pending []byte) (done, more bool) {
	for _, escape := range slashEscapes {
		if len(pending) > len(escape) {
			continue
		}
		if !strings.EqualFold(string(pending), escape[:len(pending)]) {
			continue
		}
		if len(pending) == len(escape) {
			return true, false
		}
		more = true
	}
	return false, more
}

func (s *pageScanner) pushText(c byte) {
	s.text = append(s.text, c)
	if len(s.text) >= rawTextChunk+rawTextCarry {
		s.flushText(false)
	}
}

func (s *pageScanner) flushText(final bool) {
	limit := len(s.text)
	if !final {
		limit -= rawTextCarry
	}
	if limit > s.textDone {
		s.textURLs(s.text, s.textDone, limit, final, s.rawKind)
	}
	if final {
		s.text = s.text[:0]
		s.textDone = 0
		return
	}
	from := max(limit-rawLookbehind, 0)
	n := copy(s.text, s.text[from:])
	s.text = s.text[:n]
	s.textDone = limit - from
}

func (s *pageScanner) finish() {
	if s.state == scanRaw {
		s.endRaw()
	}
}

var (
	doubleSlash    = []byte("//")
	httpsScheme    = []byte("https:")
	httpScheme     = []byte("http:")
	templateMarks  = [][]byte{[]byte("${"), []byte("{{"), []byte("+")}
	importKeyword  = []byte("@import")
	urlFunction    = []byte("url(")
	sampleTailHead = "/?#"
)

func (s *pageScanner) textURLs(buf []byte, from, limit int, final bool, kind rawKind) {
	for i := from; i < limit; {
		j := bytes.Index(buf[i:], doubleSlash)
		if j < 0 {
			return
		}
		at := i + j
		if at >= limit {
			return
		}
		i = at + 2
		start, ok := urlStart(buf, at)
		if !ok {
			continue
		}
		hostEnd := at + 2
		for hostEnd < len(buf) && isHostByte(buf[hostEnd]) {
			hostEnd++
		}
		if (hostEnd == len(buf) && !final) || (hostEnd < len(buf) && !endsHost(buf[hostEnd])) {
			continue
		}
		host, ok := normalizeHost(string(buf[at+2 : hostEnd]))
		if !ok {
			continue
		}
		authEnd, port := hostEnd, ""
		if hostEnd < len(buf) && buf[hostEnd] == ':' {
			authEnd++
			for authEnd < len(buf) && buf[authEnd] >= '0' && buf[authEnd] <= '9' {
				authEnd++
			}
			port = string(buf[hostEnd+1 : authEnd])
		}
		runEnd := authEnd
		for runEnd < len(buf) && !endsSample(buf[runEnd]) {
			runEnd++
		}
		ctx, blocking := HostContextInline, false
		if kind == rawStyle {
			ctx = HostContextCSS
			if importBefore(buf, start) {
				ctx, blocking = HostContextStylesheet, true
			}
		}
		s.out.add(host, ctx, blocking, textSample(buf, at, authEnd, runEnd, final, host, port, ctx == HostContextInline))
	}
}

func textSample(buf []byte, at, authEnd, runEnd int, final bool, host, port string, inline bool) string {
	if runEnd == len(buf) && !final {
		return ""
	}
	for _, mark := range templateMarks {
		if bytes.Contains(buf[at:runEnd], mark) {
			return ""
		}
	}
	tail := string(buf[authEnd:runEnd])
	if tail != "" && !strings.ContainsRune(sampleTailHead, rune(tail[0])) {
		return ""
	}
	return sampleURL(host, port, tail, inline)
}

func urlStart(buf []byte, at int) (int, bool) {
	switch {
	case at >= 6 && bytes.EqualFold(buf[at-6:at], httpsScheme):
		return at - 6, true
	case at >= 5 && bytes.EqualFold(buf[at-5:at], httpScheme):
		return at - 5, true
	case at == 0:
		return at, true
	}
	switch c := buf[at-1]; c {
	case '"', '\'', '`', '(', '=', ',':
		return at, true
	default:
		return at, isHTMLSpace(c)
	}
}

func endsHost(c byte) bool {
	switch c {
	case '/', ':', '"', '\'', '`', '\\', '?', '#', ')', ',', ';', '&', '<', '>':
		return true
	}
	return isHTMLSpace(c)
}

func endsSample(c byte) bool {
	switch c {
	case '"', '\'', '`', '\\', ')', '<', '>':
		return true
	}
	return isHTMLSpace(c)
}

func importBefore(buf []byte, start int) bool {
	i := start
	skip := func() {
		for i > 0 && isHTMLSpace(buf[i-1]) {
			i--
		}
	}
	skip()
	if i > 0 && (buf[i-1] == '"' || buf[i-1] == '\'') {
		i--
		skip()
	}
	if i >= len(urlFunction) && bytes.EqualFold(buf[i-len(urlFunction):i], urlFunction) {
		i -= len(urlFunction)
		skip()
	}
	return i >= len(importKeyword) && bytes.EqualFold(buf[i-len(importKeyword):i], importKeyword)
}

func (s *pageScanner) srcset(value string, ctx HostContext) {
	if value == "" {
		return
	}
	for _, part := range strings.Split(value, ",") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			s.attrURL(fields[0], ctx, false)
		}
	}
}

func (s *pageScanner) attrURL(value string, ctx HostContext, blocking bool) {
	host, sample, ok := absoluteURL(value)
	if ok {
		s.out.add(host, ctx, blocking, sample)
	}
}

func absoluteURL(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	var rest string
	switch {
	case len(value) >= 8 && strings.EqualFold(value[:8], "https://"):
		rest = value[8:]
	case len(value) >= 7 && strings.EqualFold(value[:7], "http://"):
		rest = value[7:]
	case strings.HasPrefix(value, "//"):
		rest = value[2:]
	default:
		return "", "", false
	}
	end := strings.IndexAny(rest, "/?#\\")
	if end < 0 {
		end = len(rest)
	}
	authority, tail := rest[:end], rest[end:]
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		authority = authority[at+1:]
	}
	name, port := authority, ""
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 && !strings.Contains(authority[colon:], "]") {
		name, port = authority[:colon], authority[colon+1:]
	}
	host, ok := normalizeHost(name)
	if !ok {
		return "", "", false
	}
	return host, sampleURL(host, port, tail, false), true
}

func sampleURL(host, port, tail string, inline bool) string {
	if port != "" && port != "443" && port != "80" {
		return ""
	}
	if inline {
		if cut := strings.IndexAny(tail, "?#"); cut >= 0 {
			tail = tail[:cut]
		}
	}
	raw := "https://" + host + tail
	if len(raw) > maxSampleURL {
		return ""
	}
	canonical, _, err := utils.NormalizeProbeURL(raw)
	if err != nil {
		return ""
	}
	return canonical
}

var localSuffixes = []string{".local", ".lan", ".internal", ".home.arpa", ".localhost"}

func normalizeHost(raw string) (string, bool) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if host == "" || len(host) > 253 || host == "localhost" {
		return "", false
	}
	for _, suffix := range localSuffixes {
		if strings.HasSuffix(host, suffix) {
			return "", false
		}
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return "", false
		}
		for i := 0; i < len(label); i++ {
			if !isHostByte(label[i]) {
				return "", false
			}
		}
	}
	last := labels[len(labels)-1]
	if !strings.HasPrefix(last, "xn--") && (len(last) < 2 || !allLetters(last)) {
		return "", false
	}
	if suffix, icann := publicsuffix.PublicSuffix(host); !icann && !strings.Contains(suffix, ".") {
		return "", false
	}
	return host, true
}

func allLetters(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isASCIILetter(s[i]) {
			return false
		}
	}
	return true
}

func decodeEntities(b []byte) string {
	if bytes.IndexByte(b, '&') < 0 {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		if b[i] == '&' {
			if semi := bytes.IndexByte(b[i:min(len(b), i+12)], ';'); semi > 1 {
				if r, ok := entityRune(b[i+1 : i+semi]); ok {
					out.WriteRune(r)
					i += semi + 1
					continue
				}
			}
		}
		out.WriteByte(b[i])
		i++
	}
	return out.String()
}

func entityRune(name []byte) (rune, bool) {
	switch string(name) {
	case "amp":
		return '&', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "sol":
		return '/', true
	case "colon":
		return ':', true
	}
	if len(name) < 2 || name[0] != '#' {
		return 0, false
	}
	digits, base := string(name[1:]), 10
	if digits[0] == 'x' || digits[0] == 'X' {
		digits, base = digits[1:], 16
	}
	n, err := strconv.ParseUint(digits, base, 32)
	if err != nil || n == 0 || n > utf8.MaxRune {
		return 0, false
	}
	return rune(n), true
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isHostByte(c byte) bool {
	return isASCIILetter(c) || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
