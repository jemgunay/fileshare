package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

var config Config

func main() {
	// load system config
	rootPath := os.Getenv("GOPATH") + "/src/github.com/jemgunay/fileshare"
	config.LoadConfig(rootPath)

	err := config.SaveConfig()
	if err != nil {
		fmt.Println(err)
	}

	// init servers
	err, httpServer := NewServerBase()
	if err != nil {
		return
	}

	// process command line input
	time.Sleep(time.Millisecond * 300)
	for {
		input := getConsoleInput("Enter command")
		switch input {
		// reset DB
		case "destroy":
			fileAR := FileAccessRequest{errorOut: make(chan error), operation: "destroy"}
			httpServer.fileDB.requestPool <- fileAR
			if err := <-fileAR.errorOut; err != nil {
				log.Println(err)
			}
		// terminate
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
	fmt.Println("> " + inputMsg + ":")
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	return text
}
