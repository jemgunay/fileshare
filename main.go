package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"flag"
)

var config Config

func main() {
	// log level
	logVerbosityFlag := flag.Int("log_verbosity", 0, "0 (none), 1 (critical errors), 2 (all errors), 3 (all responses)")
	flag.Parse()
	config.SetLogVerbosity(*logVerbosityFlag)

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
			response := httpServer.fileDB.performAccessRequest(FileAccessRequest{operation: "destroy"})
			if response.err != nil {
				log.Println(response.err)
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
