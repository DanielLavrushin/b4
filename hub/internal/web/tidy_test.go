package web

import (
	"strings"
	"testing"
)

func kinds(t *Tidy) map[string]int {
	out := map[string]int{}
	if t == nil {
		return out
	}
	for _, s := range t.Suggestions {
		out[s.Kind]++
	}
	return out
}

func TestTidyDomainsDiscord(t *testing.T) {
	tidy := TidyDomains([]string{
		"cdn.discordapp.com", "discordapp.net", "discordapp.com", "discord.gg", "media.discordapp.net",
		"images-ext-1.discordapp.net", "www.discord.com", "www.discord.app", "discord.app", "*.discord.com",
		"*.discord.gg", "*.discordapp.com", "*.discordapp.net", "discord.media", "*.discord.media", "discordcdn.com",
		"discord.dev", "discord.new", "discord.gift", "discordstatus.com", "dis.gd", "discord.co",
	})
	if tidy == nil {
		t.Fatal("the discord list needs tidying")
	}
	got := kinds(tidy)
	if got[TidyDeadWildcard] != 5 || got[TidyCovered] != 4 || got[TidyWWWOnly] != 1 || got[TidyDuplicate] != 0 || len(tidy.Suggestions) != 10 {
		t.Fatalf("suggestions: %+v", tidy.Suggestions)
	}
	want := "discordapp.net,discordapp.com,discord.gg,discord.com,discord.app,discord.media,discordcdn.com,discord.dev,discord.new,discord.gift,discordstatus.com,dis.gd,discord.co"
	if strings.Join(tidy.Domains, ",") != want {
		t.Fatalf("domains: %v", tidy.Domains)
	}
	for _, s := range tidy.Suggestions {
		switch s.Entry {
		case "www.discord.com":
			if s.Kind != TidyWWWOnly || s.Replacement != "discord.com" {
				t.Fatalf("www.discord.com: %+v", s)
			}
		case "*.discord.com":
			if s.Kind != TidyDeadWildcard || s.By != "discord.com" || s.Replacement != "" {
				t.Fatalf("*.discord.com: %+v", s)
			}
		case "cdn.discordapp.com":
			if s.Kind != TidyCovered || s.By != "discordapp.com" {
				t.Fatalf("cdn.discordapp.com: %+v", s)
			}
		case "www.discord.app":
			if s.Kind != TidyCovered || s.By != "discord.app" {
				t.Fatalf("www.discord.app: %+v", s)
			}
		}
	}
}

func TestTidyDomainsEdges(t *testing.T) {
	if tidy := TidyDomains([]string{"regexp:^a.*\\.example\\.com$", "example.com", "other.example"}); tidy != nil {
		t.Fatalf("a clean list must need nothing: %+v", tidy)
	}
	if tidy := TidyDomains(nil); tidy != nil {
		t.Fatalf("an empty list must need nothing: %+v", tidy)
	}
	tidy := TidyDomains([]string{"example.com", "Example.com.", "*.example.net", "*.", "www.example.org", "api.example.org"})
	if tidy == nil {
		t.Fatal("expected suggestions")
	}
	got := kinds(tidy)
	if got[TidyDuplicate] != 1 || got[TidyDeadWildcard] != 2 || got[TidyWWWOnly] != 1 || got[TidyCovered] != 1 {
		t.Fatalf("suggestions: %+v", tidy.Suggestions)
	}
	if strings.Join(tidy.Domains, ",") != "example.com,example.net,example.org" {
		t.Fatalf("domains: %v", tidy.Domains)
	}
	for _, s := range tidy.Suggestions {
		if s.Entry == "*.example.net" && s.Replacement != "example.net" {
			t.Fatalf("a wildcard without its apex must be replaced: %+v", s)
		}
		if s.Entry == "Example.com." && s.By != "example.com" {
			t.Fatalf("duplicate must name the first spelling: %+v", s)
		}
		if s.Entry == "api.example.org" && s.By != "example.org" {
			t.Fatalf("the www replacement must cover api: %+v", s)
		}
	}
}
