package discovery

import (
	"testing"

	"github.com/daniellavrushin/b4/leaktest"
)

func TestMain(m *testing.M) {
	probeRefusesAddr = nil
	leaktest.VerifyTestMain(m)
}
