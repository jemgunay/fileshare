// Package logger is a category and component-orientated logger.
package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var (
	loggers          = make(map[*Logger]bool)
	categoryPadding  = true
	categoryGrouping = true

	// BufferSize determines the size of the buffered channel used to queue messages when a logger is set to use its buffer.
	BufferSize      = 1024
	bufferEnabled   = false
	highestLoggerID = -1
	logQueue        = make(chan queueItem)
	logQueueBuffer  = make(chan queueItem, BufferSize)
	exitCh          = make(chan struct{})

	// Internal is an internal logger for logging debug and error related info.
	Internal = NewLogger(os.Stdout, "LOG", true)
)

func init() {
	startPoller()
}

// queueItem is used to
type queueItem struct {
	writer   io.Writer
	category Category
	message  string
}

// startPoller attempts to receive from both the standard queue, the buffered queue and exit channel. This serialises
// all logging writes.
func startPoller() {
	go func() {
		for {
			select {
			// receive and write a message from the queue
			case queueItem := <-logQueue:
				performWrite(queueItem)

				// receive and write a message from the queue
			case queueItem := <-logQueueBuffer:
				performWrite(queueItem)

				// stop polling for logs to write
			case <-exitCh:
				return
			}
		}
	}()
}

var (
	maxCategorySize  int
	previousCategory string
)

// performWrite formats messages to align timestamps and group messages based on category depending on whether these
// features have been enabled.
func performWrite(queueItem queueItem) {
	padding := ""
	currentCategory := queueItem.category.Compose()

	// pad log categories so that all timestamps are aligned
	if categoryPadding {
		padding = strings.Repeat(" ", maxCategorySize-len(currentCategory)+1)
	}
	if queueItem.category.Name != "" && categoryPadding == false {
		padding += " "
	}

	// group logs by category
	if categoryGrouping && previousCategory == queueItem.category.Name {
		currentCategory = strings.Repeat(" ", len(currentCategory))
	}
	queueItem.message = currentCategory + padding + queueItem.message

	// write message
	fmt.Fprintln(queueItem.writer, queueItem.message)

	previousCategory = queueItem.category.Name
}

// FormatterFunc is used to pass a string manipulating function to a Logger's Category, Timestamp or Message in order to
// format their corresponding text before it is written to output.
type FormatterFunc func(string) string

var (
	// BracketWrapper is an example formatter which wraps the target string in brackets.
	BracketWrapper = func(s string) string {
		return "(" + s + ")"
	}
	// SquareBracketWrapper is an example a formatter which wraps the target string in square brackets.
	SquareBracketWrapper = func(s string) string {
		return "[" + s + "]"
	}
)

// Category is the Logger component which is written to output first. It is used to categorise logged messages based on
// their intended purpose/meaning (if the Name property is set), i.e. INFO, WARNING, ERROR, etc.
type Category struct {
	Formatter FormatterFunc
	Name      string
}

// Compose constructs the Category component text if a Name has been provided. Otherwise, an empty Category text is
// returned.
func (c *Category) Compose() string {
	if c.Name == "" || c.Formatter == nil {
		return c.Name
	}
	return c.Formatter(c.Name)
}

// Timestamp is the Logger component which is written to output after the Category but before the Message. The Format
// determines the layout of the formatted timestamp (default of 06/01/02 15:04:05.00000).
type Timestamp struct {
	Format    string
	Formatter FormatterFunc
}

// Compose constructs the Timestamp component text if a Format has been provided. Otherwise, an empty Timestamp text is
// returned.
func (t *Timestamp) Compose() string {
	if t.Format == "" {
		return t.Format
	}

	ts := time.Now()
	datetime := ts.Format(t.Format)

	if t.Formatter == nil {
		return datetime
	}
	return t.Formatter(datetime)
}

// Message is the is the Logger component which is written to output last, following the Timestamp Component.
type Message struct {
	Formatter FormatterFunc
}

// Compose constructs the Message component text using a provided message.
func (m *Message) Compose(message string) string {
	if m.Formatter == nil {
		return message
	}
	return m.Formatter(message)
}

// Logger is a logger which is designed to output one specific type of logging information. Output messages are composed
// out of the Category, Timestamp and Message components in that order before they are written to the Writer. The Logger
// can be enabled/disabled - when disabled, any calls to a Logx function will be silently ignored. The Logger also
// counts how many messages is has logged.
type Logger struct {
	Category  Category
	Timestamp Timestamp
	Message   Message

	Writer         io.Writer
	Enabled        bool
	id             int
	splunkEnabled  bool
	counterEnabled bool
	counterName    string
	count          int
}

// NewLogger creates a new logger given an io.Writer to log to, a category to display before the timestamp and a flag to
// determine whether the logger is enabled by default. A pointer to this Logger is then returned.
func NewLogger(handle io.Writer, category string, enabled bool) *Logger {
	highestLoggerID++

	// create new logger
	newLogger := Logger{
		Writer:  handle,
		Enabled: enabled,
		id:      highestLoggerID,
		Category: Category{
			Name:      category,
			Formatter: SquareBracketWrapper,
		},
		Timestamp: Timestamp{
			Format:    "01/02 15:04:05",
			Formatter: nil,
		},
		Message: Message{
			Formatter: nil,
		},
	}

	// store reference to logger & reset prefix padding
	loggers[&newLogger] = true
	SetCategoryPadding(categoryPadding)

	return &newLogger
}

// AddLogger adds a pre-constructed Logger(s) to the logger system.
func AddLogger(newLoggers ...*Logger) {
	for _, newLogger := range newLoggers {
		// store reference to logger & reset prefix padding
		highestLoggerID++
		newLogger.id = highestLoggerID
		loggers[newLogger] = true
		SetCategoryPadding(categoryPadding)
	}
}

// SetCategoryPadding is used to enable or disable padding after all Categories to align all Timestamps. This is also
// called internally to reset the padding mechanism when a new logger is created.
func SetCategoryPadding(enabled bool) {
	categoryPadding = enabled

	maxCategorySize = 0
	if enabled {
		// determine the maximum amount of padding required to align timestamps
		var tempMax, categorySize int
		for l := range loggers {
			categorySize = len(l.Category.Compose())

			if categorySize > tempMax {
				tempMax = categorySize
			}
		}
		maxCategorySize = tempMax
	}
}

// SetCategoryGrouping enables or disables category grouping. This means that if a number of messages are output with
// the same Category Name, only the first message contains the Category Name prefix.
func SetCategoryGrouping(enabled bool) {
	categoryGrouping = enabled
}

// performLog formats & writes a log message to one of the logging queues depending on whether buffered logging has been
// enabled. Each of the Logx functions depend on performLog.
func (l *Logger) performLog(message string, newline bool) {
	if l.Enabled == false {
		return
	}

	// compose message
	message = l.Timestamp.Compose() + " " + l.Message.Compose(message)
	if newline {
		message += "\n"
	}

	// send message to be written
	newMsg := queueItem{
		writer:   l.Writer,
		category: l.Category,
		message:  message,
	}

	l.count++
	if bufferEnabled {
		logQueueBuffer <- newMsg
		return
	}
	logQueue <- newMsg
}

// SetBuffered enables or disables logging via a buffered channel. When enabled, the caller of Logx functions does not
// block. When disabled, the caller is blocked until the message is received.
func SetBuffered(useBuffer bool) {
	bufferEnabled = useBuffer
}

// Log logs the provided message if the Logger is enabled.
func (l *Logger) Log(msg ...interface{}) {
	l.performLog(fmt.Sprint(msg...), false)
}

// Logf logs the provided message with formatting if the Logger is enabled.
func (l *Logger) Logf(format string, args ...interface{}) {
	l.performLog(fmt.Sprintf(format, args...), false)
}

// Logln logs the provided message followed by a new line if the Logger is enabled.
func (l *Logger) Logln(msg ...interface{}) {
	l.performLog(fmt.Sprint(msg...), true)
}

// Enable enables the logger.
func (l *Logger) Enable() {
	l.Enabled = true
}

// Disable disables the logger, meaning any logged messages are silently ignored.
func (l *Logger) Disable() {
	l.Enabled = false
}

// Count returns the number of messages logged by the Logger.
func (l *Logger) Count() int {
	return l.count
}

// SetEnabledByCategory enables or disables all loggers with Category Names which match the list of categories provided,
// i.e. SetEnabledByCategory(false, "INCOMING", "OUTGOING") would disable both INCOMING and OUTGOING loggers if they
// exist. The categories are case sensitive.
func SetEnabledByCategory(enabled bool, categories ...string) {
	for l := range loggers {
		for _, c := range categories {
			if l.Category.Name == c {
				l.Enabled = enabled
			}
		}
	}
}

// SetEnabledByID is used to enable all loggers which have an ID of loggerID or below, and to disable all other loggers.
// This can be used to set which loggers are enabled/disabled based on a logging verbosity level. The first logger
// created (the Internal logger) will have an ID of 0, and the ID will increment by 1 for every other logger created.
// A negative loggerID will disable all loggers.
func SetEnabledByID(loggerID int) {
	for l := range loggers {
		l.Enabled = l.id <= loggerID
	}
}

// StopPoller stops all log queue channel polling, effectively disabling the logger package. The HTTP web viewer
// server is also shut down.
func StopPoller() {
	exitCh <- struct{}{}
}

// Log logs the provided message if the Logger is enabled.
func Log(logger *Logger, msg ...interface{}) {
	logger.performLog(fmt.Sprint(msg...), false)
}

// Logf logs the provided message with formatting if the Logger is enabled.
func Logf(logger *Logger, format string, args ...interface{}) {
	logger.performLog(fmt.Sprintf(format, args...), false)
}

// Logln logs the provided message followed by a new line if the Logger is enabled.
func Logln(logger *Logger, msg ...interface{}) {
	logger.performLog(fmt.Sprint(msg...), true)
}

// Count returns the number of loggers that have been created.
func Count() int {
	return len(loggers)
}
