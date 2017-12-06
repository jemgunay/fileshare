package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var config Config

func main() {
	// load system config
	rootPath := os.Getenv("GOPATH") + "/src/github.com/jemgunay/fileshare"
	config.LoadConfig(rootPath)

	// init servers
	err, httpServer := NewServerBase()
	if err != nil {
		return
	}

	// process command line input
	for {
		input := getConsoleInput("Enter command")
		switch input {
		case "client":

		case "exit":
			httpServer.Stop()
			return
		default:
			fmt.Printf("Unsupported command.\n")
		}
	}
}

// Format & print input requirement and get console input.
func getConsoleInput(inputMsg string) string {
	reader := bufio.NewReader(os.Stdin)
	println("> " + inputMsg + ":\n")
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	return text
}
