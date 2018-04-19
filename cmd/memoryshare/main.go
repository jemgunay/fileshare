// Launches a memoryshare service instance.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jemgunay/memoryshare"
)

func main() {
	// get absolute path to project base directory
	executable, err := os.Executable()
	if err != nil {
		memoryshare.Critical.Logf("Unable to determine working directory: %v", err)
		return
	}
	rootPath := filepath.Dir(executable + "/../../../")

	// create service config
	config, err := memoryshare.NewConfig(rootPath)
	if err != nil {
		memoryshare.Critical.Logf("Unable to parse config: %v", err)
		return
	}

	// launch servers
	server, err := memoryshare.NewServer(config)
	if err != nil {
		memoryshare.Critical.Logf("Unable to launch server: %v", err)
		return
	}

	// process command line input
	var exit chan bool
	if config.EnableConsoleCommands {
		time.Sleep(time.Millisecond * 300)
		for {
			input := getConsoleInput("Enter command")
			switch input {
			// terminate
			case "exit":
				server.Stop()
				return
			default:
				memoryshare.Info.Log("Unsupported command.\n")
			}
		}
	} else {
		<-exit
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
