package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

const fakeSNITestAck = 0x01020304

var fakeSNITestTimestampOptions = []byte{0x01, 0x01, 0x08, 0x0a, 0x00, 0x4c, 0x4b, 0x40, 0x00, 0x00, 0x00, 0x07}

func fakeSNITestSet(name, strategy string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = ""
	set.Name = name
	set.Faking.SNI = true
	set.Faking.SNISeqLength = 1
	set.Faking.Strategy = strategy
	set.Faking.ApplyTTL = false
	set.Faking.MD5OnFake = false
	set.Faking.SNIType = config.FakePayloadDefault2
	return set
}

func fakeSNIOriginal(ver byte, src string, opts []byte) []byte {
	pkt := synWithOptions(ver, opts, buildClientHello("i.ytimg.com", 16, 0xAB), 1000)
	l3 := 20
	if ver == IPv6 {
		l3 = IPv6HeaderLen
	}
	pkt[l3+13] = 0x18
	binary.BigEndian.PutUint32(pkt[l3+8:l3+12], fakeSNITestAck)
	if ver == IPv4 {
		if src != "" {
			copy(pkt[12:16], net.ParseIP(src).To4())
		}
		sock.FixIPv4Checksum(pkt[:20])
		sock.FixTCPChecksum(pkt)
		return pkt
	}
	if src != "" {
		copy(pkt[8:24], net.ParseIP(src).To16())
	}
	sock.FixTCPChecksumV6(pkt)
	return pkt
}

func sendFakeSNIFor(w *Worker, set *config.SetConfig, original []byte) {
	if original[0]>>4 == IPv4 {
		w.sendFakeSNISequence(set, original, net.IP(original[16:20]))
		return
	}
	w.sendFakeSNISequencev6(set, original, net.IP(original[24:40]))
}

func tcpOptionsWithLengths(pkt []byte) [][2]byte {
	l3 := int(pkt[0]&0x0f) * 4
	if pkt[0]>>4 == IPv6 {
		l3 = IPv6HeaderLen
	}
	opts := pkt[l3+20 : l3+int(pkt[l3+12]>>4)*4]
	var out [][2]byte
	for i := 0; i < len(opts) && opts[i] != 0; {
		if opts[i] == 1 {
			i++
			continue
		}
		if i+1 >= len(opts) || opts[i+1] < 2 {
			out = append(out, [2]byte{opts[i], 0})
			break
		}
		out = append(out, [2]byte{opts[i], opts[i+1]})
		i += int(opts[i+1])
	}
	return out
}

func md5OptionLengths(pkt []byte) []byte {
	var lengths []byte
	for _, o := range tcpOptionsWithLengths(pkt) {
		if o[0] == 19 {
			lengths = append(lengths, o[1])
		}
	}
	return lengths
}

func TestAnMD5SumFakeCarriesExactlyOneTCPMD5Option(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, md5OnFake := range []bool{false, true} {
			for _, opts := range [][]byte{nil, fakeSNITestTimestampOptions} {
				t.Run(fmt.Sprintf("v%d/md5_on_fake=%v/options=%d", ver, md5OnFake, len(opts)), func(t *testing.T) {
					set := fakeSNITestSet("md5-set", "md5sum")
					set.Faking.MD5OnFake = md5OnFake
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)

					sendFakeSNIFor(w, &set, fakeSNIOriginal(ver, "", opts))

					got := sent()
					if len(got) != 1 {
						t.Fatalf("want one fake, sent %d packets", len(got))
					}
					if lengths := md5OptionLengths(got[0]); len(lengths) != 1 || lengths[0] != 18 {
						t.Fatalf("the fake must carry exactly one kind 19 option of length 18, got lengths %v in options %v", lengths, tcpOptionsWithLengths(got[0]))
					}
				})
			}
		}
	}
}

func TestAnMD5SumFakeWithNoRoomForTheOptionIsNotSent(t *testing.T) {
	full := make([]byte, 40)
	for i := range full {
		full[i] = 0x01
	}
	for _, ver := range []byte{IPv4, IPv6} {
		for _, md5OnFake := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%d/md5_on_fake=%v", ver, md5OnFake), func(t *testing.T) {
				set := fakeSNITestSet("md5-full", "md5sum")
				set.Faking.MD5OnFake = md5OnFake
				set.Faking.SNISeqLength = 2
				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)

				sendFakeSNIFor(w, &set, fakeSNIOriginal(ver, "", full))

				if got := sent(); len(got) != 0 {
					t.Fatalf("an MD5 Sum fake without its option must not go out, sent %d packets", len(got))
				}
			})
		}
	}
}

func TestMD5OnFakeWithNoRoomStillSendsTheFakeOfAnotherStrategy(t *testing.T) {
	full := make([]byte, 40)
	for i := range full {
		full[i] = 0x01
	}
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			set := fakeSNITestSet("pastseq-md5", "pastseq")
			set.Faking.MD5OnFake = true
			w := newDualStackInjectingWorker(t, &set)
			sent := captureInjected(t, w)

			sendFakeSNIFor(w, &set, fakeSNIOriginal(ver, "", full))

			got := sent()
			if len(got) != 1 {
				t.Fatalf("the pastseq fake must still go out without the option, sent %d packets", len(got))
			}
			if lengths := md5OptionLengths(got[0]); len(lengths) != 0 {
				t.Fatalf("there was no room for the option, got lengths %v", lengths)
			}
		})
	}
}
