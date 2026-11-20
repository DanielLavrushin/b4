package nfq

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

const negativeUDPFakeLenChildEnv = "B4_NEGATIVE_UDP_FAKE_LEN_CHILD"

func udpFakeModeSet(name string, fakeLen int) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = name
	set.Name = name
	set.Enabled = true
	set.UDP.Mode = "fake"
	set.UDP.DPortFilter = "50000-50100"
	set.UDP.FakeLen = fakeLen
	set.UDP.FakeSeqLength = 1
	set.UDP.FakePayloadFile = ""
	set.UDP.Seg2Delay = 0
	set.UDP.Seg2DelayMax = 0
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	return set
}

func udpFakeTestPacket(ver byte) ([]byte, net.IP) {
	payload := []byte("not a quic initial, any udp payload")
	if ver == IPv6 {
		raw := makeV6UDPPacketWithExt(payload, 51000, 50005, 0, nil)
		return raw, net.IP(raw[24:40])
	}
	return makeV4UDPPacket(payload, quicClient, quicServer, 51000, 50005), quicServer
}

func injectUDPFake(w *Worker, set *config.SetConfig, ver byte, raw []byte, dst net.IP) {
	if ver == IPv6 {
		w.dropAndInjectQUICV6(set, raw, dst)
		return
	}
	w.dropAndInjectQUIC(set, raw, dst)
}

func TestQUICFakeInjectionSurvivesANegativeFakeLen(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, fakeLen := range []int{-1, -100} {
			t.Run(fmt.Sprintf("v%d/fake_len=%d", ver, fakeLen), func(t *testing.T) {
				set := udpFakeModeSet("negative-udp-fake-len", fakeLen)
				w := newDualStackInjectingWorker(t, &set)
				captureInjected(t, w)
				raw, dst := udpFakeTestPacket(ver)

				var panicked string
				func() {
					defer func() {
						if r := recover(); r != nil {
							panicked = fmt.Sprint(r)
						}
					}()
					injectUDPFake(w, &set, ver, raw, dst)
				}()
				if panicked != "" {
					t.Fatalf("the QUIC fake injection with udp.fake_len=%d panicked: %s; it runs in a goroutine with no recover(), so this ends b4", fakeLen, panicked)
				}
			})
		}
	}
}

func TestANegativeUDPFakeLenDoesNotEndTheProcess(t *testing.T) {
	if os.Getenv(negativeUDPFakeLenChildEnv) == "1" {
		set := udpFakeModeSet("negative-udp-fake-len", -1)
		w := newQUICFlowWorker(t, &set)
		sendUDPTo(w, []byte("any udp payload to the set's port"), 51000, 50005)
		w.wg.Wait()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestANegativeUDPFakeLenDoesNotEndTheProcess$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), negativeUDPFakeLenChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if !strings.Contains(text, "=== RUN   TestANegativeUDPFakeLenDoesNotEndTheProcess") {
		t.Fatalf("the child process did not run the test:\n%s", text)
	}
	if i := strings.Index(text, "panic:"); i >= 0 {
		first := text[i:]
		if j := strings.IndexByte(first, '\n'); j > 0 {
			first = first[:j]
		}
		t.Fatalf("one UDP packet through ProcessPacket to a fake-mode set with udp.fake_len=-1 ended the process (%v): %s", err, first)
	}
	if err != nil {
		t.Fatalf("the child process failed without a panic: %v\n%s", err, text)
	}
}

func refusingSender(t *testing.T) *sock.Sender {
	t.Helper()
	open := func() int {
		fd, err := syscall.Open(os.DevNull, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("open %s: %v", os.DevNull, err)
		}
		t.Cleanup(func() { _ = syscall.Close(fd) })
		return fd
	}
	return sock.NewSenderWithFDs(open(), open())
}

func udpFakeWarnings(buf *syncBuffer, setName string) []string {
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "[WARN]") && strings.Contains(line, "UDP") && strings.Contains(line, fmt.Sprintf("%q", setName)) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestAUDPFakeTheKernelRefusesIsLoggedOnceWithItsSet(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			buf := captureLog(t)
			set := udpFakeModeSet("refused-udp-fake", 64)
			set.UDP.FakeSeqLength = 3
			w := newDualStackInjectingWorker(t, &set)
			w.sock = refusingSender(t)
			raw, dst := udpFakeTestPacket(ver)

			injectUDPFake(w, &set, ver, raw, dst)

			log.Flush()
			if lines := udpFakeWarnings(buf, set.Name); len(lines) != 1 {
				t.Fatalf("three UDP fakes the socket refused: want one warning naming the set, got %d:\n%s", len(lines), buf.String())
			}
		})
	}
}
