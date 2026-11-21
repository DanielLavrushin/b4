package config

import (
	"os"
	"strings"
	"testing"
)

func TestTheZapretPayloadsShipWithTheirLicence(t *testing.T) {
	notice, err := os.ReadFile("bin/zapret-LICENSE-for-quic1.bin-and-stun.bin.txt")
	if err != nil {
		t.Fatalf("quic1.bin and stun.bin come from zapret, whose MIT licence asks for its notice in every copy: %v", err)
	}
	for _, line := range []string{
		"quic1.bin and stun.bin",
		"Copyright (c) 2016-2024 bol-van",
		"The above copyright notice and this permission notice shall be included in all\ncopies or substantial portions of the Software.",
	} {
		if !strings.Contains(string(notice), line) {
			t.Errorf("the zapret notice lacks %q", line)
		}
	}
}

func TestTheReadmeCreditsTheBuiltInPayloads(t *testing.T) {
	readme, err := os.ReadFile("../../readme.md")
	if err != nil {
		t.Fatal(err)
	}
	credits := string(readme)
	if i := strings.Index(credits, "## Credits"); i >= 0 {
		credits = credits[i:]
	}
	if i := strings.Index(credits, "\n## License"); i >= 0 {
		credits = credits[:i]
	}
	for _, name := range []string{
		"quic_initial_www_google_com.bin", "QUIC preset 1",
		"stun.bin", "Preset: STUN",
		"fake_sni_old", "Preset: Google (classic)",
		"fake_sni`", "Preset: VK",
		"QUIC preset 2",
		"zapret-LICENSE-for-quic1.bin-and-stun.bin.txt",
	} {
		if !strings.Contains(credits, name) {
			t.Errorf("the readme Credits section does not name %q", name)
		}
	}
}
