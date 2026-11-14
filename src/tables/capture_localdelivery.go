package tables

import "fmt"

const captureLoopbackDevice = "lo"

func iptLocalDeliveryReturn() []string {
	return []string{"-o", captureLoopbackDevice, "-m", "mark", "!", "--mark", fmt.Sprintf("0x0/0x%x", routeSetMarkMask), "-j", "RETURN"}
}

func nftLocalDeliveryReturn() []string {
	return []string{"oifname", `"` + captureLoopbackDevice + `"`, "meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "!=", "0x0", "return"}
}
