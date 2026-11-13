package netif

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/log"
)

func keeneticRCIMux(t *testing.T, ifaces map[string]map[string]string, system map[string]any) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rci/show/interface", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ifaces)
	})
	mux.HandleFunc("GET /rci/show/ip/hotspot", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"host":[
			{"mac":"fc:b2:14:00:00:01","ip":"192.168.0.50","hostname":"Nikitas-MBP","name":"MB_PRO_END"},
			{"mac":"b8:87:6e:00:00:02","ip":"192.168.0.109","hostname":"","name":"yandex-mini2"},
			{"mac":"90:2c:09:00:00:03","ip":"192.168.0.126","hostname":"iPhone-Maria","name":""},
			{"mac":"00:11:22:00:00:04","ip":"192.168.0.200","hostname":"","name":""}
		]}`))
	})
	mux.HandleFunc("POST /rci/", func(w http.ResponseWriter, r *http.Request) {
		var batch []keeneticSystemName
		var raw []map[string]map[string]map[string]map[string]string
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Errorf("batch body: %v", err)
		}
		for _, item := range raw {
			name := item["show"]["interface"]["system-name"]["name"]
			value, _ := json.Marshal(system[name])
			var reply keeneticSystemName
			reply.Show.Interface.SystemName = value
			batch = append(batch, reply)
		}
		_ = json.NewEncoder(w).Encode(batch)
	})
	return mux
}

func fakeKeeneticRCI(t *testing.T) http.Handler {
	t.Helper()
	ifaces := map[string]map[string]string{
		"Wireguard4":               {"id": "Wireguard4", "interface-name": "Wireguard4", "description": "WARP"},
		"Wireguard7":               {"id": "Wireguard7", "interface-name": "Wireguard7", "description": "MY_GER_AM"},
		"Bridge0":                  {"id": "Bridge0", "interface-name": "Home", "description": "Home network"},
		"Bridge1":                  {"id": "Bridge1", "interface-name": "Bridge1", "description": ""},
		"Bridge2":                  {"id": "Bridge2", "interface-name": "Bridge2", "description": "br2"},
		"GigabitEthernet0/Vlan2":   {"id": "GigabitEthernet0/Vlan2", "interface-name": "ISP", "description": "CACTUS"},
		"WifiMaster0/AccessPoint0": {"id": "WifiMaster0/AccessPoint0", "interface-name": "AccessPoint", "description": "Wi-Fi access point"},
		"0":                        {"id": "0", "interface-name": "0", "description": ""},
	}
	system := map[string]any{
		"Wireguard4":               "nwg4",
		"Wireguard7":               "nwg7",
		"Bridge0":                  "br0",
		"Bridge2":                  "br2",
		"GigabitEthernet0/Vlan2":   "eth2.2",
		"WifiMaster0/AccessPoint0": map[string]any{"status": []any{map[string]string{"status": "error"}}},
	}
	return keeneticRCIMux(t, ifaces, system)
}

type keeneticRCIStub struct {
	*httptest.Server
	fail  atomic.Bool
	calls atomic.Int32
}

func stubKeeneticRCI(t *testing.T) *keeneticRCIStub {
	t.Helper()
	stub := &keeneticRCIStub{}
	rci := fakeKeeneticRCI(t)
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.calls.Add(1)
		if stub.fail.Load() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rci.ServeHTTP(w, r)
	}))
	saved := KeeneticRCI
	KeeneticRCI = stub.URL + "/rci"
	resetKeeneticCaches()
	t.Cleanup(func() {
		stub.Close()
		KeeneticRCI = saved
		resetKeeneticCaches()
	})
	return stub
}

func resetKeeneticCaches() {
	for _, c := range []*keeneticCache{&keeneticLabels, &keeneticHosts} {
		c.at, c.names, c.failing = time.Time{}, nil, false
	}
}

func expireKeeneticCaches() {
	for _, c := range []*keeneticCache{&keeneticLabels, &keeneticHosts} {
		c.at = time.Now().Add(-2 * keeneticTTL)
	}
}

func TestFetchKeeneticLabels(t *testing.T) {
	srv := httptest.NewServer(fakeKeeneticRCI(t))
	defer srv.Close()

	got, err := fetchKeeneticLabels(context.Background(), srv.URL+"/rci")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"nwg4":   "WARP",
		"nwg7":   "MY_GER_AM",
		"br0":    "Home network",
		"eth2.2": "CACTUS",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFetchKeeneticLabelsIsStableWhenIdsShareASystemName(t *testing.T) {
	ifaces := map[string]map[string]string{
		"Wireguard1": {"id": "Wireguard1", "interface-name": "Wireguard1", "description": "First"},
		"Wireguard2": {"id": "Wireguard2", "interface-name": "Wireguard2", "description": "Second"},
	}
	system := map[string]any{"Wireguard1": "eth3", "Wireguard2": "eth3"}
	srv := httptest.NewServer(keeneticRCIMux(t, ifaces, system))
	defer srv.Close()

	for i := 0; i < 50; i++ {
		got, err := fetchKeeneticLabels(context.Background(), srv.URL+"/rci")
		if err != nil {
			t.Fatal(err)
		}
		if got["eth3"] != "First" {
			t.Fatalf("fetch %d: eth3 = %q, want the label of the first id in order", i, got["eth3"])
		}
	}
}

func TestKeeneticLabelsWithoutRCIIsEmpty(t *testing.T) {
	stub := stubKeeneticRCI(t)
	stub.Close()

	if got := KeeneticLabels(); len(got) != 0 {
		t.Fatalf("unreachable RCI must give no labels, got %v", got)
	}
}

func TestFetchKeeneticHostNames(t *testing.T) {
	srv := httptest.NewServer(fakeKeeneticRCI(t))
	defer srv.Close()

	got, err := fetchKeeneticHostNames(context.Background(), srv.URL+"/rci")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FC:B2:14:00:00:01": "MB_PRO_END",
		"B8:87:6E:00:00:02": "yandex-mini2",
		"90:2C:09:00:00:03": "iPhone-Maria",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeeneticNamesSurviveAFailedCall(t *testing.T) {
	stub := stubKeeneticRCI(t)
	for _, tc := range []struct {
		subject   string
		read      func() map[string]string
		key, want string
	}{
		{"interface names", KeeneticLabels, "nwg4", "WARP"},
		{"device names", KeeneticHostNames, "FC:B2:14:00:00:01", "MB_PRO_END"},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			stub.fail.Store(false)
			if got := tc.read()[tc.key]; got != tc.want {
				t.Fatalf("good reply: %s = %q, want %q", tc.key, got, tc.want)
			}

			stub.fail.Store(true)
			expireKeeneticCaches()
			if got := tc.read()[tc.key]; got != tc.want {
				t.Fatalf("after a failed call: %s = %q, want the last good %q", tc.key, got, tc.want)
			}

			calls := stub.calls.Load()
			if got := tc.read()[tc.key]; got != tc.want || stub.calls.Load() != calls {
				t.Fatalf("a failed call must not be repeated before the cache expires: %s = %q, calls %d -> %d",
					tc.key, got, calls, stub.calls.Load())
			}
		})
	}
}

func TestKeeneticFailureIsLoggedOncePerStreak(t *testing.T) {
	stub := stubKeeneticRCI(t)
	buf := &bytes.Buffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	read := func(fail bool) {
		stub.fail.Store(fail)
		expireKeeneticCaches()
		KeeneticHostNames()
	}
	warnings := func() int { return strings.Count(buf.String(), "[WARN]") }

	read(true)
	read(true)
	if warnings() != 1 || !strings.Contains(buf.String(), "401") {
		t.Fatalf("two failed calls in a row must log one warning with the status, got:\n%s", buf.String())
	}

	read(false)
	read(true)
	if warnings() != 2 {
		t.Fatalf("a failure after a good reply starts a new streak and must be logged, got:\n%s", buf.String())
	}
}
