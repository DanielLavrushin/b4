package main

import (
	"fmt"

	"github.com/daniellavrushin/b4/hubwire"
	_ "modernc.org/sqlite"
)

var Version = "dev"

func main() {
	fmt.Println(hubwire.WireVersion, Version)
}
