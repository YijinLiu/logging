// File redirect.go contains code redirecting Go log to a local file.

package logging

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// #include "redirect.h"
import "C"

var (
	maxLogFileSizeFlag = flag.Int64(
		"max-log-file-size", 10*1000*1000, "Switch to a new file if current log file is too big.")
	maxLogDirSizeFlag = flag.Int64(
		"max-log-dir-size", 1000*1000*1000, "Recycle old log files if the dir is at least this size")
	alsoLogToStdoutFlag = flag.Bool("also-log-to-stdout", false, "Always write log to stdout.")
	logChanSizeFlag     = flag.Int("log-chan-size", 100,
		"Log is written by a separate goroutine to avoid slowing down logging caller. "+
			"This flag defines the size of the chan to pass data to the goroutine.")
)

var lr *LogRedirector

func CloseRedirector() {
	if lr != nil {
		log.SetOutput(os.Stderr)
		lr.Close()
		lr = nil
	}
}

func RedirectTo(logDir string) (err error) {
	CloseRedirector()
	if lr, err = NewLogRedirector(logDir); err == nil {
		log.SetOutput(lr)
	}
	return
}

// Get at most numBytes log lines:
// - If startTs == 0, return the most recent log lines.
// - If startTs < 0, return log lines before that.
// - If startTs > 0, return log lines after that.
func LogLines(startTs, numBytes int64) []string {
	if lr == nil {
		return nil
	}
	return lr.LogLines(startTs, numBytes)
}

type LogRedirector struct {
	logDir, logFileName string
	startTime           time.Time
	bytesWritten        int64
	numLogFiles         int
	logCh               chan []byte
	stdout              *os.File
	stderr              *os.File
	wg                  sync.WaitGroup
}

func NewLogRedirector(logDir string) (*LogRedirector, error) {
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, err
	}
	lr := &LogRedirector{
		logDir:    logDir,
		startTime: time.Now(),
		logCh:     make(chan []byte, *logChanSizeFlag),
	}
	lr.redirectToNewFile()
	lr.stdout = os.NewFile(uintptr(C.old_stdout), "/dev/stdout")
	lr.stderr = os.NewFile(uintptr(C.old_stderr), "/dev/stderr")
	lr.wg.Add(1)
	go lr.writeLog()
	return lr, nil
}

func (l *LogRedirector) Close() error {
	close(l.logCh)
	l.wg.Wait()
	return nil
}

var logChFullErr = errors.New("log channel is full")

func (l *LogRedirector) Write(p []byte) (n int, err error) {
	logData := make([]byte, len(p))
	copy(logData, p)
	select {
	case l.logCh <- logData:
		return len(logData), nil
	}
	return 0, logChFullErr
}

const (
	LOG_FILE_TIME_FORMAT = "20060102-150405"
	LOG_LINE_TIME_FORMAT = "2006/01/02 15:04:05"
)

var (
	colorRe        = regexp.MustCompile("\033[[][;0-9]+m")
	logFileNameRe  = regexp.MustCompile("^[0-9]{8}-[0-9]{6}_([0-9]{8}-[0-9]{6})_[0-9]+[.]log$")
	logLineStartRe = regexp.MustCompile("^([0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2})")
)

func LogFileStartTs(fileName string) int64 {
	tmStr := logFileNameRe.FindStringSubmatch(fileName)[1]
	tm, _ := time.ParseInLocation(LOG_FILE_TIME_FORMAT, tmStr, time.Local)
	return tm.Unix()
}

func LogLineTs(logLine string) int64 {
	match := logLineStartRe.FindStringSubmatch(logLine)
	if len(match) == 0 {
		return 0
	}
	tm, _ := time.ParseInLocation(LOG_LINE_TIME_FORMAT, match[1], time.Local)
	return tm.Unix()
}

func (l *LogRedirector) LogLines(startTs, numBytes int64) []string {
	// Read in all log files.
	dir, err := os.Open(l.logDir)
	if err != nil {
		fmt.Fprintln(l.stderr, err)
		return nil
	}
	defer dir.Close()
	fis, err := dir.Readdir(0)
	if err != nil {
		fmt.Fprintln(l.stderr, err)
		return nil
	}

	// Filter out non log files.
	n := 0
	for _, fi := range fis {
		if fi.Mode().IsRegular() && logFileNameRe.MatchString(fi.Name()) {
			fis[n] = fi
			n++
		}
	}
	if n == 0 {
		return nil
	}
	fis = fis[:n]

	// Latest files first.
	sort.Slice(fis, func(i, j int) bool {
		return fis[i].Name() > fis[j].Name()
	})

	var lines []string
	if startTs == 0 {
		// Read most recent log lines.
		for _, fi := range fis {
			lines = append(l.logLinesBefore(fi, 0, &numBytes), lines...)
			if numBytes <= 0 {
				break
			}
		}
	} else if startTs > 0 {
		// Read log lines after startTs.
		start := sort.Search(len(fis), func(i int) bool {
			return LogFileStartTs(fis[i].Name()) <= startTs
		})
		if start == len(fis) {
			start = len(fis) - 1
		}
		for i := start; i >= 0; i-- {
			fi := fis[i]
			lines = append(lines, l.logLinesAfter(fi, startTs, &numBytes)...)
			if numBytes <= 0 {
				break
			}
		}
	} else {
		// Read log lines before startTs.
		startTs = -startTs
		start := sort.Search(len(fis), func(i int) bool {
			return LogFileStartTs(fis[i].Name()) < startTs
		})
		for i := start; i < len(fis); i++ {
			fi := fis[i]
			lines = append(l.logLinesBefore(fi, startTs, &numBytes), lines...)
			if numBytes <= 0 {
				break
			}
			// So the next call to logLinesBefore will return logs from the end of the file.
			startTs = 0
		}
	}
	return lines
}

func (l *LogRedirector) logLinesAfter(fi os.FileInfo, startTs int64, numBytes *int64) []string {
	logFile := filepath.Join(l.logDir, fi.Name())
	file, err := os.Open(logFile)
	if err != nil {
		Vlog(0, err)
		return nil
	}
	defer file.Close()

	findFirstLine := LogFileStartTs(fi.Name()) < startTs
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if findFirstLine {
			if ts := LogLineTs(line); ts < startTs {
				continue
			}
			findFirstLine = false
		}
		line = colorRe.ReplaceAllString(strings.TrimSpace(line), "")
		lines = append(lines, line)
		*numBytes -= int64(len(line))
		if *numBytes <= 0 {
			break
		}
	}
	return lines
}

// If startTs <= 0, return log lines from the end of the file.
func (l *LogRedirector) logLinesBefore(fi os.FileInfo, startTs int64, numBytes *int64) []string {
	logFile := filepath.Join(l.logDir, fi.Name())
	file, err := os.Open(logFile)
	if err != nil {
		Vlog(0, err)
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if startTs > 0 && LogLineTs(line) >= startTs {
			break
		}
		lines = append(lines, colorRe.ReplaceAllString(strings.TrimSpace(line), ""))
	}
	for i := len(lines) - 1; i >= 0; i-- {
		*numBytes -= int64(len(lines[i]))
		if *numBytes <= 0 {
			lines = lines[i:]
			break
		}
	}
	return lines
}

func (l *LogRedirector) redirectToNewFile() {
	l.numLogFiles++
	logFileBase := fmt.Sprintf("%s_%s_%d.log",
		l.startTime.Format(LOG_FILE_TIME_FORMAT), time.Now().Format(LOG_FILE_TIME_FORMAT),
		l.numLogFiles)
	if l.logFileName != "" {
		l.writeToCStdout([]byte(fmt.Sprintf("Redirecting log to '%s'.\n", logFileBase)))
	}
	logFileName := filepath.Join(l.logDir, logFileBase)
	if ret := C.redirect_stdout_stderr_to(C.CString(logFileName)); ret != 0 {
		fmt.Fprintf(l.stderr, "Failed to redirect stdout / stderr.")
	} else {
		l.logFileName = logFileName
		l.bytesWritten = 0

		// Upate "latest" symlink.
		symlink := filepath.Join(l.logDir, "latest")
		if err := os.Remove(symlink); err != nil {
			fmt.Fprintln(l.stderr, err)
		}
		if err := os.Symlink(logFileBase, symlink); err != nil {
			fmt.Fprintln(l.stderr, err)
		}

		if *alsoLogToStdoutFlag {
			fmt.Fprintf(l.stdout, "Redirecting log to '%s'.\n", logFileBase)
		}
	}
}

func (l *LogRedirector) writeLog() {
	defer l.wg.Done()
	var data []byte
	for logData := range l.logCh {
		if *alsoLogToStdoutFlag {
			l.stdout.Write(logData)
			l.stdout.Sync()
		}

		if l.logFileName == "" {
			continue
		}

		// Purge the buffer.
		if data == nil {
			data = logData
		} else {
			data = append(data, logData...)
		}
		if len(l.logCh) > 0 {
			continue
		}

		if n := l.writeToCStdout(data); n != len(data) {
			fmt.Fprintf(l.stderr, "Failed to write log: %d != %d", n, len(data))
		} else {
			// Check log file / dir every "block" bytes
			block := *maxLogFileSizeFlag / 10
			old := l.bytesWritten / block
			l.bytesWritten += int64(n)
			if new := l.bytesWritten / block; new != old {
				if fi, err := os.Stat(l.logFileName); err != nil {
					fmt.Fprintln(l.stderr, err)
				} else if fi.Size() >= *maxLogFileSizeFlag {
					l.redirectToNewFile()
					l.recycleOldLogFiles()
				}
			}
		}
		data = nil
	}
}

// Have to call C to write log to avoid problems caused by writing at the same time by Go and C.
func (l *LogRedirector) writeToCStdout(data []byte) int {
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	return int(C.write_to_log((*C.char)(unsafe.Pointer(bh.Data)), C.int(bh.Len)))
}

// This function blocks writeLog. It avoids using the normal logging functions in order
// to prevent deadlock.
func (l *LogRedirector) recycleOldLogFiles() {
	dir, err := os.Open(l.logDir)
	if err != nil {
		fmt.Fprintln(l.stderr, err)
		return
	}
	defer dir.Close()

	fis, err := dir.Readdir(0)
	if err != nil {
		fmt.Fprintln(l.stderr, err)
		return
	}
	var dirSize int64
	for _, fi := range fis {
		dirSize += fi.Size()
	}
	if maxLogDirSize := *maxLogDirSizeFlag; dirSize > maxLogDirSize {
		sort.Slice(fis, func(i, j int) bool {
			return fis[i].ModTime().Before(fis[j].ModTime())
		})
		l.writeToCStdout([]byte(fmt.Sprintf(
			"Trying to recycle old log files from '%s' (%d>%d)...\n",
			l.logDir, dirSize, maxLogDirSize)))
		for _, fi := range fis {
			if !fi.Mode().IsRegular() {
				continue
			}
			file := filepath.Join(l.logDir, fi.Name())
			if file == l.logFileName {
				continue
			}
			l.writeToCStdout([]byte(fmt.Sprintf(
				"Deleting '%s' (%d-%d)...\n", file, dirSize, fi.Size())))
			if err := os.Remove(file); err != nil {
				fmt.Fprintln(l.stderr, err)
				continue
			}
			dirSize -= fi.Size()
			if dirSize <= maxLogDirSize {
				break
			}
		}
	}
}
