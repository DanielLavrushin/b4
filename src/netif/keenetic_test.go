package netif

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func fakeKeeneticRCI(t *testing.T) *httptest.Server {
	t.Helper()
	ifaces := map[string]map[string]string{
		"Wireguard4":               {"id": "Wireguard4", "interface-name": "Wireguard4", "description": "WARP"},
		"Wireguard7":               {"id": "Wireguard7", "interface-name": "Wireguard7", "description": "MY_GER_AM"},
		"Bridge0":                  {"id": "Bridge0", "interface-name": "Home", "description": "Home network"},
		"Bridge1":                  {"id": "Bridge1", "interface-name": "Bridge1", "description": ""},
		"GigabitEthernet0/Vlan2":   {"id": "GigabitEthernet0/Vlan2", "interface-name": "ISP", "description": "CACTUS"},
		"WifiMaster0/AccessPoint0": {"id": "WifiMaster0/AccessPoint0", "interface-name": "AccessPoint", "description": "Wi-Fi access point"},
		"0":                        {"id": "0", "interface-name": "0", "description": ""},
	}
	system := map[string]any{
		"Wireguard4":               "nwg4",
		"Wireguard7":               "nwg7",
		"Bridge0":                  "br0",
		"GigabitEthernet0/Vlan2":   "eth2.2",
		"WifiMaster0/AccessPoint0": map[string]any{"status": []any{map[string]string{"status": "error"}}},
	}
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
	return httptest.NewServer(mux)
}

func TestFetchKeeneticLabels(t *testing.T) {
	srv := fakeKeeneticRCI(t)
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

func TestKeeneticLabelsWithoutRCIIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	saved := KeeneticRCI
	KeeneticRCI = srv.URL + "/rci"
	keeneticLabels.labels = nil
	t.Cleanup(func() {
		KeeneticRCI = saved
		keeneticLabels.labels = nil
	})

	if got := KeeneticLabels(); len(got) != 0 {
		t.Fatalf("unreachable RCI must give no labels, got %v", got)
	}
}

func TestFetchKeeneticHostNames(t *testing.T) {
	srv := fakeKeeneticRCI(t)
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
