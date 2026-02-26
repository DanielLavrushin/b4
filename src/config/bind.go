package config

import "github.com/spf13/cobra"

func (c *Config) BindFlags(cmd *cobra.Command) {
	// Config path
	cmd.Flags().StringVar(&c.ConfigPath, "config", c.ConfigPath, "Path to config file")

	// Queue configuration
	cmd.Flags().IntVar(&c.Queue.StartNum, "queue-num", c.Queue.StartNum, "Netfilter queue number")
	cmd.Flags().IntVar(&c.Queue.Threads, "threads", c.Queue.Threads, "Number of worker threads")
	cmd.Flags().UintVar(&c.Queue.Mark, "mark", c.Queue.Mark, "Packet mark value (default 32768)")
	cmd.Flags().BoolVar(&c.Queue.IPv4Enabled, "ipv4", c.Queue.IPv4Enabled, "Enable IPv4 processing")
	cmd.Flags().BoolVar(&c.Queue.IPv6Enabled, "ipv6", c.Queue.IPv6Enabled, "Enable IPv6 processing")

	// System configuration
	cmd.Flags().IntVar(&c.System.Tables.MonitorInterval, "tables-monitor-interval", c.System.Tables.MonitorInterval, "Tables monitor interval in seconds (default 10, 0 to disable)")
	cmd.Flags().BoolVar(&c.System.Tables.SkipSetup, "skip-tables", c.System.Tables.SkipSetup, "Skip iptables/nftables setup on startup")

	// Logging configuration
	cmd.Flags().BoolVarP(&c.System.Logging.Instaflush, "instaflush", "i", c.System.Logging.Instaflush, "Flush logs immediately")
	cmd.Flags().BoolVar(&c.System.Logging.Syslog, "syslog", c.System.Logging.Syslog, "Enable syslog output")
	cmd.Flags().StringVar(&c.System.Logging.ErrorFile, "error-file", c.System.Logging.ErrorFile, "Path to error log file (empty disables)")

	// Web Server configuration
	cmd.Flags().IntVar(&c.System.WebServer.Port, "web-port", c.System.WebServer.Port, "Port for internal web server (0 disables)")
	
	// TLS configuration
	cmd.Flags().StringVar(&c.System.WebServer.TLSCert, "tls-cert", c.System.WebServer.TLSCert, "Path to TLS certificate file")
	cmd.Flags().StringVar(&c.System.WebServer.TLSKey, "tls-key", c.System.WebServer.TLSKey, "Path to TLS key file")
}
