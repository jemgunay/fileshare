package main

import (
	"os"
)

func main() {
	rootPath := os.Getenv("GOPATH") + "/src/github.com/jemgunay/fileshare"
	server, err := NewServer(rootPath)

	for err == nil {
		_ = server
	}
}
