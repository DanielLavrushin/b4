package iphealth

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
)

const (
	BanFileName = "ip_bans.json"

	banFileVersion  = 1
	banFileMode     = 0644
	banMemory       = 30 * 24 * time.Hour
	maxBans         = 1024
	banPassesToLift = 3
	banBurstLimit   = 32
	banBurstWindow  = 10 * time.Minute
)

type Ban struct {
	IP      string    `json:"ip"`
	Port    int       `json:"port"`
	Host    string    `json:"host,omitempty"`
	Set     string    `json:"set,omitempty"`
	Strikes int       `json:"strikes"`
	Repeat  bool      `json:"repeat"`
	Since   time.Time `json:"since"`
	Until   time.Time `json:"until"`
	Passes  int       `json:"passes"`
	Checked time.Time `json:"checked,omitzero"`
}

func (b *Ban) Key() string {
	return net.JoinHostPort(b.IP, strconv.Itoa(b.Port))
}

func (b *Ban) active(now time.Time) bool {
	return now.Before(b.Until)
}

func (b *Ban) remembered(now time.Time) bool {
	return now.Before(b.Until.Add(banMemory))
}

func BanKey(ip net.IP, port int) string {
	return net.JoinHostPort(ip.String(), strconv.Itoa(port))
}

func ParseBanKey(key string) (net.IP, int, bool) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(key))
	if err != nil {
		return nil, 0, false
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(portStr)
	if ip == nil || err != nil || port <= 0 || port > 65535 {
		return nil, 0, false
	}
	return ip, port, true
}

func BanFilePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), BanFileName)
}

type banFile struct {
	Version int    `json:"version"`
	Bans    []*Ban `json:"bans"`
}

type BanRegistry struct {
	mu          sync.Mutex
	flushMu     sync.Mutex
	path        string
	byIP        map[string][]*Ban
	count       atomic.Int32
	dirty       bool
	burst       []time.Time
	burstWarned time.Time
	now         func() time.Time
}

func NewBanRegistry(path string) *BanRegistry {
	return &BanRegistry{path: path, byIP: make(map[string][]*Ban), now: time.Now}
}

func (r *BanRegistry) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

func (r *BanRegistry) Len() int {
	if r == nil {
		return 0
	}
	return int(r.count.Load())
}

func (r *BanRegistry) Load() error {
	if r == nil || r.path == "" {
		return nil
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file banFile
	if err := json.Unmarshal(raw, &file); err != nil {
		kept, moveErr := config.MoveCorruptAside(r.path)
		if moveErr != nil {
			log.Errorf("IP bans: %s is not valid JSON (%v) and could not be moved aside (%v); starting with no bans", r.path, err, moveErr)
			return nil
		}
		log.Errorf("IP bans: %s is not valid JSON (%v); it was moved to %s and b4 starts with no bans", r.path, err, kept)
		return nil
	}

	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.byIP)
	r.count.Store(0)
	changed := false
	for _, b := range file.Bans {
		rec, ok := sanitizeBan(b)
		if !ok || !rec.remembered(now) {
			changed = true
			continue
		}
		if prev := r.findLocked(rec.IP, rec.Port); prev != nil {
			changed = true
			if !rec.Until.After(prev.Until) {
				continue
			}
			r.deleteLocked(prev)
		}
		r.makeRoomLocked(now)
		r.insertLocked(rec)
	}
	r.dirty = changed || len(file.Bans) != r.Len()
	return nil
}

func sanitizeBan(b *Ban) (*Ban, bool) {
	if b == nil || b.Until.IsZero() || b.Port <= 0 || b.Port > 65535 {
		return nil, false
	}
	ip := net.ParseIP(strings.TrimSpace(b.IP))
	if ip == nil {
		return nil, false
	}
	rec := *b
	rec.IP = ip.String()
	if rec.Strikes < 1 {
		rec.Strikes = 1
	}
	if rec.Passes < 0 || rec.Passes >= banPassesToLift {
		rec.Passes = 0
	}
	if rec.Since.IsZero() || rec.Since.After(rec.Until) {
		rec.Since = rec.Until
	}
	return &rec, true
}

func (r *BanRegistry) Strike(ip net.IP, port int, host, set string, first, repeat time.Duration) (time.Time, bool, bool) {
	if r == nil || ip == nil || port <= 0 || port > 65535 {
		return time.Time{}, false, false
	}
	now := r.now()
	r.mu.Lock()
	until, isRepeat, ok, warn := r.strikeLocked(now, ip.String(), port, host, set, first, repeat)
	r.mu.Unlock()
	if warn {
		log.Warnf("IP bans: %d addresses were banned within %s, which reads as an uplink or resolver problem rather than per-address blocks; no more addresses are banned until the rate drops", banBurstLimit, banBurstWindow)
	}
	return until, isRepeat, ok
}

func (r *BanRegistry) strikeLocked(now time.Time, addr string, port int, host, set string, first, repeat time.Duration) (time.Time, bool, bool, bool) {
	b := r.findLocked(addr, port)
	if b != nil && b.active(now) {
		tier := first
		if b.Repeat {
			tier = repeat
		}
		if next := now.Add(tier); next.After(b.Until) {
			b.Until = next
		}
		b.Strikes++
		b.Passes = 0
		b.label(host, set)
		r.dirty = true
		return b.Until, b.Repeat, true, false
	}

	if allowed, warn := r.allowBanLocked(now); !allowed {
		return time.Time{}, false, false, warn
	}

	isRepeat := b != nil && b.remembered(now)
	if b == nil {
		r.makeRoomLocked(now)
		b = &Ban{IP: addr, Port: port}
		r.insertLocked(b)
	}
	if !isRepeat {
		b.Strikes = 0
	}
	ttl := first
	if isRepeat {
		ttl = repeat
	}
	b.Strikes++
	b.Repeat = isRepeat
	b.Since = now
	b.Until = now.Add(ttl)
	b.Passes = 0
	b.Checked = now
	b.label(host, set)
	r.dirty = true
	return b.Until, isRepeat, true, false
}

func (b *Ban) label(host, set string) {
	if host != "" {
		b.Host = host
	}
	if set != "" {
		b.Set = set
	}
}

func (r *BanRegistry) allowBanLocked(now time.Time) (bool, bool) {
	cutoff := now.Add(-banBurstWindow)
	live := r.burst[:0]
	for _, at := range r.burst {
		if at.After(cutoff) {
			live = append(live, at)
		}
	}
	r.burst = live
	if len(r.burst) >= banBurstLimit {
		if !r.burstWarned.IsZero() && now.Sub(r.burstWarned) < banBurstWindow {
			return false, false
		}
		r.burstWarned = now
		return false, true
	}
	r.burst = append(r.burst, now)
	return true, false
}

func (r *BanRegistry) Banned(ip net.IP, port int) bool {
	if r == nil || ip == nil || r.count.Load() == 0 {
		return false
	}
	now := r.now()
	addr := ip.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.findLocked(addr, port)
	return b != nil && b.active(now)
}

func (r *BanRegistry) BannedIP(ip net.IP) bool {
	if r == nil || ip == nil || r.count.Load() == 0 {
		return false
	}
	now := r.now()
	addr := ip.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.byIP[addr] {
		if b.active(now) {
			return true
		}
	}
	return false
}

func (r *BanRegistry) Due(interval time.Duration, max int) []Ban {
	if r == nil || max <= 0 || r.count.Load() == 0 {
		return nil
	}
	now := r.now()
	r.mu.Lock()
	var due []Ban
	for _, list := range r.byIP {
		for _, b := range list {
			if b.active(now) && now.Sub(b.Checked) >= interval {
				due = append(due, *b)
			}
		}
	}
	r.mu.Unlock()
	slices.SortFunc(due, func(a, b Ban) int {
		if c := a.Checked.Compare(b.Checked); c != 0 {
			return c
		}
		return strings.Compare(a.Key(), b.Key())
	})
	if len(due) > max {
		due = due[:max]
	}
	return due
}

func (r *BanRegistry) RecordCheck(key string, ok bool) bool {
	if r == nil {
		return false
	}
	ip, port, valid := ParseBanKey(key)
	if !valid {
		return false
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.findLocked(ip.String(), port)
	if b == nil {
		return false
	}
	b.Checked = now
	if !b.active(now) {
		return false
	}
	if !ok {
		if b.Passes != 0 {
			b.Passes = 0
			r.dirty = true
		}
		return false
	}
	b.Passes++
	r.dirty = true
	if b.Passes < banPassesToLift {
		return false
	}
	b.Until = now
	b.Passes = 0
	return true
}

func (r *BanRegistry) Lift(key string) bool {
	if r == nil {
		return false
	}
	ip, port, valid := ParseBanKey(key)
	if !valid {
		return false
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.findLocked(ip.String(), port)
	if b == nil {
		return false
	}
	r.deleteLocked(b)
	r.dirty = true
	return b.active(now)
}

func (r *BanRegistry) Clear() []Ban {
	if r == nil {
		return nil
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	var active []Ban
	for _, list := range r.byIP {
		for _, b := range list {
			if b.active(now) {
				active = append(active, *b)
			}
		}
	}
	if len(r.byIP) > 0 {
		r.dirty = true
	}
	clear(r.byIP)
	r.count.Store(0)
	return active
}

func (r *BanRegistry) List() []Ban {
	if r == nil || r.count.Load() == 0 {
		return nil
	}
	now := r.now()
	r.mu.Lock()
	out := make([]Ban, 0, r.count.Load())
	for _, list := range r.byIP {
		for _, b := range list {
			if b.active(now) {
				out = append(out, *b)
			}
		}
	}
	r.mu.Unlock()
	slices.SortFunc(out, func(a, b Ban) int {
		if c := b.Until.Compare(a.Until); c != 0 {
			return c
		}
		return strings.Compare(a.Key(), b.Key())
	})
	return out
}

func (r *BanRegistry) Prune() int {
	if r == nil || r.count.Load() == 0 {
		return 0
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	var expired []*Ban
	for _, list := range r.byIP {
		for _, b := range list {
			if !b.remembered(now) {
				expired = append(expired, b)
			}
		}
	}
	for _, b := range expired {
		r.deleteLocked(b)
	}
	if len(expired) > 0 {
		r.dirty = true
	}
	return len(expired)
}

func (r *BanRegistry) Flush() error {
	if r == nil || r.path == "" {
		return nil
	}
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return nil
	}
	data, err := r.encodeLocked(r.now())
	r.dirty = false
	r.mu.Unlock()

	if err == nil {
		err = utils.WriteFileAtomic(r.path, data, banFileMode)
	}
	if err != nil {
		r.mu.Lock()
		r.dirty = true
		r.mu.Unlock()
	}
	return err
}

func (r *BanRegistry) encodeLocked(now time.Time) ([]byte, error) {
	file := banFile{Version: banFileVersion, Bans: make([]*Ban, 0, r.count.Load())}
	for _, list := range r.byIP {
		for _, b := range list {
			rec := *b
			rec.Since = wallClock(rec.Since, now)
			rec.Until = wallClock(rec.Until, now)
			rec.Checked = wallClock(rec.Checked, now)
			file.Bans = append(file.Bans, &rec)
		}
	}
	slices.SortFunc(file.Bans, func(a, b *Ban) int {
		return strings.Compare(a.Key(), b.Key())
	})
	return json.MarshalIndent(file, "", "  ")
}

func wallClock(t, now time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return now.Add(t.Sub(now)).Truncate(time.Second).UTC()
}

func (r *BanRegistry) findLocked(addr string, port int) *Ban {
	for _, b := range r.byIP[addr] {
		if b.Port == port {
			return b
		}
	}
	return nil
}

func (r *BanRegistry) insertLocked(b *Ban) {
	r.byIP[b.IP] = append(r.byIP[b.IP], b)
	r.count.Add(1)
}

func (r *BanRegistry) deleteLocked(b *Ban) {
	list := r.byIP[b.IP]
	for i, cur := range list {
		if cur != b {
			continue
		}
		list = slices.Delete(list, i, i+1)
		if len(list) == 0 {
			delete(r.byIP, b.IP)
		} else {
			r.byIP[b.IP] = list
		}
		r.count.Add(-1)
		return
	}
}

func (r *BanRegistry) makeRoomLocked(now time.Time) {
	for int(r.count.Load()) >= maxBans {
		var victim *Ban
		victimEnded := false
		for _, list := range r.byIP {
			for _, b := range list {
				ended := !b.active(now)
				switch {
				case victim == nil,
					ended && !victimEnded,
					ended == victimEnded && b.Until.Before(victim.Until):
					victim = b
					victimEnded = ended
				}
			}
		}
		if victim == nil {
			return
		}
		r.deleteLocked(victim)
	}
}
