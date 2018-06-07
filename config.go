package memoryshare

import (
	"flag"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/jemgunay/logger"
)

var (
	// Info is a logger for general info.
	Info = logger.NewLogger(os.Stdout, "INFO", true)
	// Critical is a logger for critical errors.
	Critical = logger.NewLogger(os.Stdout, "CRITICAL", true)
	// Input is a logger for non-critical errors caused by expected/acceptable invalid user input.
	Input = logger.NewLogger(os.Stdout, "INPUT", false)
	// Creation is a logger for User and File creation.
	Creation = logger.NewLogger(os.Stdout, "CREATED", false)
	// Output is a noisy logger for HTTP response.
	Output = logger.NewLogger(os.Stdout, "OUTPUT", false)
	// Incoming is a logger for all incoming requests.
	Incoming = logger.NewLogger(os.Stdout, "INCOMING", false)
	// Outgoing is a logger for all outgoing requests.
	Outgoing = logger.NewLogger(os.Stdout, "OUTGOING", false)
)

// Config is a container for all service settings which are acquired from a TOML config file.
type Config struct {
	rootPath string
	file     string

	GeneralSettings `toml:"general_settings"`
	ServerSettings  `toml:"server_settings"`
	FileFormats     `toml:"file_formats"`
}

// GeneralSettings is a container for general service settings.
type GeneralSettings struct {
	Version               string `toml:"version"`
	ServiceName           string `toml:"service_name"`
	EnableConsoleCommands bool   `toml:"enable_console_commands"`
}

// ServerSettings is a container for HTTP server, mail and access settings.
type ServerSettings struct {
	HTTPPort int `toml:"http_port"`

	EmailServer      string `toml:"email_server"`
	EmailPort        int    `toml:"email_port"`
	EmailAddr        string `toml:"email_addr"`
	EmailPass        string `toml:"email_pass"`
	EmailDisplayAddr string `toml:"email_display_addr"`

	AllowPublicWebApp   bool `toml:"allow_public_web_app"`
	ServePublicUpdates  bool `toml:"serve_public_updates"`
	EnablePublicReads   bool `toml:"enable_public_reads"`
	EnablePublicUploads bool `toml:"enable_public_uploads"`
	MaxFileUploadSize   int  `toml:"max_file_upload_size"`
	MaxSessionAge       int  `toml:"max_session_age"`
}

// FileFormats is a container for permitted file upload types.
type FileFormats struct {
	ImageFormats []string `toml:"image_formats"`
	VideoFormats []string `toml:"video_formats"`
	AudioFormats []string `toml:"audio_formats"`
	TextFormats  []string `toml:"text_formats"`
	OtherFormats []string `toml:"other_formats"`
	fileFormats  map[string]string
}

// NewConfig initialises a new configuration for a memory service.
func NewConfig(rootPath string) (conf *Config, err error) {
	conf = &Config{
		rootPath: rootPath,
		file:     rootPath + "/config/settings.ini",
	}

	// pull config from file
	if err = conf.Load(); err != nil {
		return
	}
	conf.CollateFileFormats()

	// parse flags
	debug := flag.Int("debug", 0, "1=INCOMING/OUTGOING/INPUT/CREATION, 2=OUTPUT")
	flag.IntVar(&conf.HTTPPort, "port", conf.HTTPPort, "overrides the port setting in the config file")
	flag.Parse()

	switch *debug {
	case 2:
		Output.Enable()
		fallthrough
	case 1:
		logger.SetEnabledByCategory(true, "INCOMING", "OUTGOING", "INPUT", "CREATED")
	}

	Info.Logf("running version [%v]", conf.Version)

	return
}

// Load service config from file.
func (c *Config) Load() (err error) {
	// parse TOML config file
	if _, err = toml.DecodeFile(c.file, &c); err != nil {
		return
	}

	// process config values
	c.MaxFileUploadSize *= 1024 * 1024

	Input.Log("\n", ToJSON(*c, true))
	return
}

// CollateFileFormats collates all file types from config and constructs a map of format:type pairs.
func (c *Config) CollateFileFormats() {
	c.fileFormats = make(map[string]string)

	formatMapping := []string{Image, Video, Audio, Text, Other}
	formatTypes := [][]string{c.ImageFormats, c.VideoFormats, c.AudioFormats, c.TextFormats, c.OtherFormats}
	for i, formatType := range formatTypes {
		for _, format := range formatType {
			c.fileFormats[format] = formatMapping[i]
		}
	}

}

// CheckMediaType determines the media type grouping for the provided file extension.
func (c *Config) CheckMediaType(fileExtension string) string {
	if mediaType, ok := c.fileFormats[fileExtension]; ok {
		return mediaType
	}

	return Unsupported
}
