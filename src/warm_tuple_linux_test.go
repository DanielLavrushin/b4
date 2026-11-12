package main

import "testing"

func TestWarmTupleCommandIsPartOfB4(t *testing.T) {
	c, args, err := rootCmd.Find([]string{"warm-tuple"})
	if err != nil || c == rootCmd || c.Name() != "warm-tuple" || len(args) != 0 || c.Parent() != rootCmd {
		t.Fatalf("native warm-tuple command is missing: %v %v", args, err)
	}
}
