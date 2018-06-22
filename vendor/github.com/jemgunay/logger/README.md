## A Category & Component-Orientated Logger

This package facilities the creation of individual Loggers which each represent a specific category of information. This is achieved in a modular fashion, where the combination of a Category, Timestamp and Message result in customisable logging styles. The Loggers can be enabled or disabled which provides more control over which logs you want to see. 

#### Default logger creation & various Log methods
```go
// output to stdout
Info := logger.NewLogger(os.Stdout, "INFO", true)
Warning := logger.NewLogger(os.Stdout, "WARNING", true)
// output to stderr
Error := logger.NewLogger(os.Stderr, "ERROR", true)

// calling a Loggers' Logx methods 
Info.Log("this is a logged general info message...")
Warning.Logln("this is a logged warning message...")
Error.Logf("this is a logged critical error message: [%v]...", errors.New("BOOM!"))

// calling the global Logx methods (which accept a Logger argument)
logger.Log(Info, "this is a logged general info message...")
logger.Logln(Warning, "this is a logged warning message...")
logger.Logf(Error, "this is a logged critical error message: [%v]...", errors.New("BOOM!"))
```
Result:
```
[INFO]      18/04/27 14:51:45.59825 this is a logged general info message...
[WARNING]   18/04/27 14:51:45.59828 this is a logged warning message...

[ERROR]     18/04/27 14:51:45.59830 this is a logged critical error message: [BOOM!]...
[INFO]      18/04/27 14:51:45.59825 this is a logged general info message...
[WARNING]   18/04/27 14:51:45.59828 this is a logged warning message...

[ERROR]     18/04/27 14:51:45.59830 this is a logged critical error message: [BOOM!]...
```

#### Configure a new customised logger
```go
newLogger := logger.Logger{
    Writer:  os.Stdout,
    Enabled: true,
    Category: logger.Category{
        Name:      "ERROR",
        Formatter: logger.SquareBracketWrapper,
    },
    Timestamp: logger.Timestamp{
        Format:    "15:04:05.00000",
        Formatter: logger.BracketWrapper,
    },
    Message: logger.Message{
        Formatter: nil,
    },
}

logger.AddLogger(&newLogger)
newLogger.Log("another error log...")
``` 
Result:
```
[ERROR] (14:59:08.54493) another error log...
```

#### Modifying existing loggers
```go
Error := logger.NewLogger(os.Stderr, "ERROR", true)
Error.Log("original format")

Error.Timestamp.Format = "15:04:05.00000"
Error.Category = logger.Category{Name: "CRITICAL", Formatter: logger.BracketWrapper}
Error.Log("new Timestamp Format & Category")
```
Result:
```
[ERROR] 18/04/27 15:16:16.76343 original format
(CRITICAL) 15:16:16.76346 new Timestamp Format & Category
```

#### Enable & disable loggers
```go
Info := logger.NewLogger(os.Stdout, "INFO", true)
Info.Disable()
Error := logger.NewLogger(os.Stderr, "ERROR", false)
Error.Enable()

Info.Log("info - not logged")
Error.Log("error - logged")
```
Result:
```
[ERROR] 18/04/27 15:23:25.57138 error - logged
```

#### Logging to files
```go
fileWriter, _ := os.OpenFile("./test.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
File := logger.NewLogger(fileWriter, "FILE", true)
File.Log("this message has been written to a file")
```
(remember to close file before program terminates)

#### Category padding & grouping logged messages by Category
```go
// both padding & grouping are enabled by default
logger.SetCategoryGrouping(true)
logger.SetCategoryPadding(true)
```
Result:
```
[INFO]     18/04/27 15:25:47.31102 this logger has been enabled
[ERROR]    18/04/27 15:25:47.31103 example error message no. 1
           18/04/27 15:25:47.31104 example error message no. 2
           18/04/27 15:25:47.31104 example error message no. 3
           18/04/27 15:25:47.31106 example error message no. 4
[INCOMING] 18/04/27 15:25:47.31106 < this is an incoming request: localhost:8080/retrieve
           18/04/27 15:25:47.31107 < this is an incoming request: localhost:8080/upload
[OUTGOING] 18/04/27 15:25:47.31108 > this is an outgoing request: http://google.com
```

#### Formatting component output
Category, Timestamp and Message all use their Formatter to format each logged message component before it is written to the output.  

Many functions from the strings package (such as ```strings.ToUpper```) can be used as a Formatter, as well as your own ```FormatterFunc``` functions. The FormatterFuncs ```BracketWrapper``` and ```SquareBracketWrapper``` have been defined which wrap messages in brackets. Leave a Formatter set to ```nil``` to disable it.
```go
Formatted = logger.NewLogger(os.Stdout, "FORMATTED", true)
// wrap category in square brackets
Formatted.Category.Formatter = logger.SquareBracketWrapper

// capitalise whole message
Formatted.Message.Formatter = strings.ToUpper
Formatted.Log("this message used to be lower case")

// alternate letter case
alternateCase := func(s string) (result string) {
    i := 0
    for _, char := range s {
        i++
        if i%2 == 0 {
            result += strings.ToUpper(string(char))
            continue
        }
        result += string(char)
    }
    return
}
Formatted.Message.Formatter = alternateCase
Formatted.Log("this message also used to be lower case")
```
Result:
```
[FORMATTED] 18/04/27 15:25:47.31112 THIS MESSAGE USED TO BE LOWER CASE
            18/04/27 15:25:47.31112 tHiS MeSsAgE AlSo uSeD To bE LoWeR CaSe
```

#### Buffered & unbuffered queueing
When logger is set to use a queue buffer, the caller of Logx functions does not block.
```go
logger.SetBuffered(true)
logger.SetBuffered(false)
```