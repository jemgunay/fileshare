package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// System settings (acquired from config file).
type Config struct {
	rootPath    string
	file        string
	params      map[string]string
	fileFormats map[MediaType]string
}

// Load server config from local file.
func (c *Config) LoadConfig(rootPath string) (err error) {
	c.rootPath = rootPath
	c.file = c.rootPath + "/config/server.conf"
	c.params = make(map[string]string)

	// open config file
	file, err := os.Open(c.file)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer file.Close()

	// read file by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// skip empty lines or # comments
		if strings.TrimSpace(line) == "" || []rune(line)[0] == '#' {
			continue
		}
		// check if param is valid
		paramSplit := strings.Split(line, "=")
		if len(paramSplit) < 2 {
			continue
		}
		c.params[paramSplit[0]] = paramSplit[1]
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
		return err
	}

	// set up media type pairings
	c.fileFormats = make(map[MediaType]string)
	c.fileFormats[IMAGE] = c.params["image_formats"]
	c.fileFormats[VIDEO] = c.params["video_formats"]
	c.fileFormats[AUDIO] = c.params["audio_formats"]
	c.fileFormats[TEXT] = c.params["text_formats"]
	c.fileFormats[OTHER] = c.params["other_formats"]

	log.Printf("running version [%v]\n", c.params["version"])

	return nil
}

// Get the media type grouping for the provided file extension.
func (c *Config) CheckMediaType(fileExtension string) (MediaType, error) {
	// check for malicious commas
	if strings.Contains(fileExtension, ",") {
		return UNSUPPORTED, fmt.Errorf("'%s' is an unsupported file format", fileExtension)
	}

	for mediaType, formats := range c.fileFormats {
		if strings.Contains(formats, fileExtension) {
			return mediaType, nil
		}
	}
	return UNSUPPORTED, fmt.Errorf("'%s' is an unsupported file format", fileExtension)
}

// Save server config to local file.
func (c *Config) SaveConfig() {

}
