package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/hpcloud/tail"
	"github.com/hpcloud/tail/util"
	"github.com/jessevdk/go-flags"
	"github.com/nsf/termbox-go"
)

var colors = []color.Attribute{color.FgGreen, color.FgCyan, color.FgYellow, color.FgBlue, color.FgRed, color.FgMagenta}

type DockerJSONLogRecord struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

type options struct {
	Docker      bool `short:"d" long:"docker" description:"Parse log files as Docker JSON format"`
	Positionals struct {
		Filenames []string
	} `positional-args:"yes" required:"yes"`
}

var opts options

func parseArgs(args []string) error {
	parser := flags.NewParser(&opts, flags.PassDoubleDash|flags.HelpFlag)
	_, err := parser.ParseArgs(args)
	if err != nil {
		return err
	}
	return nil
}

func getTermSize() (int, int, error) {
	if err := termbox.Init(); err != nil {
		return 0, 0, err
	}
	w, h := termbox.Size()
	termbox.Close()
	return w, h, nil
}

func trimFilename(s string, max int) string {
	length := utf8.RuneCountInString(s)
	if length > max {
		new := "..." + s[length-(max-3):]
		return new
	}
	return s
}

func parseRecord(record string) (line string, err error) {
	line = record
	if opts.Docker {
		r := &DockerJSONLogRecord{}
		err = json.Unmarshal([]byte(record), r)
		if err != nil {
			return line, errors.New("JSON Parse Error: " + err.Error())
		}
		line = strings.TrimRight(r.Log, "\n")
	}
	return line, nil
}

func tailFile(file string, c color.Attribute, termWidth int, stdoutLock *sync.Mutex, done chan bool) {
	defer func() { done <- true }()
	colorPrintf := color.New(c).PrintfFunc()

	// limit the size of captured lines in order to accomodate filename on the left hand side
	// which is currently hardcoded to 17 chars. We include 4 additional chars to account for the pipes in
	// the prefix `|filename| `.
	maxLineSize := int(termWidth - (17 + 4))

	config := tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: true,
		Location:  &tail.SeekInfo{-512, os.SEEK_END},
		Poll:      true,
		Logger:    tail.DiscardingLogger,
	}
	// if no file specified, assume stdin
	if file == "" {
		config.Location = nil
		file = "/dev/stdin"
		config.Pipe = true
	}

	t, err := tail.TailFile(file, config)
	if err != nil {
		fmt.Printf("Error reading file %s: %s\n", file, err)
		return
	}

	// throw away the first "line" as it is likely a partial line due to the seeking function of the tail
	// library being byte specific and not line aware. A partial line would fail json parsing when -d is used so
	// it's best to skip it.
	<-t.Lines

	for record := range t.Lines {
		text, err := parseRecord(record.Text)
		if err != nil {
			fmt.Println("Error parsing line:\n", err)
			continue
		}

		// split long strings into multiple lines to preserve formatting of the left-hand-side
		lines := []string{text}
		if len(text) > maxLineSize {
			lines = util.PartitionString(text, maxLineSize)
		}

		stdoutLock.Lock()
		for _, line := range lines {
			colorPrintf("|%-17s| %s\n", trimFilename(file, 17), line)
		}
		stdoutLock.Unlock()
	}
	err = t.Wait()
	if err != nil {
		fmt.Println(err)
	}
}

func main() {
	err := parseArgs(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	width, _, err := getTermSize()
	if err != nil {
		log.Fatal("Unable to determine terminal width: ", err)
	}

	done := make(chan bool)
	var stdoutLock = &sync.Mutex{}

	for idx, filename := range opts.Positionals.Filenames {
		c := colors[idx%len(colors)]
		go tailFile(filename, c, width, stdoutLock, done)
	}
	for _, _ = range opts.Positionals.Filenames {
		<-done
	}
}
