package tables

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func stubSetListing(t *testing.T, answer func(args []string) (string, error)) *[]string {
	t.Helper()
	var calls []string
	prev := run
	t.Cleanup(func() { run = prev })
	run = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return answer(args)
	}
	return &calls
}

func TestIpsetProbeListsOnlyTheSetHeader(t *testing.T) {
	stubBinaries(t, "ipset")
	calls := stubSetListing(t, func(args []string) (string, error) {
		if args[len(args)-1] == "b4r_x_v6" {
			return "Name: b4r_x_v6\nType: hash:net\nNumber of entries: 9000\n", nil
		}
		return "ipset v7.19: The set with the given name does not exist", errors.New("exit status 1")
	})
	be := &routeIptBackend{}
	if !be.hasIPSet("b4r_x_v6") || be.hasIPSet("b4r_y_v6") {
		t.Fatal("the probe must tell a present set from a missing one")
	}
	want := []string{"ipset list -t b4r_x_v6", "ipset list -t b4r_y_v6"}
	if !slices.Equal(*calls, want) {
		t.Errorf("a full listing prints every member of a set that may hold thousands, ran %v, want %v", *calls, want)
	}
}

func TestNftProbeListsOnlyTheSetHeader(t *testing.T) {
	prevTerse := routeNftTerse.Load()
	t.Cleanup(func() { routeNftTerse.Store(prevTerse) })
	missing := "Error: No such file or directory\nlist set inet b4_route b4r_x_v6\n"
	for _, c := range []struct {
		name    string
		terse   bool
		present map[string]bool
		noTerse bool
		want    bool
		calls   []string
	}{
		{"present", true, map[string]bool{"b4r_x_v6": true}, false, true, []string{
			"nft -t list set inet b4_route b4r_x_v6",
		}},
		{"only the timeout set left", true, map[string]bool{"b4r_x_v6_d": true}, false, true, []string{
			"nft -t list set inet b4_route b4r_x_v6",
			"nft -t list set inet b4_route b4r_x_v6_d",
		}},
		{"gone", true, nil, false, false, []string{
			"nft -t list set inet b4_route b4r_x_v6",
			"nft -t list set inet b4_route b4r_x_v6_d",
		}},
		{"an nft without the terse flag", true, map[string]bool{"b4r_x_v6": true}, true, true, []string{
			"nft -t list set inet b4_route b4r_x_v6",
			"nft list set inet b4_route b4r_x_v6",
		}},
		{"terse already known to fail", false, nil, true, false, []string{
			"nft list set inet b4_route b4r_x_v6",
			"nft list set inet b4_route b4r_x_v6_d",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			routeNftTerse.Store(c.terse)
			calls := stubSetListing(t, func(args []string) (string, error) {
				if c.noTerse && args[1] == "-t" {
					return "nft: invalid option -- 't'", errors.New("exit status 64")
				}
				if c.present[args[len(args)-1]] {
					return "table inet b4_route {\n}\n", nil
				}
				return missing, errors.New("exit status 1")
			})
			if got := (&routeNftBackend{}).hasIPSet("b4r_x_v6"); got != c.want {
				t.Errorf("hasIPSet = %v, want %v", got, c.want)
			}
			if !slices.Equal(*calls, c.calls) {
				t.Errorf("ran %v, want %v", *calls, c.calls)
			}
		})
	}
}
