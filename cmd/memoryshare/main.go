// Launches a memoryshare service instance.
package main

import (
	"os"
	"path/filepath"
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
			input, err := memoryshare.ReadStdin("Enter command", false)
			if err != nil {
				memoryshare.Critical.Log(err)
			}

			switch input {
			// terminate service
			case "exit":
				server.Stop()
				return
			default:
				memoryshare.Info.Log("Unsupported command.")
			}
		}
	} else {
		<-exit
	}
}
