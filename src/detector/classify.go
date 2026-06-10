package detector

import "github.com/daniellavrushin/b4/netprobe"

type DomainStatus = netprobe.DomainStatus

const (
	DomainOk       = netprobe.DomainOk
	DomainTLSDPI   = netprobe.DomainTLSDPI
	DomainTLSMITM  = netprobe.DomainTLSMITM
	DomainTLSSpoof = netprobe.DomainTLSSpoof
	DomainTLSAlert = netprobe.DomainTLSAlert
	DomainTLSReset = netprobe.DomainTLSReset
	DomainTLSDrop  = netprobe.DomainTLSDrop
	DomainSYNDrop  = netprobe.DomainSYNDrop
	DomainTCP16    = netprobe.DomainTCP16
	DomainISPPage  = netprobe.DomainISPPage
	DomainBlocked  = netprobe.DomainBlocked
	DomainDNSFake  = netprobe.DomainDNSFake
	DomainTimeout  = netprobe.DomainTimeout
	DomainError    = netprobe.DomainError
)

type tlsStage = netprobe.TLSStage

const (
	stageConnect   = netprobe.StageConnect
	stageHandshake = netprobe.StageHandshake
	stageRead      = netprobe.StageRead
)

const tcp16MaxBytes = netprobe.TCP16MaxBytes

func ClassifyTLSError(err error) (DomainStatus, string) {
	return netprobe.ClassifyTLSError(err)
}

func ClassifyTLSErrorStaged(err error, stage tlsStage, bytesRead int) (DomainStatus, string) {
	return netprobe.ClassifyTLSErrorStaged(err, stage, bytesRead)
}

func ClassifyHTTPResponse(statusCode int, location, body string) (DomainStatus, string) {
	return netprobe.ClassifyHTTPResponse(statusCode, location, body)
}
