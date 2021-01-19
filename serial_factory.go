package ratp

import (
	"errors"
	"github.com/tarm/serial"
	"io"
	"strconv"
	"strings"
	"time"
)

type portCfg struct {
	baudRate int
	dataBits int
	stopBits int
	parity   rune
}

var errBadCfgFormat error = errors.New("Bad config format. Should be like this: 115200,8E1")
var errBadDataBits error = errors.New("Only 8-bit communication supported")
var errBadParity error = errors.New("Only those parity modes supported: 'N', 'O', 'E', 'M', 'S'")
var errBadStopBits error = errors.New("Only 1 or 2 stop bits supported")

// Parses format like this: 115200,8E1
func parseCfg(cfg string) (*portCfg, error) {
	parts := strings.SplitN(cfg, ",", 2)
	if len(parts) != 2 || len(parts[1]) != 3 {
		return nil, errBadCfgFormat
	}
	uint64BaudRate, err := strconv.ParseUint(parts[0], 10, 31)
	if err != nil {
		return nil, err
	}

	var result portCfg

	result.baudRate = int(uint64BaudRate)

	if parts[1][0] == '8' {
		result.dataBits = 8
	} else {
		return nil, errBadDataBits
	}

	switch parts[1][1] {
	case 'N', 'O', 'E', 'M', 'S':
		result.parity = rune(parts[1][1])
	default:
		return nil, errBadParity
	}

	switch parts[1][2] {
	case '1':
		result.stopBits = 1
	case '2':
		result.stopBits = 2
	default:
		return nil, errBadStopBits
	}

	return &result, nil
}

func openPort(port string, portCfg string, readTimeout time.Duration) (io.ReadWriteCloser, error) {
	cfg, err := parseCfg(portCfg)
	if err != nil {
		return nil, err
	}

	tty, err := serial.OpenPort(&serial.Config{
		Name:        port,
		Baud:        cfg.baudRate,
		Size:        byte(cfg.dataBits),
		StopBits:    serial.StopBits(cfg.stopBits),
		Parity:      serial.Parity(cfg.parity),
		ReadTimeout: readTimeout})
	return tty, err
}
