package main

import (
	"fmt"
	"os"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	reasonDuplicate = "Duplicates an existing set with the same targets."
	reasonSpam      = "Advertising or unrelated content."
	reasonTooBroad  = "Targets far more than the title says."
	reasonBroken    = "Reported broken by several networks; hidden until the author updates it."
	reasonBanSpam   = "Repeated spam submissions."
	reasonWithdraw  = "Withdrawn at the author's request."
	reasonOperator  = "No contact for the operator; resubmit with one."
	reasonOutage    = "The report describes an ISP outage, not the set."
)

var reasonPresets = []store.ReasonPreset{
	{Scope: store.ScopeReject, Label: "Duplicate", Text: reasonDuplicate},
	{Scope: store.ScopeReject, Label: "Spam", Text: reasonSpam},
	{Scope: store.ScopeReject, Label: "Too broad", Text: reasonTooBroad},
	{Scope: store.ScopeHide, Label: "Broken strategy", Text: reasonBroken},
	{Scope: store.ScopeBan, Label: "Spam", Text: reasonBanSpam},
	{Scope: store.ScopeWithdraw, Label: "Author request", Text: reasonWithdraw},
	{Scope: store.ScopeMirrorReject, Label: "Unknown operator", Text: reasonOperator},
	{Scope: store.ScopeReportDismiss, Label: "Network issue", Text: reasonOutage},
}

func (s *seeder) story() {
	w := s.world
	for range 80 {
		s.voters = append(s.voters, w.random())
	}
	staff := w.on("12389")
	ytFixer := w.on("12389")
	tehran := w.on("58224")
	quitter := w.on("3216")
	megafon := w.on("31213")
	flooder := w.on("31213")
	qa := w.on("8359")
	spammer := w.router(networks[len(networks)-1])
	s.voters = append(s.voters, staff, ytFixer, tehran, quitter, megafon, flooder)
	author := func() *router {
		r := w.random()
		s.voters = append(s.voters, r)
		return r
	}

	mirrorA := &mirrorNode{router: w.on("24940"), url: "http://127.0.0.2:" + s.port, version: "1.3.0"}
	mirrorB := &mirrorNode{router: w.on("3320"), url: "https://mirror.b4hub.test", version: "1.2.0"}
	mirrorC := &mirrorNode{router: w.on("24940"), url: "http://127.0.0.3:" + s.port, version: "1.3.0"}
	mirrorD := &mirrorNode{router: w.on("9198"), url: "https://b4.mirror-kz.test", version: "1.3.0"}
	mirrorE := &mirrorNode{router: w.router(networks[len(networks)-1]), url: "https://free-vpn-mirror.test", version: "1.1.0"}

	s.presets(0.30, reasonPresets)
	s.on(0.40, func() error { return s.build(catalogue.TriggerStartup) })
	s.daily()

	s.announce(mirrorA, 0.45, scriptDays)
	s.mirrorAction(0.50, mirrorA, s.cli, moderation.ActionApprove, "")
	s.checks(mirrorA, 0.55, scriptDays, "", "")
	s.announce(mirrorB, 4.2, 41.4)
	s.mirrorAction(5.1, mirrorB, s.console, moderation.ActionApprove, "")
	s.checks(mirrorB, 5.15, 41.5, catalogue.CheckHealth, `Get "https://mirror.b4hub.test/b4/health": dial tcp: lookup mirror.b4hub.test: no such host`)
	s.announce(mirrorC, 11.6, scriptDays)
	s.mirrorAction(12.2, mirrorC, s.console, moderation.ActionApprove, "")
	s.checks(mirrorC, 12.25, scriptDays, "", "")
	s.announce(mirrorE, 19.4, 21.4)
	s.mirrorAction(20.1, mirrorE, s.console, moderation.ActionReject, reasonOperator)
	s.announce(mirrorD, 43.5, scriptDays)

	youtube := []string{"youtube.com", "googlevideo.com", "ytimg.com", "youtu.be", "ggpht.com"}
	yt := &setSpec{title: "YouTube", description: "Fake SNI with a TLS record split. Tested on Rostelecom and MTS home lines.", domains: youtube, strategy: "tls", ttl: 6}
	s.share(1.0, ytFixer, yt, nil)
	s.moderate(1.4, moderation.ActionApprove, "", yt)
	s.votes(yt, 40, 0.85, 1.5, 30)
	s.on(5.25, func() error { return s.voteAs(qa, yt, hubwire.VoteWorks) })

	discord := &setSpec{title: "Discord", description: "Gateway, CDN and voice endpoints.", domains: []string{"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media"}, strategy: "combo", ttl: 5}
	s.share(2.0, author(), discord, nil)
	s.moderate(2.6, moderation.ActionApprove, "", discord)
	s.votes(discord, 34, 0.8, 2.7, 44.8)
	s.duplicate(9.4, author(), discord)

	meta := &setSpec{title: "Instagram + Facebook", domains: []string{"instagram.com", "cdninstagram.com", "facebook.com", "fbcdn.net"}, strategy: "oob", ttl: 7}
	x := &setSpec{title: "X / Twitter", description: "Images load slowly on MTS mobile; text works.", domains: []string{"x.com", "twitter.com", "twimg.com", "t.co"}, strategy: "disorder", ttl: 6}
	s.share(3.0, author(), meta, nil)
	s.share(3.3, ytFixer, x, nil)
	s.moderate(3.7, moderation.ActionApprove, "", meta, x)
	s.votes(meta, 22, 0.7, 3.8, 44.8)
	s.votes(x, 26, 0.55, 3.8, 44.8)

	tgWeb := &setSpec{title: "Telegram Web", domains: []string{"web.telegram.org", "telegram.org", "t.me"}, strategy: "tls", ttl: 8}
	tgIP := &setSpec{title: "Telegram DC addresses", description: "IP targets for the Telegram data centres.", ips: []string{"149.154.160.0/20", "91.108.4.0/22", "91.108.56.0/22"}, strategy: "tcp", ttl: 6}
	s.share(4.0, staff, tgWeb, nil)
	s.share(4.1, staff, tgIP, nil)
	s.profile(4.2, staff, "hub staff", "Maintainer key for sets published by the hub team.", store.TagStaff)
	s.moderate(4.3, moderation.ActionApprove, "", tgWeb, tgIP)
	s.votes(tgWeb, 18, 0.9, 4.4, 44.8)
	s.votes(tgIP, 12, 0.75, 4.4, 44.8)

	linkedin := &setSpec{title: "LinkedIn", description: "Uses a captured ClientHello as the fake.", domains: []string{"linkedin.com", "licdn.com"}, strategy: "tls", ttl: 5, capture: true}
	s.share(5.0, author(), linkedin, nil)
	s.profile(5.3, qa, "QA router", "Lab router used for end-to-end tests.", store.TagTest)
	s.moderate(5.5, moderation.ActionApprove, "", linkedin)
	s.votes(linkedin, 8, 0.7, 5.6, 44.8)

	rutracker := &setSpec{title: "rutracker.org", domains: []string{"rutracker.org", "rutracker.cc"}, strategy: "firstbyte", ttl: 6}
	s.share(6.0, author(), rutracker, nil)
	s.moderate(6.4, moderation.ActionApprove, "", rutracker)
	s.on(8.0, func() error {
		_, err := s.mod.EditText(s.ctx, s.console, moderation.Ref{SetID: rutracker.pub.setID, Version: rutracker.pub.version}, moderation.TextEdit{Title: "Rutracker", Description: "Tracker and forum.", Note: "title cleanup"})
		return err
	})
	s.votes(rutracker, 20, 0.9, 6.5, 44.8)

	chatgpt := &setSpec{title: "ChatGPT / OpenAI", description: "Works on TCI and MCI; needs the extended split.", domains: []string{"chatgpt.com", "openai.com", "oaistatic.com", "oaiusercontent.com"}, strategy: "extsplit", ttl: 6}
	s.share(7.0, tehran, chatgpt, nil)
	s.moderate(7.3, moderation.ActionApprove, "", chatgpt)
	s.votes(chatgpt, 30, 0.88, 7.4, 44.8)
	s.report(44.4, chatgpt, w.on("197207"), "Slow on MCI mobile, pages time out after a minute.")

	netflix := &setSpec{title: "Netflix", domains: []string{"netflix.com", "nflxvideo.net", "nflxso.net"}, strategy: "tls", ttl: 3}
	netflixA, netflixB := w.on("12389"), w.on("8359")
	s.share(8.0, author(), netflix, nil)
	s.moderate(8.3, moderation.ActionApprove, "", netflix)
	s.votes(netflix, 22, 0.3, 8.4, 44.8)
	s.report(15.2, netflix, netflixA, "Buffering forever on Rostelecom.")
	s.report(15.8, netflix, netflixB, "Does not load at all, the player shows an error.")
	s.settleReport(17.1, netflix, netflixA, moderation.ActionDismiss, reasonOutage)
	s.settleReport(17.2, netflix, netflixB, moderation.ActionResolve, "Author notified.")

	proton := &setSpec{title: "Proton Mail / VPN", domains: []string{"proton.me", "protonmail.com", "protonvpn.com"}, strategy: "oob", ttl: 7}
	s.share(9.0, author(), proton, nil)
	s.moderate(9.5, moderation.ActionApprove, "", proton)
	s.votes(proton, 12, 0.8, 9.6, 44.8)

	signal := &setSpec{title: "Signal", domains: []string{"signal.org", "whispersystems.org", "signal.art"}, strategy: "tls", ttl: 5}
	s.share(10.0, tehran, signal, nil)
	s.moderate(10.4, moderation.ActionApprove, "", signal)
	s.votes(signal, 14, 0.85, 10.5, 44.8)
	retired := w.router(networks[0]).id.KeyID()
	s.on(10.6, func() error {
		_, err := s.mod.Revoke(s.ctx, s.cli, retired, moderation.RevokeOptions{Confirm: retired})
		return err
	})

	tor := &setSpec{title: "Tor Project", domains: []string{"torproject.org"}, strategy: "disorder", ttl: 4}
	s.share(11.0, author(), tor, nil)
	s.moderate(11.3, moderation.ActionApprove, "", tor)
	s.votes(tor, 6, 0.5, 11.4, 44.8)

	reddit := &setSpec{title: "Reddit", domains: []string{"reddit.com", "redd.it", "redditmedia.com", "redditstatic.com"}, strategy: "combo", ttl: 6}
	s.share(12.0, author(), reddit, nil)
	s.moderate(12.4, moderation.ActionApprove, "", reddit)
	s.votes(reddit, 18, 0.7, 12.5, 44.8)
	s.key(12.5, ytFixer, moderation.ActionTrust, "")
	s.profile(12.6, ytFixer, "yt-fixer", "Keeps the YouTube sets current.", "")

	twitch := &setSpec{title: "Twitch", domains: []string{"twitch.tv", "ttvnw.net", "jtvnw.net"}, strategy: "tls", ttl: 6}
	s.share(13.0, author(), twitch, nil)
	s.moderate(13.4, moderation.ActionApprove, "", twitch)
	s.votes(twitch, 16, 0.75, 13.5, 44.8)
	s.report(40.3, twitch, w.on("9198"), "Chat loads, video does not.")

	soundcloud := &setSpec{title: "SoundCloud", domains: []string{"soundcloud.com", "sndcdn.com"}, strategy: "tcp", ttl: 2}
	s.share(14.0, author(), soundcloud, nil)
	s.moderate(14.5, moderation.ActionApprove, "", soundcloud)
	s.votes(soundcloud, 5, 0.3, 14.6, 22)
	s.moderate(22.3, moderation.ActionHide, reasonBroken, soundcloud)

	s.key(15.0, flooder, moderation.ActionBan, "Vote flooding from one network.")
	s.key(19.0, flooder, moderation.ActionUnban, "")

	notion := &setSpec{title: "Notion", domains: []string{"notion.so", "notion.site"}, strategy: "tls", ttl: 7}
	s.share(16.0, author(), notion, nil)
	s.moderate(16.4, moderation.ActionApprove, "", notion)
	s.votes(notion, 6, 0.5, 16.5, 36)
	s.report(36.1, notion, w.on("12389"), "Pages never finish loading.")
	s.report(36.6, notion, w.on("8359"), "Broken since the last update.")
	s.report(37.2, notion, w.on("58224"), "Does not work on TCI.")

	whatsapp := &setSpec{title: "WhatsApp Web", domains: []string{"web.whatsapp.com", "whatsapp.net"}, strategy: "oob", ttl: 6}
	s.share(18.0, quitter, whatsapp, nil)
	s.moderate(18.4, moderation.ActionApprove, "", whatsapp)
	s.votes(whatsapp, 10, 0.7, 18.5, 33)
	s.on(33.2, func() error {
		_, err := s.mod.Withdraw(s.ctx, s.console, whatsapp.pub.setID, reasonWithdraw)
		return err
	})

	grok := &setSpec{title: "Grok", domains: []string{"grok.com", "x.ai"}, strategy: "extsplit", ttl: 6}
	s.share(20.0, ytFixer, grok, nil)
	s.moderate(20.3, moderation.ActionApprove, "", grok)
	s.votes(grok, 12, 0.7, 20.4, 44.8)
	grok2 := grok.next(func(p *setSpec) {
		p.ttl = 4
		p.description = "Lower TTL for MegaFon mobile."
	})
	s.share(38.0, ytFixer, grok2, nil)

	spam := &setSpec{title: "FREE VPN 100% WORKING ALL SITES", description: "best unblock join our channel", domains: []string{"regexp:.*"}, strategy: "tls", ttl: 8}
	s.share(21.0, spammer, spam, nil)
	s.moderate(21.5, moderation.ActionReject, reasonSpam, spam)
	spam2 := &setSpec{title: "UNBLOCK EVERYTHING FAST", description: "subscribe for more", domains: []string{"google.com", "yandex.ru", "vk.com", "mail.ru", "ok.ru"}, strategy: "oob", ttl: 8}
	s.share(22.0, spammer, spam2, nil)
	s.key(22.6, spammer, moderation.ActionBan, reasonBanSpam)

	patreon := &setSpec{title: "Patreon", domains: []string{"patreon.com", "patreonusercontent.com", "google-analytics.com", "doubleclick.net"}, strategy: "tls", ttl: 6}
	s.share(24.0, author(), patreon, nil)
	s.moderate(24.4, moderation.ActionReject, reasonTooBroad, patreon)

	ytFork := &setSpec{title: "YouTube (mobile networks)", description: "TTL 4 and a TCP split; better on MegaFon LTE.", domains: append([]string{"youtubei.googleapis.com"}, youtube...), strategy: "tcp", ttl: 4}
	s.share(25.0, megafon, ytFork, yt)
	s.moderate(25.5, moderation.ActionApprove, "", ytFork)
	s.votes(ytFork, 14, 0.8, 25.6, 44.8)

	ads := &setSpec{title: "Block ad trackers", domains: []string{"doubleclick.net", "googlesyndication.com", "adservice.google.com"}, strategy: "none", tune: func(set *config.SetConfig) {
		set.Faking.SNI = false
		set.Routing.Enabled = true
		set.Routing.Mode = config.RoutingModeBlock
	}}
	s.share(26.0, author(), ads, nil)
	s.moderate(26.5, moderation.ActionApprove, "", ads)
	s.votes(ads, 5, 0.8, 26.6, 44.8)

	s.report(27.0, yt, w.on("12389"), "Stopped working on Rostelecom since Tuesday.")
	yt2 := yt.next(func(p *setSpec) {
		p.strategy = "combo"
		p.ttl = 5
		p.description = "Combo split after the Rostelecom change; v1 stopped working there."
	})
	s.share(30.0, ytFixer, yt2, nil)
	s.moderate(30.3, moderation.ActionApprove, "", yt2)
	s.settleVersionReports(30.5, yt, moderation.ActionResolve, "Fixed in version 2.")
	s.votes(yt2, 30, 0.92, 30.4, 44.8)

	cf := &setSpec{title: "Cloudflare-hosted sites", description: "Targets AS13335 instead of domains.", asns: []string{"13335"}, strategy: "tls", ttl: 5}
	s.share(39.0, author(), cf, nil)
	blanket := &setSpec{title: "Everything in category-ru-blocked", geosite: []string{"category-ru-blocked"}, strategy: "combo", ttl: 6}
	s.share(40.0, author(), blanket, nil)
	ntc := &setSpec{title: "ntc.party", domains: []string{"ntc.party"}, strategy: "firstbyte", ttl: 5}
	s.share(41.0, author(), ntc, nil)
	medium := &setSpec{title: "Medium", domains: []string{"medium.com"}, strategy: "tls", ttl: 6}
	s.share(42.0, author(), medium, nil)
	rutor := &setSpec{title: "Rutor", description: "Pins the tracker address because DNS answers are poisoned.", domains: []string{"rutor.info", "rutor.is"}, strategy: "tls", ttl: 6, tune: func(set *config.SetConfig) {
		set.DNS.Enabled = true
		set.DNS.TargetDNS = "1.1.1.1"
		set.DNS.Pins = map[string][]string{"rutor.info": {"193.46.255.29"}}
	}}
	s.share(42.5, author(), rutor, nil)
	steam := &setSpec{title: "Steam Community", domains: []string{"steamcommunity.com", "steampowered.com"}, strategy: "disorder", ttl: 6}
	s.share(43.0, author(), steam, nil)
	s.report(43.5, steam, qa, "Test report from the QA router.")
	spotify := &setSpec{title: "Spotify", domains: []string{"spotify.com", "scdn.co", "spotifycdn.com"}, strategy: "combo", ttl: 6}
	s.share(44.2, author(), spotify, nil)
}

func (s *seeder) summary(layout hubdata.Layout, id *hubwire.Identity) error {
	counts := make(map[string]int)
	for _, status := range []string{hubwire.SetStatusActive, hubwire.SetStatusPending, hubwire.SetStatusHidden, hubwire.SetStatusRejected} {
		versions, err := s.st.VersionsByStatus(s.ctx, status)
		if err != nil {
			return err
		}
		counts[status] = len(versions)
	}
	mirrors, err := s.st.Mirrors(s.ctx)
	if err != nil {
		return err
	}
	latest := s.builder.Latest()
	fmt.Printf("seeded %s with %d days of history (hub key %s)\n", layout.Root, s.days, id.KeyID())
	fmt.Printf("  set versions: %d active, %d pending, %d hidden, %d rejected\n", counts[hubwire.SetStatusActive], counts[hubwire.SetStatusPending], counts[hubwire.SetStatusHidden], counts[hubwire.SetStatusRejected])
	fmt.Printf("  records: %d shares, %d votes, %d reports, %d mirror announcements\n", s.stats[hubwire.RecordShare], s.stats[hubwire.RecordVote], s.stats[hubwire.RecordReport], s.stats[hubwire.RecordMirror])
	fmt.Printf("  mirrors: %d, builds: %d\n", len(mirrors), s.stats["builds"])
	if latest != nil {
		fmt.Printf("  published %s with %d sets\n", latest.Manifest.Catalogue.File, len(latest.Catalogue.Sets))
	}
	if os.Getenv("B4HUB_ADMIN_PASSWORD") == "" {
		fmt.Println("the console needs a password: B4HUB_ADMIN_PASSWORD=<password> make hub-run")
	}
	return nil
}
