// This file contains wrapper functions of package log.
// We always log the file / line, provide Vlog and use different colors for different levels.

package logging

// #include "logging.h"
import "C"

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	COLOR_ERROR   = "\033[0;31m" // Red
	COLOR_WARNING = "\033[0;33m" // Yellow
	COLOR_SUCCESS = "\033[0;32m" // Green
	COLOR_NONE    = "\033[0m"
)

var (
	logChanBufSizeFlag = flag.Int("log-chan-buf-size", 10000, "")

	wg              sync.WaitGroup
	logCh           chan logEntry
	droppedLogLines uint32

	nlogMu  sync.Mutex
	nlogMap map[string]int // "file:line" => count
)

func init() {
	flag.IntVar((*int)(unsafe.Pointer(&C.verbose_log_level)), "v", 1, "Logging verbose level")
	logCh = make(chan logEntry, 10000)
	wg.Add(1)
	go processLogs()
	nlogMap = make(map[string]int)
}

func Fatal(v ...interface{}) {
	file, line := getFileLine()
	logText(file, line, -1, fmt.Sprint(v...))
	Close()
	os.Exit(1)
}

func Fatalf(format string, v ...interface{}) {
	file, line := getFileLine()
	logText(file, line, -1, fmt.Sprintf(format, v...))
	Close()
	os.Exit(1)
}

func Print(v ...interface{}) {
	file, line := getFileLine()
	logText(file, line, 1, fmt.Sprint(v...))
}

func Printf(format string, v ...interface{}) {
	file, line := getFileLine()
	logText(file, line, 1, fmt.Sprintf(format, v...))
}

func VerboseLevel() int {
	return int(C.verbose_log_level)
}

func SetVerboseLevel(level int) {
	C.verbose_log_level = C.int(level)
}

func Vlog(level int, v ...interface{}) {
	if level <= VerboseLevel() {
		file, line := getFileLine()
		logText(file, line, level, fmt.Sprint(v...))
	}
}

func Vlogf(level int, format string, v ...interface{}) {
	if level <= VerboseLevel() {
		file, line := getFileLine()
		logText(file, line, level, fmt.Sprintf(format, v...))
	}
}

// Log one every N times.
func Nlog(n, level int, v ...interface{}) {
	prefix := getFileLinePrefix()
	if cnt := incNLogCnt(prefix); cnt%n == 1 {
		Vlog(level, v...)
	}
}

func Nlogf(n, level int, format string, v ...interface{}) {
	prefix := getFileLinePrefix()
	if cnt := incNLogCnt(prefix); cnt%n == 1 {
		Vlogf(level, format, v...)
	}
}

func incNLogCnt(key string) int {
	nlogMu.Lock()
	defer nlogMu.Unlock()
	nlogMap[key]++
	return nlogMap[key]
}

type VLogger struct {
	level int
}

func NewVLogger(level int) *VLogger {
	return &VLogger{level}
}

func (l *VLogger) Fatal(v ...interface{}) {
	Fatal(v...)
}

func (l *VLogger) Fatalf(format string, v ...interface{}) {
	Fatalf(format, v...)
}

func (l *VLogger) Fatalln(v ...interface{}) {
	Fatal(v...)
}

func (l *VLogger) Print(v ...interface{}) {
	Vlog(l.level, v...)
}

func (l *VLogger) Printf(format string, v ...interface{}) {
	Vlogf(l.level, format, v...)
}

func (l *VLogger) Println(v ...interface{}) {
	Vlog(l.level, v...)
}

func vlogPrefix(level int) string {
	if level < 0 {
		return COLOR_ERROR
	} else if level == 0 {
		return COLOR_WARNING
	} else if level >= 3 {
		return COLOR_SUCCESS
	}
	return ""
}

func vlogSuffix(level int) string {
	if level <= 0 || level >= 3 {
		return COLOR_NONE
	}
	return ""
}

// Returns the source file name after "go/src/".
func srcFilePath(path string) string {
	const START_AFTER = "go/src/"
	if index := strings.LastIndex(path, START_AFTER); index != -1 {
		return path[index+len(START_AFTER):]
	}
	return path
}

func getFileLine() (string, int) {
	// Find the first caller outside of package logging.
	for i := 2; ; i++ {
		if _, file, line, ok := runtime.Caller(i); ok {
			srcFile := srcFilePath(file)
			if strings.HasPrefix(srcFile, "logging/") {
				continue
			}
			return srcFile, line
		}
		break
	}
	return "", 0
}

func fileLinePrefix(file string, line int) string {
	if file != "" {
		if line > 0 {
			return fmt.Sprintf("[%s:%d] ", file, line)
		} else {
			return fmt.Sprintf("[%s] ", file)
		}
	}
	return "[unknown] "
}

func getFileLinePrefix() string {
	file, line := getFileLine()
	return fileLinePrefix(file, line)
}

func logText(file string, line, level int, text string) {
	// Send to a channel instead of log directly so we could dedup.
	select {
	case logCh <- logEntry{file: file, line: line, level: level, text: text}:
	default:
		atomic.AddUint32(&droppedLogLines, 1)
	}
}

//export cLogText
func cLogText(file *C.char, line, level C.int, text *C.char) {
	logText(srcFilePath(C.GoString(file)), int(line), int(level), C.GoString(text))
}

//export Close
func Close() error {
	close(logCh)
	// Wait for "logCh" to be flushed.
	wg.Wait()
	CloseRedirector()
	return nil
}

type logEntry struct {
	file, text  string
	line, level int
}

func processLogs() {
	defer wg.Done()
	var lastLogLine string
	var lastLogLineRepeatCount int
	for le := range logCh {
		if dropped := atomic.SwapUint32(&droppedLogLines, 0); dropped > 0 {
			log.Printf("%s%d log lines were dropped.%s", COLOR_WARNING,
				dropped, COLOR_NONE)
		}
		if lastLogLine == le.text {
			lastLogLineRepeatCount++
		} else {
			if lastLogLineRepeatCount > 0 {
				log.Printf("%sLast line repeated %d times.%s", COLOR_SUCCESS,
					lastLogLineRepeatCount, COLOR_NONE)
				lastLogLineRepeatCount = 0
			}
			lastLogLine = le.text
			log.Print(fileLinePrefix(le.file, le.line), vlogPrefix(le.level), le.text,
				vlogSuffix(le.level))
		}
	}
}
