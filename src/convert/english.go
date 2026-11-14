package convert

import (
	"fmt"
	"regexp"
	"strings"
)

var reasonEnglish = map[string]string{
	"proxyRuntime":                "Configures the byedpi proxy process itself. b4 works on packets in the firewall, so there is nothing to carry over.",
	"resolverBehaviour":           "Controls how the byedpi proxy handles hostnames in incoming requests. It has no counterpart in b4.",
	"autoRetryTiming":             "Tunes byedpi's own retry timing. b4 escalation uses its own RST threshold and time window instead.",
	"globalTTL":                   "Sets the TTL of every outgoing connection. b4 only changes the TTL of fake packets.",
	"processControl":              "Process management for the source daemon.",
	"noEquivalent":                "b4 has no equivalent for this option.",
	"profileCopy":                 "Replays another profile's options. Expand it in the source command line and convert again.",
	"unmappedTarget":              "Recognized, but the converter has no rule for it yet.",
	"strayArgument":               "Not an option. Ignored.",
	"unknownOption":               "Not a known option for this tool and version.",
	"ambiguousOption":             "Abbreviated option name matches more than one option.",
	"missingValue":                "The option needs a value but none was given.",
	"unexpectedValue":             "The option takes no value.",
	"badValue":                    "The value could not be parsed: {{detail}}",
	"noIPv6Mapped":                "IPv6 is excluded, so the set is limited to IPv4.",
	"noUDPUpstream":               "UDP forwarding was disabled in the source. b4 handles UDP per set instead.",
	"delayMapped":                 "Became the inter-packet delay between fragments.",
	"fakeSNIMapped":               "b4 generates a ClientHello for this hostname and uses it as the fake packet.",
	"fakeSNINormalised":           "\"{{from}}\" is not a hostname. Read as {{to}}, which is what b4 generates the fake ClientHello for.",
	"fakeSNINotAHost":             "This value is not a hostname, so no fake payload could be generated from it.",
	"hostsMapped":                 "{{count}} domain(s) became the targets of this set.",
	"hostsFileUnresolved":         "Reads the host list from {{path}}, a file on the machine that ran the source tool, which b4 cannot read. Add the domains to the set's targets.",
	"ipsMapped":                   "The addresses became the IP targets of this set.",
	"ipsetFileUnresolved":         "Reads the address list from {{path}}, a file on the machine that ran the source tool, which b4 cannot read. Add the addresses to the set's targets.",
	"portFilterMapped":            "Became the destination port filter of the set.",
	"protoAsPorts":                "b4 selects traffic by port rather than by protocol, so the protocol filter became a port filter.",
	"protoUDPOnly":                "This profile is UDP only, so the set carries UDP handling and no TCP strategy.",
	"udpFakeCount":                "Became the number of fake UDP packets b4 sends.",
	"tlsRecMapped":                "Became TLS record splitting at this position.",
	"disoobNoOOB":                 "Disorder with an out-of-band byte is not available in b4. The disorder part is kept, the OOB byte is dropped.",
	"oobMapped":                   "Became out-of-band splitting at this position, using byedpi's default OOB byte.",
	"oobDroppedForCombo":          "A set uses one splitting method at a time, and another one was selected for this profile. The OOB split is dropped.",
	"singleStrategyOnly":          "A set uses one splitting method at a time, and another one was selected for this profile.",
	"oobByteMapped":               "Became the out-of-band byte.",
	"repeatsWithoutSkip":          "The repeat count has no step, so every repetition lands on the same offset. This does nothing in byedpi either, and nothing was carried over.",
	"repeatsUnsupported":          "Repeats the split {{repeats}} times with a step of {{skip}} bytes. b4 splits at fixed points instead, so only the first position was kept.",
	"sniMiddleMapped":             "Splits in the middle of the hostname.",
	"sniAnchorApproximated":       "Anchored at {{position}}. b4 splits in the middle of the hostname, which is the closest equivalent.",
	"httpHostAnchor":              "Anchored on the HTTP Host header. b4 splits on the TLS hostname instead.",
	"packetAnchorApproximated":    "Anchored at {{position}} inside the packet. b4 splits relative to the hostname instead.",
	"negativeOffset":              "Counts backwards from the end of the packet. b4 counts from the start of the TLS payload, so hostname splitting was used instead.",
	"zeroOffset":                  "An offset of 0 means no split.",
	"fixedPositionIgnored":        "This splitting method uses hostname-relative split points in b4, so the fixed offset was dropped.",
	"positionClamped":             "The offset is beyond the range b4 accepts and was clamped.",
	"fixedPositionMapped":         "Became a fixed split offset.",
	"requiresFake":                "Only affects fake packets, and this profile sends none.",
	"fakeMapped":                  "Enables fake packets before the real ClientHello. byedpi places the fake at {{position}}; b4 sends it as a separate packet with a low TTL.",
	"fakeTTLMapped":               "Became the TTL of the fake packets.",
	"md5sigMapped":                "Adds a TCP MD5 option to the fake packet.",
	"fakeDataMapped":              "Saved byte for byte as a payload file, which the web interface lists under Settings, Payloads, and the set's fakes carry it.",
	"fakeDataFileUnresolved":      "Reads the fake payload from {{path}}, a file on the machine that ran the source tool, which b4 cannot read. A built-in payload is used instead.",
	"fakeTLSModMapped":            "Randomizes the fake ClientHello.",
	"fakeTLSModUnsupported":       "This fake ClientHello modification has no counterpart in b4.",
	"fakeOffsetUnsupported":       "Shifts where the fake data starts. b4 sends the fake as a whole packet, so there is no offset to set.",
	"ipOptUnsupported":            "Adds IP options to the fake packet. b4 has no equivalent.",
	"dropSackMapped":              "Removes the SACK option from TCP packets.",
	"httpTamper":                  "Rewrites HTTP request headers. b4 does not modify plaintext HTTP.",
	"roundUnsupported":            "Selects which request in a connection to act on. b4 uses a packet limit per connection instead.",
	"escalateMapped":              "Starts a new profile that runs when the previous one fails. Became an escalation link between the two sets.",
	"escalateTriggersPartial":     "Became an escalation link between the two sets. b4 escalates on repeated RSTs; the HTTP redirect and TLS error triggers have no counterpart.",
	"autoNoneAsSeparateSet":       "Starts a profile that runs when the previous one does not match. It became a separate set rather than an escalation step.",
	"firstByteMapped":             "Became a split after the first byte.",
	"splitPointsCollapsed":        "The source configuration splits at {{count}} positions here. A b4 set has a fixed set of split points and cannot reproduce an arbitrary ladder, so the closest available ones were selected.",
	"profileWithoutDesync":        "This profile carries no bypass options, so it passes traffic through unchanged - the same thing the source tool does when it falls back to this group.",
	"unaccountedOption":           "Recognized, but nothing in the converted set came out of it. Report the command line so a rule can be added.",
	"udpFoldedIntoSet":            "The UDP-only profile carries no targets of its own, so its UDP handling was folded into this set. Kept as a separate set it would have matched the same TCP traffic first and passed it through untouched.",
	"autoNoneEntrySet":            "Starts the profile that handles everything the previous one did not. It is the entry set here.",
	"desyncModesMapped":           "The desync modes became the strategy of this set.",
	"desyncModesDropped":          "b4 has no equivalent for {{dropped}}, so that part of the mode list was dropped.",
	"desyncModesEmpty":            "None of these modes produced anything b4 can apply.",
	"foolingMapped":               "The fooling method became the way b4 makes the fake packet unusable to the server.",
	"foolingPartial":              "A set has one faking strategy. {{kept}} was kept, {{dropped}} dropped.",
	"fakeRepeatsMapped":           "Became the number of fake packets sent before the real one.",
	"udpFakeRepeatsMapped":        "Became the number of fake UDP packets.",
	"udpFakeTTLMapped":            "Became the set's Fake TTL, which b4's UDP fakes use.",
	"udpFakeTTLShared":            "A b4 set has one Fake TTL for its TCP and UDP fakes, and the TCP profile this one was folded into sets it.",
	"duplicateMapped":             "Became packet duplication.",
	"seqOvlMapped":                "Became sequence overlap.",
	"seqOvlPatternMapped":         "The overlap pattern was read as raw bytes; a TLS 1.2 header is used when it cannot be parsed.",
	"wsSizeMapped":                "Became Real TCP window (wssize), the set's tcp.wssize settings: b4 writes this window into the connection's own packets until the ClientHello has gone out, and lowers the window scale to this value. A lowered scale slows the whole connection. zapret skips the option in a profile with a host list, unless the list is an automatic one, while b4 applies it to a set with domains once it knows their addresses.",
	"wsSizeApproximated":          "Became Real TCP window (wssize), the set's tcp.wssize settings, with the window only. zapret lowers the window scale to 0 when none is given, while a scale of 0 in b4 leaves the client's scale alone, so after the first packet the server may send more than this many bytes at once.",
	"wsSizeOff":                   "A window of 0 leaves the window unchanged, which is the default.",
	"wsSizeWithDuplicate":         "b4's Real TCP window (wssize) does not work in a set that also uses Packet Duplication, and this profile has --dup, so the window was not carried over.",
	"wsSizeCutoffUnsupported":     "b4 stops rewriting the window after the ClientHello or after the first data packet, and has no cutoff by packet count. By default zapret also stops at the first HTTP request or TLS ClientHello, whatever this option says.",
	"wsSizeForcedCutoffOn":        "1 is the default: the window rewrite stops at the first HTTP request or TLS ClientHello, as it does in b4.",
	"wsSizeForcedCutoffOff":       "b4 always stops rewriting the window at the ClientHello, or at the first packet of other data, and cannot keep the small window for the rest of the connection.",
	"skipMapped":                  "The profile is marked skipped, so the set is created disabled.",
	"newProfileAsSet":             "Starts a new profile. Each one became a separate set, in the same order.",
	"sniExtMapped":                "Splits right before the SNI extension.",
	"sniExtApproximated":          "Anchored on the SNI extension with an offset. b4 splits exactly at the extension or in the middle of the hostname, so the offset was dropped.",
	"ipFragMapped":                "Became IP-layer fragmentation.",
	"ipVersionMapped":             "Limits the set to one IP version.",
	"l7QUICMapped":                "b4 selects UDP traffic by QUIC parsing rather than by an L7 filter.",
	"l7FilterApproximated":        "b4 selects traffic by port and by domain rather than by an L7 protocol filter.",
	"fakeQUICApproximated":        "b4 generates a fresh QUIC Initial per packet instead of replaying a captured one. To use the original, upload the file under Settings, Payloads in the web interface.",
	"fakeBuiltinPayload":          "Uses the built-in fake ClientHello.",
	"fakeHexInvalid":              "Not a valid hex payload: after 0x it needs an even number of hex digits. The fake carries another payload.",
	"fakeHexPayloadOverridden":    "Another option in this profile supplies the fake payload, so this hex payload was not used.",
	"tcpOnlyFakeOption":           "Only affects TCP fakes, and this profile handles UDP.",
	"tcpOnlyOption":               "Only affects TCP, and this profile handles only UDP, so zapret never applies it.",
	"udpFakeUnused":               "Only affects fake UDP packets, and this profile sends none.",
	"udpDefaultFakes":             "The source profile does nothing to UDP, but this set still sends b4's default fake UDP packets, as every new set does. To leave UDP alone, set udp.mode of the set to off.",
	"negatedPortFilter":           "b4 port filters list the ports to act on and cannot express a negation.",
	"excludeListUnsupported":      "b4 has no exclusion list. Remove the entries from the set targets instead.",
	"autoTTLUnsupported":          "Picks the fake TTL from the measured hop count. b4 uses a fixed TTL, so set it by hand or let Discovery find it.",
	"origPacketTTL":               "Changes the TTL of the real packets. b4 only changes the TTL of fake ones.",
	"packetWindowUnsupported":     "Limits the desync to a range of packets in the connection. b4 uses a per-connection packet limit instead.",
	"ipFragPosition":              "Sets the IP fragmentation offset. b4 chooses it itself.",
	"dupKnobUnsupported":          "b4 duplication sends plain copies and has no separate TTL, fooling or window of its own.",
	"hostlistAuto":                "zapret builds this host list itself by watching for blocks. b4 has Watchdog and Discovery for that.",
	"httpFakePayload":             "A fake payload for plaintext HTTP. b4 fakes TLS and QUIC only.",
	"obsoleteOption":              "Marked obsolete in zapret itself.",
	"engineDetail":                "Configures how nfqws attaches to the firewall. b4 manages that itself.",
	"anyProtocol":                 "Acts on any data packet rather than only HTTP and TLS. On the TCP side b4 works from the TLS ClientHello and has no counterpart, and this profile sends no UDP fake for the option to widen.",
	"ssidFilter":                  "Applies the profile only on certain Wi-Fi networks. b4 has no equivalent.",
	"windowsOnly":                 "Only applies to the Windows build of zapret.",
	"shadowedByEarlierSet":        "{{other}} already claims {{domain}}. b4 picks one set per destination and does not fall back to the next, so this set would never run. It is created disabled - give it its own domains to use it.",
	"ipv6OnlyKnob":                "Sets the hop limit for IPv6 desync packets separately. b4 uses one TTL for both address families.",
	"fakeTLSModPartial":           "Kept what b4 has an equivalent for. Dropped: {{dropped}}.",
	"templatePlaceholder":         "A placeholder from the configuration template. It is substituted before the daemon runs, so there is nothing to convert.",
	"udpOnlySetNotProtocolScoped": "This profile only filters UDP, but a b4 set is not limited to one protocol. Given domains, it will also claim them for TCP, taking them away from the TCP sets. Leave it without targets unless it is the only set for those domains.",
	"profileNotUnderstood":        "{{count}} option(s) in this profile were not recognized, and nothing else in it produced a strategy. The set is created disabled so it cannot claim domains while doing nothing.",
	"fakeTTLOriginal":             "A TTL of 0 means the fake keeps the TTL of the real packet, so b4 leaves it alone too.",
	"udpFakeTTLOriginal":          "A TTL of 0 means the fake keeps the TTL of the real packet. b4's UDP fakes always use the set's Fake TTL instead.",
	"repeatedOptionCombined":      "The option appears {{count}} times in this profile and every value applies; they were combined into one setting.",
	"supersededByLater":           "Given again later as {{winner}}, and that is the value that applies. This one has no effect.",
	"extraSplitPositionDropped":   "A set splits at one point for the strategy it uses, and another option already supplied it. This position was dropped.",
	"everyPortMatched":            "Acts on every port, which is what a b4 set does when its port filter is empty.",
	"seqOvlLengthUnsupported":     "b4 overlaps a fixed number of bytes counted from the start of the payload, so a relative or non-positive length has no counterpart.",
	"fakeZeroPayload":             "Became b4's all-zero fake payload.",
	"fakeZeroPayloadOverridden":   "b4 has an all-zero fake payload, but another option in this profile supplies the payload instead, so that one was used.",
	"fakeUnknownUDPApproximated":  "Became the UDP fake payload. b4 carries one payload per set and generates a fresh QUIC Initial per packet instead of replaying a captured one.",
	"anyProtocolUDP":              "b4's UDP fakes are not limited to QUIC to begin with: other UDP the set matches gets them too, call packets aside. So there is nothing to switch on. b4 has no counterpart on the TCP side, where it acts on TLS.",
	"anyProtocolOff":              "A value of 0 leaves the option off, which is the default, so there is nothing to carry over.",
	"detachedOptionValue":         "{{option}} takes its value written with =, so this word is not read as its value. nfqws ignores it in the same way.",
	"zapret2Lua":                  "zapret2 keeps its bypass strategies in Lua scripts rather than in options, and b4 cannot read them.",
	"tlsRecAnchorApproximated":    "Splits the TLS record at an offset from the hostname. b4 splits its TLS record around the hostname itself, so the offset was dropped.",
	"oobAnchorApproximated":       "Sends the out-of-band byte at an offset from the hostname. b4 sends it in the middle of the hostname, so the offset was dropped.",
	"requiresOOB":                 "Only affects out-of-band splitting, and this set uses another splitting method.",
	"fakeSplitBoundaryLost":       "The fake position also splits the request upstream. This set sends fake packets but passes the real request whole, since no other option asked for a split.",
	"escalationTargetIdentical":   "This set ends up identical to {{other}}, so escalating from it would change nothing. The escalation link was removed.",
	"portFilterDroppedOnFallback": "A set reached only through escalation carries no port filter of its own, so the ports from this profile were dropped.",
	"positionInRange":             "Falls inside the range of offsets the set splits at. b4 picks one offset from that range per packet rather than splitting at all of them.",
	"l7FilterMapped":              "The set handles only calls on its UDP ports, to any address, as this profile does. Such a set takes no domain or IP targets.",
	"l7FilterPartial":             "The set handles only calls on its UDP ports, of the kinds b4 recognizes. b4 does not recognize {{dropped}}, so the set leaves those packets alone.",
	"l7FilterNeedsPorts":          "b4 sees UDP only on port 443 and on the ports sets list, and this profile lists no UDP ports, so the protocol filter was not carried over and the set is created disabled. To use it, set udp.dport_filter to the ports, such as 19294-19344,50000-50100 for Discord, set udp.filter_l7 to the kinds of call it should handle and enable the set.",
	"l7FilterWithTargets":         "Only a set without targets can handle only calls, and this profile has its own host or address list. That list stays the set's target list, so the set acts on UDP traffic to those targets, and with udp.filter_stun on it passes STUN requests and Discord voice unchanged.",
	"l7TCPPortsUnused":            "zapret recognizes Discord and STUN packets only in UDP, so this profile never applies to TCP. The set takes no TCP port filter.",
	"l7FilterNotUDPOnly":          "b4 handles only calls in a set that covers UDP alone, and this profile also covers TCP, so the set was built from its ports and targets instead.",
	"l7FakeAsSTUN":                "b4 does not use this payload: the set sends a generated STUN request in its fake packets instead.",
	"l7FakeUnused":                "This set handles only calls and sends a generated STUN request in its fake packets, so b4 never sends this fake.",
	"l7DecoySTUN":                 "When no fake payload is given, zapret sends 64 zero bytes as the fake before the packets the profile selects by protocol. This set sends a generated STUN request instead, because on some networks zero-filled fake packets cut Discord voice calls short.",
}

var warningEnglish = map[string]string{
	"ambiguousVersion":      "The version could not be determined from the options used, and {{flags}} mean different things across byedpi versions. Pass the version explicitly if the result looks wrong.",
	"needsTargets":          "None of the {{sets}} converted sets has domains or IPs. {{tool}} applies its profiles to everything it sees, while a b4 set only acts on the targets it is given. Add domains to the sets that should act; a set that is reached only through escalation does not need any.",
	"udpDisabledUpstream":   "UDP was disabled in the source configuration. The UDP settings of the converted sets are left at their defaults.",
	"profilesAsSets":        "The source configuration has {{count}} profiles. Each one became a separate set, in the same order. Where the source retries a failed connection through the next profile, the sets are linked by escalation and only the first one takes targets.",
	"shadowedSets":          "{{count}} set(s) claim domains that an earlier set already claims. {{tool}} picks a profile per packet using its port and protocol filters, while b4 picks one set per destination by domain and does not fall through to the next. Profiles that share a host list but differ only by port cannot all apply. The later ones are created disabled - give each the domains it should handle, or delete the ones you do not need.",
	"nothingRecognized":     "None of the options that actually do the bypass were recognized, so no set would carry a strategy. This looks like a {{tool}} variant with a different option syntax. Send the configuration to the b4 issue tracker so support can be added.",
	"sourceLayout":          "The text was read as a {{layout}} configuration file. The command line was rebuilt from {{vars}} in the order that launcher uses, so the profiles match what the source tool actually runs.",
	"foreignDaemonVars":     "{{vars}} configure a different daemon than {{tool}} and were left out of the conversion. Convert them separately if you need them.",
	"alternativeStrategies": "The source file has {{count}} commented-out alternative strategies for {{vars}}. They were not converted - uncomment the one that should be used and import the text again to convert it instead.",
	"setsWithoutStrategy":   "{{count}} set(s) carry no bypass options at all, so they pass traffic through unchanged.",
	"unusedVars":            "{{vars}} carry options but are not variables {{tool}} reads, so they were left out of the conversion. Rename them or import their contents on their own if they should be converted.",
	"catchAllProfile":       "{{count}} profile(s) have no host list of their own. In {{tool}} such a profile handles everything the others did not match; a b4 set with no targets matches nothing by domain or address, so it is created disabled. A set with a port filter is the exception: it would act on every connection on those ports.",
	"sharedDomainsUnused":   "None of the sets took the shared domains: each one has its own host list, address list or domains, is meant to handle only calls, or is reached only through escalation. Add the domains to the targets of the set that should handle them.",
	"sharedDomainsNoSet":    "None of the converted sets takes domains: each is meant to handle only calls or is reached only through escalation, so the shared domains were not used.",
}

var englishPlaceholder = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

func englishText(catalog map[string]string, code string, params map[string]any) string {
	text, ok := catalog[code]
	if !ok {
		return code
	}
	return englishPlaceholder.ReplaceAllStringFunc(text, func(m string) string {
		name := englishPlaceholder.FindStringSubmatch(m)[1]
		v, ok := params[name]
		if !ok {
			return m
		}
		return englishValue(v)
	})
}

func englishValue(v any) string {
	switch list := v.(type) {
	case []string:
		return strings.Join(list, ", ")
	case []any:
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func (n Note) English() string {
	return englishText(reasonEnglish, n.Reason, n.Params)
}

func (w Warning) English() string {
	return englishText(warningEnglish, w.Code, w.Params)
}

var disabledNote = map[string]string{
	DisabledSkipped:       "skipMapped",
	DisabledShadowed:      "shadowedByEarlierSet",
	DisabledNotUnderstood: "profileNotUnderstood",
	DisabledNeedsPorts:    "l7FilterNeedsPorts",
}

var disabledEnglish = map[string]string{
	DisabledSkipped:       "The profile is marked skipped in the source, so the set is created disabled.",
	DisabledShadowed:      "An earlier set of this import already claims one of its domains. b4 picks one set per destination, so this set would never run until it has domains of its own.",
	DisabledNotUnderstood: "Options in its profile were not recognized, and nothing else in it produced a strategy, so the set would claim domains while doing nothing.",
	DisabledNoStrategy:    "Its profile carries no bypass option that b4 could convert, so the set would pass traffic through unchanged.",
	DisabledNeedsPorts:    "It handles only calls, and its profile lists no UDP ports. b4 sees UDP only on port 443 and on the ports sets list, so the set needs udp.dport_filter and udp.filter_l7 before it is enabled.",
	DisabledNeedsTargets:  "It has no domains or addresses. Nothing else keeps it disabled.",
	DisabledUnreached:     "It is a fallback that no other set escalates to, and a fallback takes no targets of its own.",
}

func (r *Result) DisabledEnglish(profile int) string {
	if profile < 0 || profile >= len(r.Plan) {
		return ""
	}
	why := r.Plan[profile].DisabledFor
	if reason, ok := disabledNote[why]; ok {
		for _, n := range r.Notes {
			if n.Profile == profile && n.Reason == reason {
				return n.English()
			}
		}
	}
	return disabledEnglish[why]
}
