package config

import (
	"github.com/spf13/cobra"
)

type CLIOverrides struct {
	QueueNum        int
	Threads         int
	Mark            uint
	IPv4Enabled     bool
	IPv6Enabled     bool
	MonitorInterval int
	SkipSetup       bool
	Instaflush      bool
	Syslog          bool
	LogDir          string
	WebPort         int
}

func (c *Config) BindFlags(cmd *cobra.Command, o *CLIOverrides) {
	cmd.Flags().StringVar(&c.ConfigPath, "config", c.ConfigPath, "Path to config file")

	d := DefaultConfig
	cmd.Flags().IntVar(&o.QueueNum, "queue-num", d.Queue.StartNum, "Netfilter queue number")
	cmd.Flags().IntVar(&o.Threads, "threads", d.Queue.Threads, "Number of worker threads")
	cmd.Flags().UintVar(&o.Mark, "mark", d.Queue.Mark, "Packet mark value (default 32768)")
	cmd.Flags().BoolVar(&o.IPv4Enabled, "ipv4", d.Queue.IPv4Enabled, "Enable IPv4 processing")
	cmd.Flags().BoolVar(&o.IPv6Enabled, "ipv6", d.Queue.IPv6Enabled, "Enable IPv6 processing")
	cmd.Flags().IntVar(&o.MonitorInterval, "tables-monitor-interval", d.System.Tables.MonitorInterval, "Tables monitor interval in seconds (default 10, 0 to disable)")
	cmd.Flags().BoolVar(&o.SkipSetup, "skip-tables", d.System.Tables.SkipSetup, "Skip iptables/nftables setup on startup")
	cmd.Flags().BoolVarP(&o.Instaflush, "instaflush", "i", d.System.Logging.Instaflush, "Flush logs immediately")
	cmd.Flags().BoolVar(&o.Syslog, "syslog", d.System.Logging.Syslog, "Enable syslog output")
	cmd.Flags().StringVar(&o.LogDir, "log-dir", d.System.Logging.Directory, "Directory for b4 log files (empty disables file logging)")
	cmd.Flags().IntVar(&o.WebPort, "web-port", d.System.WebServer.Port, "Port for internal web server (0 disables)")
}

var persistedOverrides struct {
	snapshot  *Config
	fields    map[string]bool
	overrides CLIOverrides
}

func (c *Config) ApplyCLIOverrides(cmd *cobra.Command, o *CLIOverrides) {
	fields := make(map[string]bool)
	changed := func(name string) bool {
		if cmd.Flags().Changed(name) {
			fields[name] = true
			return true
		}
		return false
	}

	snapshot := &Config{Queue: c.Queue, System: c.System}

	if changed("queue-num") {
		c.Queue.StartNum = o.QueueNum
	}
	if changed("threads") {
		c.Queue.Threads = o.Threads
	}
	if changed("mark") {
		c.Queue.Mark = o.Mark
	}
	if changed("ipv4") {
		c.Queue.IPv4Enabled = o.IPv4Enabled
	}
	if changed("ipv6") {
		c.Queue.IPv6Enabled = o.IPv6Enabled
	}
	if changed("tables-monitor-interval") {
		c.System.Tables.MonitorInterval = o.MonitorInterval
	}
	if changed("skip-tables") {
		c.System.Tables.SkipSetup = o.SkipSetup
	}
	if changed("instaflush") {
		c.System.Logging.Instaflush = o.Instaflush
	}
	if changed("syslog") {
		c.System.Logging.Syslog = o.Syslog
	}
	if changed("log-dir") {
		c.System.Logging.Directory = o.LogDir
	}
	if changed("web-port") {
		c.System.WebServer.Port = o.WebPort
	}

	if len(fields) > 0 {
		persistedOverrides.snapshot = snapshot
		persistedOverrides.fields = fields
		persistedOverrides.overrides = *o
	} else {
		persistedOverrides.snapshot = nil
		persistedOverrides.fields = nil
		persistedOverrides.overrides = CLIOverrides{}
	}
}

func stripCLIOverrides(c *Config) *Config {
	snap := persistedOverrides.snapshot
	f := persistedOverrides.fields
	if snap == nil || len(f) == 0 {
		return c
	}
	ov := persistedOverrides.overrides

	clone := *c
	if f["queue-num"] && c.Queue.StartNum == ov.QueueNum {
		clone.Queue.StartNum = snap.Queue.StartNum
	}
	if f["threads"] && c.Queue.Threads == ov.Threads {
		clone.Queue.Threads = snap.Queue.Threads
	}
	if f["mark"] && c.Queue.Mark == ov.Mark {
		clone.Queue.Mark = snap.Queue.Mark
	}
	if f["ipv4"] && c.Queue.IPv4Enabled == ov.IPv4Enabled {
		clone.Queue.IPv4Enabled = snap.Queue.IPv4Enabled
	}
	if f["ipv6"] && c.Queue.IPv6Enabled == ov.IPv6Enabled {
		clone.Queue.IPv6Enabled = snap.Queue.IPv6Enabled
	}
	if f["tables-monitor-interval"] && c.System.Tables.MonitorInterval == ov.MonitorInterval {
		clone.System.Tables.MonitorInterval = snap.System.Tables.MonitorInterval
	}
	if f["skip-tables"] && c.System.Tables.SkipSetup == ov.SkipSetup {
		clone.System.Tables.SkipSetup = snap.System.Tables.SkipSetup
	}
	if f["instaflush"] && c.System.Logging.Instaflush == ov.Instaflush {
		clone.System.Logging.Instaflush = snap.System.Logging.Instaflush
	}
	if f["syslog"] && c.System.Logging.Syslog == ov.Syslog {
		clone.System.Logging.Syslog = snap.System.Logging.Syslog
	}
	if f["log-dir"] && c.System.Logging.Directory == ov.LogDir {
		clone.System.Logging.Directory = snap.System.Logging.Directory
	}
	if f["web-port"] && c.System.WebServer.Port == ov.WebPort {
		clone.System.WebServer.Port = snap.System.WebServer.Port
	}
	return &clone
}
