package tables

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
)

const (
	captureLoopbackDevice      = "lo"
	iptLocalDeliveryPosition   = 2
	iptRouterOwnProxyPosition  = 3
	routerOwnProxyMarkTestBits = ^routeSetMarkMask
)

func iptLocalDeliveryReturn() []string {
	return []string{"-o", captureLoopbackDevice, "-m", "mark", "!", "--mark", fmt.Sprintf("0x0/0x%x", routeSetMarkMask), "-j", "RETURN"}
}

func iptRouterOwnProxyReturn() []string {
	return []string{
		"-m", "mark", "--mark", fmt.Sprintf("0x%x/0x%x", config.RouterOwnProxyMarkBit, routerOwnProxyMarkTestBits),
		"-m", "mark", "!", "--mark", fmt.Sprintf("0x0/0x%x", routeSetMarkMask),
		"-j", "RETURN",
	}
}

func nftSetMarkReturn(mark uint32) []string {
	return []string{"meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "==", fmt.Sprintf("0x%x", mark), "return"}
}

func nftLocalDeliveryReturn() []string {
	return []string{"oifname", `"` + captureLoopbackDevice + `"`, "meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "!=", "0x0", "return"}
}

func nftRouterOwnProxyReturn() []string {
	return []string{
		"meta", "mark", "&", fmt.Sprintf("0x%x", routerOwnProxyMarkTestBits), "==", fmt.Sprintf("0x%x", config.RouterOwnProxyMarkBit),
		"meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "!=", "0x0",
		"return",
	}
}
