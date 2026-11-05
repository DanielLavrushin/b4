package netif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var KeeneticRCI = "http://127.0.0.1:79/rci"

const (
	keeneticTimeout  = time.Second
	keeneticLabelTTL = time.Minute
)

var keeneticLabels struct {
	sync.Mutex
	at     time.Time
	labels map[string]string
}

type keeneticInterface struct {
	ID          string `json:"id"`
	Name        string `json:"interface-name"`
	Description string `json:"description"`
}

type keeneticSystemName struct {
	Show struct {
		Interface struct {
			SystemName json.RawMessage `json:"system-name"`
		} `json:"interface"`
	} `json:"show"`
}

func KeeneticLabels() map[string]string {
	keeneticLabels.Lock()
	defer keeneticLabels.Unlock()
	if keeneticLabels.labels != nil && time.Since(keeneticLabels.at) < keeneticLabelTTL {
		return keeneticLabels.labels
	}
	ctx, cancel := context.WithTimeout(context.Background(), keeneticTimeout)
	defer cancel()
	labels, err := fetchKeeneticLabels(ctx, KeeneticRCI)
	if err != nil {
		labels = map[string]string{}
	}
	keeneticLabels.labels = labels
	keeneticLabels.at = time.Now()
	return labels
}

func fetchKeeneticLabels(ctx context.Context, base string) (map[string]string, error) {
	var ifaces map[string]keeneticInterface
	if err := keeneticCall(ctx, http.MethodGet, base+"/show/interface", nil, &ifaces); err != nil {
		return nil, err
	}

	var ids []string
	var batch []map[string]any
	for id, iface := range ifaces {
		if keeneticLabel(id, iface) == "" {
			continue
		}
		ids = append(ids, id)
		batch = append(batch, map[string]any{
			"show": map[string]any{"interface": map[string]any{"system-name": map[string]string{"name": id}}},
		})
	}
	if len(batch) == 0 {
		return map[string]string{}, nil
	}

	var names []keeneticSystemName
	if err := keeneticCall(ctx, http.MethodPost, base+"/", batch, &names); err != nil {
		return nil, err
	}

	labels := make(map[string]string, len(names))
	for i, reply := range names {
		if i >= len(ids) {
			break
		}
		var system string
		if json.Unmarshal(reply.Show.Interface.SystemName, &system) != nil || system == "" {
			continue
		}
		if _, taken := labels[system]; !taken {
			labels[system] = keeneticLabel(ids[i], ifaces[ids[i]])
		}
	}
	return labels, nil
}

func keeneticLabel(id string, iface keeneticInterface) string {
	if iface.Description != "" {
		return iface.Description
	}
	if iface.Name != "" && iface.Name != id {
		return iface.Name
	}
	return ""
}

func keeneticCall(ctx context.Context, method, url string, body, out any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("keenetic rci %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func KeeneticHostNames() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), keeneticTimeout)
	defer cancel()
	names, err := fetchKeeneticHostNames(ctx, KeeneticRCI)
	if err != nil {
		return nil
	}
	return names
}

func fetchKeeneticHostNames(ctx context.Context, base string) (map[string]string, error) {
	var reply struct {
		Host []struct {
			MAC      string `json:"mac"`
			Name     string `json:"name"`
			Hostname string `json:"hostname"`
		} `json:"host"`
	}
	if err := keeneticCall(ctx, http.MethodGet, base+"/show/ip/hotspot", nil, &reply); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(reply.Host))
	for _, host := range reply.Host {
		name := strings.TrimSpace(host.Name)
		if name == "" {
			name = strings.TrimSpace(host.Hostname)
		}
		if host.MAC != "" && name != "" {
			names[strings.ToUpper(host.MAC)] = name
		}
	}
	return names, nil
}
