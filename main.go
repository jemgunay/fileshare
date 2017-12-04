package main

import (
	"os"
)

var rootPath = os.Getenv("GOPATH") + "/src/github.com/jemgunay/fileshare"

func main() {
	server, err := NewServerBase()

	for err == nil {
		_ = server
	}
}
