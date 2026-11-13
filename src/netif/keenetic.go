package netif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

var KeeneticRCI = "http://127.0.0.1:79/rci"

const (
	keeneticTimeout = time.Second
	keeneticTTL     = time.Minute
)

type keeneticCache struct {
	mu      sync.Mutex
	subject string
	fetch   func(ctx context.Context, base string) (map[string]string, error)
	at      time.Time
	names   map[string]string
	failing bool
}

var (
	keeneticLabels = keeneticCache{subject: "interface names", fetch: fetchKeeneticLabels}
	keeneticHosts  = keeneticCache{subject: "device names", fetch: fetchKeeneticHostNames}
)

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
	return keeneticLabels.get()
}

func KeeneticHostNames() map[string]string {
	return keeneticHosts.get()
}

func (c *keeneticCache) get() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < keeneticTTL {
		return c.names
	}
	ctx, cancel := context.WithTimeout(context.Background(), keeneticTimeout)
	defer cancel()
	names, err := c.fetch(ctx, KeeneticRCI)
	c.at = time.Now()
	if err != nil {
		if !c.failing {
			log.Warnf("Keenetic RCI: cannot read %s, keeping the last ones: %v", c.subject, err)
		}
		c.failing = true
		return c.names
	}
	if c.failing {
		log.Infof("Keenetic RCI: %s are read again", c.subject)
	}
	c.failing = false
	c.names = names
	return names
}

func fetchKeeneticLabels(ctx context.Context, base string) (map[string]string, error) {
	var ifaces map[string]keeneticInterface
	if err := keeneticCall(ctx, http.MethodGet, base+"/show/interface", nil, &ifaces); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(ifaces))
	for id, iface := range ifaces {
		if keeneticLabel(id, iface) != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	slices.Sort(ids)

	batch := make([]map[string]any, len(ids))
	for i, id := range ids {
		batch[i] = map[string]any{
			"show": map[string]any{"interface": map[string]any{"system-name": map[string]string{"name": id}}},
		}
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
		label := keeneticLabel(ids[i], ifaces[ids[i]])
		if _, taken := labels[system]; !taken && label != system {
			labels[system] = label
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
		return fmt.Errorf("%s %s: %s", method, url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
