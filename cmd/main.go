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

	// log level
	logVerbosityFlag := flag.Int("verbosity", 0, "0 (none), 1 (critical errors), 2 (all errors), 3 (all responses)")
	flag.Parse()
	config.SetLogVerbosity(*logVerbosityFlag)

	// load system config
	executable, err := os.Executable()
	if err != nil {
		fmt.Printf("Unable to determine working directory.\n")
		return
	}
	rootPath := filepath.Dir(executable + "/../../")
	config.LoadConfig(rootPath)

	if err = config.SaveConfig(); err != nil {
		fmt.Println(err)
	}

	// init servers
	err, httpServer := memoryshare.NewServerBase(&config)
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
				httpServer.Stop()
				return
			default:
				fmt.Printf("Unsupported command.\n")
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