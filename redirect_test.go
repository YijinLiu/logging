package logging_test

import (
	"testing"
	"time"

	"github.com/YijinLiu/logging"
	"github.com/stretchr/testify/assert"
)

func init() {
	if loc, err := time.LoadLocation("America/Los_Angeles"); err != nil {
		logging.Fatal(err)
	} else {
		time.Local = loc
	}
}

func TestLogFileStartTs(t *testing.T) {
	assert.Equal(t, int64(1525213976), logging.LogFileStartTs(
		"20180501-153256_20180501-153256_1.log"))
	assert.Equal(t, int64(1521669476), logging.LogFileStartTs(
		"20180321-115417_20180321-145756_3.log"))
}

func TestLogLineTs(t *testing.T) {
	assert.Equal(t, int64(1525214265), logging.LogLineTs(
		"2018/05/01 15:37:45 [server/home_server/main_common.go:184] Running HTTPS server"))
	assert.Equal(t, int64(0), logging.LogLineTs(
		"############## Camect HTTP Server #############"))
}
