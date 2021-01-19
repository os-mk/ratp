package ratp

import (
	"fmt"
	"testing"
)

func TestParseInvalidCfg(t *testing.T) {
	// empty string
	cfg, err := parseCfg("")
	if cfg != nil || err != errBadCfgFormat {
		t.Error()
	}

	// invalid integer value
	cfg, err = parseCfg("not-an-int,8E1")
	if cfg != nil || err == nil {
		t.Error()
	}

	// too large integer value
	cfg, err = parseCfg("2147483648,8E1")
	if cfg != nil || err == nil {
		t.Error()
	}

	// extra symbols
	cfg, err = parseCfg("115200,1E1ZZ")
	if cfg != nil || err != errBadCfgFormat {
		t.Error()
	}

	// invalid data bits
	cfg, err = parseCfg("115200,1E1")
	if cfg != nil || err != errBadDataBits {
		t.Error()
	}

	// invalid pariry
	cfg, err = parseCfg("115200,8X1")
	if cfg != nil || err != errBadParity {
		t.Error()
	}

	// ivalid stop bits
	cfg, err = parseCfg("115200,8E9")
	if cfg != nil || err != errBadStopBits {
		t.Error()
	}
}

func TestParseValidCfg(t *testing.T) {
	baudRates := []int{110, 150, 300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200,
		230400, 460800, 921600}
	dataBits := []int{8}
	parities := []rune{'N', 'O', 'E', 'M', 'S'}
	stopBits := []int{1, 2}
	for _, br := range baudRates {
		for _, db := range dataBits {
			for _, p := range parities {
				for _, sb := range stopBits {
					cfgString := fmt.Sprintf("%d,%d%c%d", br, db, p, sb)
					cfg, err := parseCfg(cfgString)
					if err != nil || cfg == nil || cfg.baudRate != br ||
						cfg.dataBits != db || cfg.parity != p || cfg.stopBits != sb {
						t.Error(cfgString)
					}
				}
			}
		}
	}
}
