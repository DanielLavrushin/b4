package discovery

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const customPayloadBase = 1000

func customPayloadID(idx int) int { return customPayloadBase + idx }

func isCustomPayload(payloadType int) bool { return payloadType >= customPayloadBase }

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
		if presets[i].Name == "combo-pastseq" {
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
				Payload: customPayloadID(i),
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

	variants := []struct {
		presetName string
		sniType    int
	}{
		{basePreset.Name, config.FakePayloadSTUN},
		{basePreset.Name + "-p1", config.FakePayloadDefault1},
		{basePreset.Name + "-alt", config.FakePayloadDefault2},
	}

	for i, v := range variants {
		if ds.canceled() {
			break
		}
		if _, exists := ds.domainResults[ds.Domain].Results[v.presetName]; exists {
			continue
		}

		if i > 0 {
			ds.CheckSuite.mu.Lock()
			ds.TotalChecks++
			ds.CheckSuite.mu.Unlock()
		}

		testPreset := *basePreset
		testPreset.Name = v.presetName
		testPreset.Config.Faking.SNIType = v.sniType

		result := ds.testPreset(testPreset)
		ds.storeResult(testPreset, result)

		ds.workingPayloads = append(ds.workingPayloads, PayloadTestResult{
			Payload: v.sniType,
			Works:   result.Status == CheckStatusComplete,
			Speed:   result.Speed,
		})

		if result.Status == CheckStatusComplete {
			log.DiscoveryLogf("    Payload %s: SUCCESS (%.2f KB/s)", ds.getPayloadName(v.sniType), result.Speed/1024)
		} else {
			log.DiscoveryLogf("    Payload %s: FAILED", ds.getPayloadName(v.sniType))
		}
	}

	ds.selectBestPayload()
}

func (ds *DiscoverySuite) selectBestPayload() {
	var bestSpeed float64
	ds.bestPayload = config.FakePayloadSTUN
	ds.bestPayloadFile = ""

	workingCount := 0
	for _, pr := range ds.workingPayloads {
		if pr.Works {
			workingCount++
			if pr.Speed > bestSpeed {
				bestSpeed = pr.Speed
				ds.bestPayload = pr.Payload

				// Track filepath for custom payloads
				if isCustomPayload(pr.Payload) {
					idx := pr.Payload - customPayloadBase
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
	if isCustomPayload(payloadType) {
		idx := payloadType - customPayloadBase
		if idx < len(ds.customPayloads) {
			return ds.customPayloads[idx].Name
		}
	}
	switch payloadType {
	case config.FakePayloadSTUN:
		return "stun"
	case config.FakePayloadDefault1:
		return "google"
	case config.FakePayloadDefault2:
		return "duckduckgo"
	case config.FakePayloadRandom:
		return "random"
	case config.FakePayloadZero:
		return "zero"
	case config.FakePayloadInverted:
		return "inverted"
	case config.FakePayloadDomain:
		return "domain"
	case config.FakePayloadCapture:
		return "capture"
	default:
		return "unknown"
	}
}

func (ds *DiscoverySuite) applyBestPayload(faking *config.FakingConfig) {
	if isCustomPayload(ds.bestPayload) {
		faking.SNIType = config.FakePayloadCapture
		idx := ds.bestPayload - customPayloadBase
		if idx < len(ds.customPayloads) {
			faking.PayloadFile = ds.customPayloads[idx].Filepath
			faking.PayloadData = ds.customPayloads[idx].Data
		}
	} else {
		faking.SNIType = ds.bestPayload
	}
}

func (ds *DiscoverySuite) testPresetWithPayload(preset ConfigPreset, payloadType int) CheckResult {
	modifiedPreset := preset

	if isCustomPayload(payloadType) {
		modifiedPreset.Config.Faking.SNIType = config.FakePayloadCapture
		idx := payloadType - customPayloadBase
		if idx < len(ds.customPayloads) {
			modifiedPreset.Config.Faking.PayloadFile = ds.customPayloads[idx].Filepath
			modifiedPreset.Config.Faking.PayloadData = ds.customPayloads[idx].Data
		}
	} else {
		modifiedPreset.Config.Faking.SNIType = payloadType
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

	if preset.FixedPayload {
		result := ds.testPresetInternal(preset)
		if result.Status == CheckStatusComplete && !isCustomPayload(preset.Config.Faking.SNIType) {
			ds.updatePayloadKnowledge(preset.Config.Faking.SNIType, result.Speed)
		}
		return result
	}

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

	for i := range ds.customPayloads {
		result := ds.testPresetWithPayload(preset, customPayloadID(i))
		if result.Status == CheckStatusComplete {
			ds.updatePayloadKnowledge(customPayloadID(i), result.Speed)
			return result
		}
	}

	fallbacks := []int{config.FakePayloadSTUN, config.FakePayloadDefault1, config.FakePayloadDefault2}
	var firstResult CheckResult
	for i, payload := range fallbacks {
		result := ds.testPresetWithPayload(preset, payload)
		if i == 0 {
			firstResult = result
		}
		if result.Status == CheckStatusComplete {
			ds.updatePayloadKnowledge(payload, result.Speed)
			return result
		}
	}

	return firstResult
}
