package tproxy

import (
	"testing"

	"github.com/daniellavrushin/b4/leaktest"
)

func TestMain(m *testing.M) {
	leaktest.VerifyTestMain(m)
}
