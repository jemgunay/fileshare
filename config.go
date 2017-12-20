package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// Index represents the line in the config file, val is the param value.
type ConfigSet struct {
	index int
	val   string
}

// System settings (acquired from config file).
type Config struct {
	rootPath     string
	file         string
	params       map[string]ConfigSet
	fileFormats  map[MediaType]string
	indexCounter int
	commentLines []ConfigSet
}

// Load server config from local file.
func (c *Config) LoadConfig(rootPath string) (err error) {
	c.rootPath = rootPath
	c.file = c.rootPath + "/config/server.conf"
	c.params = make(map[string]ConfigSet)

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
		if strings.TrimSpace(line) == "" {
			c.commentLines = append(c.commentLines, ConfigSet{c.indexCounter, "\n"})
			c.indexCounter++
			continue
		}
		if []rune(line)[0] == '#' {
			c.commentLines = append(c.commentLines, ConfigSet{c.indexCounter, line})
			c.indexCounter++
			continue
		}
		// check if param is valid
		paramSplit := strings.Split(line, "=")
		if len(paramSplit) < 2 {
			continue
		}
		c.params[paramSplit[0]] = ConfigSet{c.indexCounter, line[len(paramSplit[0])+1:]}
		c.indexCounter++
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
		return err
	}

	// set up media type pairings
	c.fileFormats = make(map[MediaType]string)
	c.fileFormats[IMAGE] = c.params["image_formats"].val
	c.fileFormats[VIDEO] = c.params["video_formats"].val
	c.fileFormats[AUDIO] = c.params["audio_formats"].val
	c.fileFormats[TEXT] = c.params["text_formats"].val
	c.fileFormats[OTHER] = c.params["other_formats"].val

	log.Printf("running version [%v]\n", c.params["version"].val)

	return nil
}

// Set a param/value pair in config.
func (c *Config) Set(param string, value string) {
	c.params[param] = ConfigSet{c.indexCounter, value}
	c.indexCounter++
	config.SaveConfig()
}

// Get the media type grouping for the provided file extension.
func (c *Config) CheckMediaType(fileExtension string) (MediaType, error) {
	// check for malicious commas before parsing
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
func (c *Config) SaveConfig() error {
	type ConfigPairSet struct {
		key string
		val string
	}
	// order while mapping map to slice
	confSlice := make([]ConfigPairSet, c.indexCounter)
	for key, value := range c.params {
		confSlice[value.index] = ConfigPairSet{key, value.val}
	}
	for _, value := range c.commentLines {
		confSlice[value.index] = ConfigPairSet{"", value.val}
	}

	// slice to string
	var configStr string
	for _, value := range confSlice {
		if value.key == "" {
			configStr += value.val
			if value.val != "\n" {
				configStr += "\n"
			}
			continue
		}
		configStr += value.key + "=" + value.val + "\n"
	}

	// write to file
	file, err := os.OpenFile(c.file, os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(strings.TrimSpace(configStr))
	return err
}
