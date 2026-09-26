package hubwire

type FieldClass int

const (
	Crosses FieldClass = iota
	Never
)

type Field struct {
	Class FieldClass
	Since string
}

const BaselineVersion = "1.82.0"

const (
	MaxPayloadBytes       = 16 * 1024
	MaxCustomPayloadBytes = 8 * 1024
	MaxSNIDomains         = 500
	MaxIPs                = 200
	MaxASNs               = 50
)

var Fields = map[string]Field{
	"id":      {Class: Never},
	"name":    {Class: Crosses},
	"enabled": {Class: Never},
	"hub":     {Class: Never},

	"tcp.conn_bytes_limit": {Class: Crosses},
	"tcp.seg2delay":        {Class: Crosses},
	"tcp.seg2delay_max":    {Class: Crosses},
	"tcp.syn_fake":         {Class: Crosses},
	"tcp.syn_fake_len":     {Class: Crosses},
	"tcp.syn_ttl":          {Class: Crosses},
	"tcp.drop_sack":        {Class: Crosses},
	"tcp.http_methodeol":   {Class: Crosses},
	"tcp.dport_filter":     {Class: Crosses},

	"tcp.incoming.mode":       {Class: Crosses},
	"tcp.incoming.min":        {Class: Crosses},
	"tcp.incoming.max":        {Class: Crosses},
	"tcp.incoming.fake_ttl":   {Class: Crosses},
	"tcp.incoming.fake_count": {Class: Crosses},
	"tcp.incoming.strategy":   {Class: Crosses},

	"tcp.desync.mode":        {Class: Crosses},
	"tcp.desync.ttl":         {Class: Crosses},
	"tcp.desync.count":       {Class: Crosses},
	"tcp.desync.post_desync": {Class: Crosses},

	"tcp.win.mode":   {Class: Crosses},
	"tcp.win.values": {Class: Crosses},

	"tcp.duplicate.enabled": {Class: Crosses},
	"tcp.duplicate.count":   {Class: Crosses},

	"tcp.ip_block_detect": {Class: Never},

	"tcp.rst_protection.enabled":       {Class: Crosses},
	"tcp.rst_protection.ttl_tolerance": {Class: Crosses},

	"udp.mode":              {Class: Crosses},
	"udp.fake_seq_length":   {Class: Crosses},
	"udp.fake_len":          {Class: Crosses},
	"udp.faking_strategy":   {Class: Crosses},
	"udp.fake_payload_file": {Class: Crosses},
	"udp.dport_filter":      {Class: Crosses},
	"udp.filter_quic":       {Class: Crosses},
	"udp.filter_stun":       {Class: Crosses},
	"udp.conn_bytes_limit":  {Class: Crosses},
	"udp.seg2delay":         {Class: Crosses},
	"udp.seg2delay_max":     {Class: Crosses},

	"fragmentation.strategy":            {Class: Crosses},
	"fragmentation.reverse_order":       {Class: Crosses},
	"fragmentation.strategy_pool":       {Class: Crosses},
	"fragmentation.tlsrec_pos":          {Class: Crosses},
	"fragmentation.tlsrec_pos_max":      {Class: Crosses},
	"fragmentation.middle_sni":          {Class: Crosses},
	"fragmentation.sni_position":        {Class: Crosses},
	"fragmentation.sni_position_max":    {Class: Crosses},
	"fragmentation.oob_position":        {Class: Crosses},
	"fragmentation.oob_position_max":    {Class: Crosses},
	"fragmentation.oob_char":            {Class: Crosses},
	"fragmentation.seq_overlap_pattern": {Class: Crosses},
	"fragmentation.seq_overlap_length":  {Class: Crosses},

	"fragmentation.combo.first_byte_split":       {Class: Crosses},
	"fragmentation.combo.extension_split":        {Class: Crosses},
	"fragmentation.combo.shuffle_mode":           {Class: Crosses},
	"fragmentation.combo.first_delay_ms":         {Class: Crosses},
	"fragmentation.combo.first_delay_ms_max":     {Class: Crosses},
	"fragmentation.combo.jitter_max_us":          {Class: Crosses},
	"fragmentation.combo.jitter_max_us_max":      {Class: Crosses},
	"fragmentation.combo.decoy_enabled":          {Class: Crosses},
	"fragmentation.combo.fake_per_segment":       {Class: Crosses},
	"fragmentation.combo.fake_per_seg_count":     {Class: Crosses},
	"fragmentation.combo.fake_per_seg_count_max": {Class: Crosses},

	"fragmentation.disorder.shuffle_mode":           {Class: Crosses},
	"fragmentation.disorder.min_jitter_us":          {Class: Crosses},
	"fragmentation.disorder.max_jitter_us":          {Class: Crosses},
	"fragmentation.disorder.fake_per_segment":       {Class: Crosses},
	"fragmentation.disorder.fake_per_seg_count":     {Class: Crosses},
	"fragmentation.disorder.fake_per_seg_count_max": {Class: Crosses},

	"faking.sni":                {Class: Crosses},
	"faking.ttl":                {Class: Crosses},
	"faking.strategy":           {Class: Crosses},
	"faking.seq_offset":         {Class: Crosses},
	"faking.sni_seq_length":     {Class: Crosses},
	"faking.sni_type":           {Class: Crosses},
	"faking.custom_payload":     {Class: Crosses},
	"faking.payload_file":       {Class: Crosses},
	"faking.payload_domain":     {Class: Crosses},
	"faking.tls_mod":            {Class: Crosses},
	"faking.timestamp_decrease": {Class: Crosses},
	"faking.tcp_md5":            {Class: Crosses},
	"faking.apply_ttl":          {Class: Crosses},
	"faking.md5_on_fake":        {Class: Crosses},
	"faking.fake_len_mode":      {Class: Crosses},

	"faking.sni_mutation.mode":           {Class: Crosses},
	"faking.sni_mutation.grease_count":   {Class: Crosses},
	"faking.sni_mutation.padding_size":   {Class: Crosses},
	"faking.sni_mutation.fake_ext_count": {Class: Crosses},
	"faking.sni_mutation.fake_snis":      {Class: Crosses},

	"targets.sni_domains":            {Class: Crosses},
	"targets.ip":                     {Class: Crosses},
	"targets.geosite_categories":     {Class: Crosses},
	"targets.geoip_categories":       {Class: Crosses},
	"targets.asns":                   {Class: Crosses, Since: "1.84.0"},
	"targets.source_devices":         {Class: Never},
	"targets.source_devices_exclude": {Class: Never},
	"targets.domain_only":            {Class: Crosses},
	"targets.tls":                    {Class: Crosses},
	"targets.ip_version":             {Class: Crosses},

	"dns.enabled":        {Class: Crosses},
	"dns.target_dns":     {Class: Never},
	"dns.doh_url":        {Class: Crosses},
	"dns.fragment_query": {Class: Crosses},
	"dns.strict":         {Class: Crosses},
	"dns.pins":           {Class: Crosses},

	"routing.enabled":           {Class: Crosses},
	"routing.mode":              {Class: Crosses},
	"routing.block_action":      {Class: Crosses},
	"routing.egress_interface":  {Class: Never},
	"routing.egress_ip":         {Class: Never},
	"routing.upstream":          {Class: Never},
	"routing.fwmark":            {Class: Never},
	"routing.table":             {Class: Never},
	"routing.source_interfaces": {Class: Never},
	"routing.ip_ttl_seconds":    {Class: Never},
	"routing.router_traffic":    {Class: Never},
	"routing.kill_switch":       {Class: Never},

	"escalate": {Class: Never},

	"mss_clamp.enabled": {Class: Crosses},
	"mss_clamp.size":    {Class: Crosses},

	"discovery": {Class: Never},
}

var silentStrips = map[string]bool{
	"id":      true,
	"enabled": true,
	"hub":     true,
}

func classOf(path string) (Field, bool) {
	if f, ok := Fields[path]; ok {
		return f, true
	}
	for i := len(path) - 1; i > 0; i-- {
		if path[i] != '.' {
			continue
		}
		if f, ok := Fields[path[:i]]; ok && f.Class == Never {
			return f, true
		}
	}
	return Field{}, false
}

func neverPaths() []string {
	out := make([]string, 0, len(Fields))
	for p, f := range Fields {
		if f.Class == Never {
			out = append(out, p)
		}
	}
	return out
}
