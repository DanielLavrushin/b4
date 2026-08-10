package nfq

const (
	IPv4             = 4
	IPv6             = 6
	IPv4HeaderMinLen = 20
	IPv6HeaderLen    = 40
	TCPHeaderMinLen  = 20
	UDPHeaderLen     = 8
	TLSHandshakeType = 0x16
	TLSClientHello   = 0x01
	HTTPSPort        = 443

	nfqReadBufferBytes = 4 << 20

	connKeyFormat = "%s:%d->%s:%d"
)
