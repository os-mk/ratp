package ratp

import "testing"

func matchPacket(p packet, orig []byte) bool {
	if len(p) != len(orig) {
		return false
	}
	for i, b := range p {
		if orig[i] != b {
			return false
		}
	}
	if p.flags() != flags(orig[posFlags]) {
		return false
	}
	return true
}

func TestMakePacketWithoutData(t *testing.T) {
	p := makePacketWithoutData(0, 0)
	orig := []byte{0x01, 0x00, 0x00, 0xFF}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	p = makePacketWithoutData(flagSYN, 0xFF)
	orig = []byte{0x01, 0x80, 0xFF, 0x80}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}
}

func TestMakePacketWithtData(t *testing.T) {
	_, err := makePacketWithData(flagSYN, []byte{1, 2, 3})
	if err == nil {
		t.Error()
	}

	_, err = makePacketWithData(0, make([]byte, 300))
	if err == nil {
		t.Error()
	}

	p, err := makePacketWithData(0, []byte{0x42})
	orig := []byte{0x01, 0x01, 0x42, 0xBC}
	if err != nil || !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	p, err = makePacketWithData(0, []byte{0x42, 0x43})
	orig = []byte{0x01, 0x00, 0x02, 0xFD, 0x42, 0x43, 0x13, 0x09}
	if err != nil || !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}
}

func TestMakePacketDecoder(t *testing.T) {
	// Byte by byte
	dec := makePacketDecoder()
	if dec == nil {
		t.Error()
	}

	orig := []byte{0x01, 0x00, 0x02, 0xFD, 0x42, 0x43, 0x13, 0x09}
	for i := 0; i < len(orig)-1; i++ {
		p, n := dec(orig[i : i+1])
		if p != nil || n != 1 {
			t.Error()
		}
	}
	p, n := dec(orig[len(orig)-1:])
	if p == nil || n != 1 {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	// Bad first byte
	dec = makePacketDecoder()
	p, n = dec([]byte{0x00})
	if p != nil || n != 1 {
		t.Error()
	}
	p, n = dec(orig)
	if p == nil || n != len(orig) {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	// Fake synch near
	dec = makePacketDecoder()
	p, n = dec([]byte{0x01})
	if p != nil || n != 1 {
		t.Error()
	}
	p, n = dec(orig)
	if p == nil || n != len(orig) {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	// Fake synch in one byte
	dec = makePacketDecoder()
	p, n = dec([]byte{0x01, 0x00})
	if p != nil || n != 2 {
		t.Error()
	}
	p, n = dec(orig)
	if p == nil || n != len(orig) {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	// Fake synch in two bytes
	dec = makePacketDecoder()
	p, n = dec([]byte{0x01, 0x00, 0x00})
	if p != nil || n != 3 {
		t.Error()
	}
	p, n = dec(orig)
	if p == nil || n != len(orig) {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	// Fake synch in three bytes
	dec = makePacketDecoder()
	p, n = dec([]byte{0x01, 0x00, 0x00, 0x00})
	if p != nil || n != 4 {
		t.Error()
	}
	p, n = dec(orig)
	if p == nil || n != len(orig) {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}

	// Without data
	orig = []byte{0x01, 0x80, 0xFF, 0x80}
	dec = makePacketDecoder()
	p, n = dec(orig)
	if p == nil || n != len(orig) {
		t.Error()
	}
	if !matchPacket(p, orig) {
		t.Errorf("%X != %X\n", p, orig)
	}
}

func TestDecoderRegress(t *testing.T) {
	dec := makePacketDecoder()
	if dec == nil {
		t.Error()
	}

	buf := [...]byte{0x01, 0x48, 0xFF, 0xB8, 0x30}
	for i := range buf {
		p, n := dec(buf[i : i+1])
		if p != nil || n != 1 {
			t.Error()
		}
	}
}
