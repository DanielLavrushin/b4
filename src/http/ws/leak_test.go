package ws

import (
	"testing"

	"github.com/daniellavrushin/b4/leaktest"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	leaktest.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/http/ws.(*LogHub).run"),
	)
}
