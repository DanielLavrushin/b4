package convert

import (
	"strings"
	"testing"
)

func TestExtractArgv_Sources(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"bare", "-Ku -a1", []string{"-Ku", "-a1"}},
		{"withBinary", "ciadpi -s1 -f-1", []string{"-s1", "-f-1"}},
		{"withPath", "/usr/bin/ciadpi -s1", []string{"-s1"}},
		{"withSudo", "sudo ciadpi -s1", []string{"-s1"}},
		{"execStart", "ExecStart=/opt/byedpi/ciadpi -s1 -t8", []string{"-s1", "-t8"}},
		{"shellVar", `NFQWS_OPT="--dpi-desync=fake --dpi-desync-ttl=3"`, []string{"--dpi-desync=fake", "--dpi-desync-ttl=3"}},
		{"exportVar", `export CIADPI_ARGS='-s1 -d2'`, []string{"-s1", "-d2"}},
		{"comment", "# my config\n-s1 -f-1", []string{"-s1", "-f-1"}},
		{"continuation", "-s1 \\\n-f-1", []string{"-s1", "-f-1"}},
		{"quotedValue", `-l ":GET / HTTP"`, []string{"-l", ":GET / HTTP"}},
		{"multiline", "-s1\n-f-1", []string{"-s1", "-f-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArgv(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractArgv_Empty(t *testing.T) {
	if got := extractArgv("   \n\n # just a comment\n"); len(got) != 0 {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestShellSplit_HashInsideWordIsNotAComment(t *testing.T) {
	got := shellSplit("-n a#b -s1")
	if strings.Join(got, "|") != "-n|a#b|-s1" {
		t.Fatalf("got %q", got)
	}
}
