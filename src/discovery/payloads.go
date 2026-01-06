package discovery

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func loadCustomPayloads(cfg *config.Config, payloadFiles []string) []CustomPayload {
	var result []CustomPayload

	captureManager := capture.GetManager(cfg)
	if captureManager == nil {
		return result
	}

	captures := captureManager.ListCaptures()
	captureMap := make(map[string]*capture.Capture)
	for _, c := range captures {
		captureMap[c.Domain] = c
	}

	for _, name := range payloadFiles {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}

		if c, ok := captureMap[name]; ok {
			data, err := captureManager.LoadCaptureData(c)
			if err != nil {
				log.DiscoveryLogf("Discovery: failed to load capture %s: %v", name, err)
				continue
			}
			result = append(result, CustomPayload{
				Name:     c.Domain,
				Filepath: c.Filepath,
				Data:     data,
			})
			log.DiscoveryLogf("Loaded custom payload: %s (%d bytes)", c.Domain, len(data))
		} else {
			log.DiscoveryLogf("Discovery: capture not found: %s", name)
		}
	}

	return result
}

func (ds *DiscoverySuite) detectWorkingPayloads(presets []ConfigPreset) {
	log.DiscoveryLogf("  Testing payload variants...")

	var basePreset *ConfigPreset
	for i := range presets {
		if presets[i].Name == "proven-combo" {
			basePreset = &presets[i]
			break
		}
	}
	if basePreset == nil {
		return
	}

	if len(ds.customPayloads) > 0 {
		for i, cp := range ds.customPayloads {
			testPreset := *basePreset
			testPreset.Name = fmt.Sprintf("payload-test-%s", cp.Name)
			testPreset.Config.Faking.SNIType = config.FakePayloadCapture
			testPreset.Config.Faking.PayloadFile = cp.Filepath
			testPreset.Config.Faking.PayloadData = cp.Data

			result := ds.testPresetInternal(testPreset)

			ds.workingPayloads = append(ds.workingPayloads, PayloadTestResult{
				Payload: config.FakePayloadCapture + i,
				Works:   result.Status == CheckStatusComplete,
				Speed:   result.Speed,
			})

			if result.Status == CheckStatusComplete {
				log.DiscoveryLogf("    Payload '%s': SUCCESS (%.2f KB/s)", cp.Name, result.Speed/1024)
			} else {
				log.DiscoveryLogf("    Payload '%s': FAILED", cp.Name)
			}
		}
		ds.selectBestPayload()
		return
	}

	if _, exists := ds.domainResult.Results["proven-combo"]; !exists {
		result1 := ds.testPreset(*basePreset)
		ds.storeResult(*basePreset, result1)

		ds.workingPayloads = append(ds.workingPayloads, PayloadTestResult{
			Payload: config.FakePayloadDefault1,
			Works:   result1.Status == CheckStatusComplete,
			Speed:   result1.Speed,
		})

		if result1.Status == CheckStatusComplete {
			log.DiscoveryLogf("    Payload 1 (google): SUCCESS (%.2f KB/s)", result1.Speed/1024)
		} else {
			log.DiscoveryLogf("    Payload 1 (google): FAILED")
		}
	}

	var payload2Preset *ConfigPreset
	for i := range presets {
		if presets[i].Name == "proven-combo-alt" {
			payload2Preset = &presets[i]
			break
		}
	}

	if payload2Preset != nil {
		if _, exists := ds.domainResult.Results["proven-combo-alt"]; !exists {
			result2 := ds.testPreset(*payload2Preset)
			ds.storeResult(*payload2Preset, result2)

			ds.workingPayloads = append(ds.workingPayloads, PayloadTestResult{
				Payload: config.FakePayloadDefault2,
				Works:   result2.Status == CheckStatusComplete,
				Speed:   result2.Speed,
			})

			if result2.Status == CheckStatusComplete {
				log.DiscoveryLogf("    Payload 2 (duckduckgo): SUCCESS (%.2f KB/s)", result2.Speed/1024)
			} else {
				log.DiscoveryLogf("    Payload 2 (duckduckgo): FAILED")
			}
		}
	}

	ds.selectBestPayload()
}

func (ds *DiscoverySuite) selectBestPayload() {
	var bestSpeed float64
	ds.bestPayload = config.FakePayloadDefault1
	ds.bestPayloadFile = ""

	workingCount := 0
	for _, pr := range ds.workingPayloads {
		if pr.Works {
			workingCount++
			if pr.Speed > bestSpeed {
				bestSpeed = pr.Speed
				ds.bestPayload = pr.Payload

				// Track filepath for custom payloads
				if pr.Payload >= config.FakePayloadCapture {
					idx := pr.Payload - config.FakePayloadCapture
					if idx < len(ds.customPayloads) {
						ds.bestPayloadFile = ds.customPayloads[idx].Filepath
					}
				} else {
					ds.bestPayloadFile = ""
				}
			}
		}
	}

	if workingCount == 0 {
		log.DiscoveryLogf("  No payloads worked - will test during discovery")
	} else {
		log.DiscoveryLogf("  Selected payload: %s", ds.getPayloadName(ds.bestPayload))
	}
}

func (ds *DiscoverySuite) getPayloadName(payloadType int) string {
	if payloadType >= config.FakePayloadCapture {
		idx := payloadType - config.FakePayloadCapture
		if idx < len(ds.customPayloads) {
			return ds.customPayloads[idx].Name
		}
	}
	switch payloadType {
	case config.FakePayloadDefault1:
		return "google"
	case config.FakePayloadDefault2:
		return "duckduckgo"
	default:
		return "unknown"
	}
}

func (ds *DiscoverySuite) testPresetWithPayload(preset ConfigPreset, payloadType int) CheckResult {
	modifiedPreset := preset
	modifiedPreset.Config.Faking.SNIType = payloadType

	if payloadType >= config.FakePayloadCapture {
		idx := payloadType - config.FakePayloadCapture
		if idx < len(ds.customPayloads) {
			modifiedPreset.Config.Faking.PayloadFile = ds.customPayloads[idx].Filepath
			modifiedPreset.Config.Faking.PayloadData = ds.customPayloads[idx].Data
		}
	}

	return ds.testPresetInternal(modifiedPreset)
}

func (ds *DiscoverySuite) updatePayloadKnowledge(payload int, speed float64) {
	for i, pr := range ds.workingPayloads {
		if pr.Payload == payload {
			if !pr.Works || speed > pr.Speed {
				ds.workingPayloads[i].Works = true
				ds.workingPayloads[i].Speed = speed
			}
			ds.selectBestPayload()
			return
		}
	}

	ds.workingPayloads = append(ds.workingPayloads, PayloadTestResult{
		Payload: payload,
		Works:   true,
		Speed:   speed,
	})
	ds.selectBestPayload()
}

func (ds *DiscoverySuite) testPresetWithBestPayload(preset ConfigPreset) CheckResult {
	defer func() {
		ds.CheckSuite.mu.Lock()
		ds.CompletedChecks++
		ds.CheckSuite.mu.Unlock()
	}()

	hasWorkingPayload := false
	for _, pr := range ds.workingPayloads {
		if pr.Works {
			hasWorkingPayload = true
			break
		}
	}

	if hasWorkingPayload {
		return ds.testPresetWithPayload(preset, ds.bestPayload)
	}

	result1 := ds.testPresetWithPayload(preset, config.FakePayloadDefault1)
	if result1.Status == CheckStatusComplete {
		ds.updatePayloadKnowledge(config.FakePayloadDefault1, result1.Speed)
		return result1
	}

	result2 := ds.testPresetWithPayload(preset, config.FakePayloadDefault2)
	if result2.Status == CheckStatusComplete {
		ds.updatePayloadKnowledge(config.FakePayloadDefault2, result2.Speed)
		return result2
	}

	return result1
}
