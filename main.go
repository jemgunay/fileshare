package main

import "os"

func main() {
	server := NewServer(os.Getenv("GOPATH") + "/src/github.com/jemgunay/fileshare")
	for server != nil {

	}
}
