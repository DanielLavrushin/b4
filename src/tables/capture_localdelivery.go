package tables

import "fmt"

const (
	captureLoopbackDevice    = "lo"
	iptLocalDeliveryPosition = 2
)

func iptLocalDeliveryReturn() []string {
	return []string{"-o", captureLoopbackDevice, "-m", "mark", "!", "--mark", fmt.Sprintf("0x0/0x%x", routeSetMarkMask), "-j", "RETURN"}
}

func nftSetMarkReturn(mark uint32) []string {
	return []string{"meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "==", fmt.Sprintf("0x%x", mark), "return"}
}

func nftLocalDeliveryReturn() []string {
	return []string{"oifname", `"` + captureLoopbackDevice + `"`, "meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "!=", "0x0", "return"}
}
