// Package cmd launches a memoryshare service instance.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jemgunay/memoryshare"
)

func main() {
	var config memoryshare.Config

	debug := flag.Int("debug", 0, "1=INCOMING/INPUT/CREATION, 2=OUTPUT")
	flag.Parse()

	switch *debug {
	case 1:
		memoryshare.Incoming.Enable()
		memoryshare.Input.Enable()
		memoryshare.Creation.Enable()
	case 2:
		memoryshare.Output.Enable()
	}

	// load system config
	executable, err := os.Executable()
	if err != nil {
		fmt.Printf("Unable to determine working directory.\n")
		return
	}
	rootPath := filepath.Dir(executable + "/../../../")
	config.LoadConfig(rootPath)

	if err = config.SaveConfig(); err != nil {
		memoryshare.Critical.Log(err)
	}

	// init servers
	err, server := memoryshare.NewServer(&config)
	if err != nil {
		return
	}

	// process command line input
	if config.GetBool("enable_console_commands") {
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
		var exit chan bool
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
