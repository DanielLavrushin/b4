package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/leaktest"
)

func TestMain(m *testing.M) {
	systemNameservers = func() []string { return []string{"127.0.0.1"} }
	leaktest.VerifyTestMain(m)
}
