package main

import "github.com/daniellavrushin/b4/warmtuplecmd"

func init() {
	rootCmd.AddCommand(warmtuplecmd.NewCommand())
}
