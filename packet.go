package ratp

import (
	"bytes"
	"errors"
	"fmt"
)

type packet []byte

type flags byte

const headerSize = 4
const crcSize = 2
const maxPacketSize = 261
const maxDataSize = maxPacketSize - headerSize - crcSize
const synchLeader = 0x01

const (
	flagSO flags = (1 << iota)
	flagEOR
	flagAN
	flagSN
	flagRST
	flagFIN
	flagACK
	flagSYN
)

const (
	posSynchLeader = iota
	posFlags
	posLength
	posChecksum
	posData
)

func (p *packet) flags() flags {
	return flags((*p)[posFlags])
}

func (p *packet) String() string {
	if len(*p) < 4 {
		return fmt.Sprintf("%X (incorrect format)", (*p)[:])
	}

	var buf bytes.Buffer
	buf.WriteString("[SN=")
	f := flags((*p)[posFlags])
	if (f & flagSN) == 0 {
		buf.WriteString("0, AN=")
	} else {
		buf.WriteString("1, AN=")
	}
	if (f & flagAN) == 0 {
		buf.WriteString("0")
	} else {
		buf.WriteString("1")
	}
	if (f & flagSO) != 0 {
		buf.WriteString(", SO")
	}
	if (f & flagEOR) != 0 {
		buf.WriteString(", EOR")
	}
	if (f & flagRST) != 0 {
		buf.WriteString(", RST")
	}
	if (f & flagFIN) != 0 {
		buf.WriteString(", FIN")
	}
	if (f & flagACK) != 0 {
		buf.WriteString(", ACK")
	}
	if (f & flagSYN) != 0 {
		buf.WriteString(", SYN")
	}

	fmt.Fprintf(&buf, "] LEN=%d CS=%X", uint((*p)[posLength]),
		uint((*p)[posChecksum]))
	if len(*p) > 4 {
		fmt.Fprintf(&buf, " DATA=%X CRC=%X", (*p)[posData:len(*p)-2], (*p)[len(*p)-2:])
	}

	fmt.Fprintf(&buf, "\nDump: %X", (*p))

	return buf.String()
}

func makeCksum(b1, b2 byte) byte {
	return (b1 + b2) ^ 0xFF
}

func verifyCksum(b1, b2, ck byte) bool {
	b1 += b2 + ck
	return b1 == 0xFF
}

func isDatalessPacket(f byte) bool {
	return (flags(f) & (flagSYN | flagRST | flagFIN)) != 0
}

func makePacketWithoutData(flags flags, dataLen byte) packet {
	var buf [headerSize]byte
	buf[posSynchLeader] = 0x01
	buf[posFlags] = byte(flags)
	buf[posLength] = dataLen
	buf[posChecksum] = makeCksum(buf[posFlags], buf[posLength])
	return buf[:]
}

func makePacketWithData(flags flags, data []byte) (packet, error) {
	if len(data) > 0 && isDatalessPacket(byte(flags)) {
		return nil, errors.New("Data not allowed with SYN, RST or FIN packets")
	}

	if len(data) > maxDataSize {
		return nil, errors.New("Data is too long")
	}

	if len(data) == 1 {
		var buf [headerSize]byte
		buf[posSynchLeader] = synchLeader
		buf[posFlags] = byte(flags | flagSO)
		buf[posLength] = data[0]
		buf[posChecksum] = makeCksum(buf[posFlags], buf[posLength])
		return buf[:], nil
	}

	var buf [maxPacketSize]byte
	buf[posSynchLeader] = synchLeader
	buf[posFlags] = byte(flags)
	buf[posLength] = byte(len(data))
	buf[posChecksum] = makeCksum(buf[posFlags], buf[posLength])
	copy(buf[posData:], data)
	crc := calcCRC16(data)
	buf[posData+int(buf[posLength])] = byte(crc >> 8)
	buf[posData+int(buf[posLength])+1] = byte(crc & 0xFF)
	return buf[:len(data)+headerSize+crcSize], nil
}

func makePacketDecoder() func([]byte) (packet, int) {
	const (
		SynchDetection int = iota
		HeaderReception
		DataReception
		DataChecksumReception
	)
	state := SynchDetection
	bytesReceived := 0
	var pktBuf [maxPacketSize]byte
	pktBuf[posSynchLeader] = synchLeader
	return func(data []byte) (packet, int) {
		ptr := 0
		for ptr < len(data) {
			switch state {
			case SynchDetection:
				for ptr < len(data) && data[ptr] != synchLeader {
					ptr++
				}
				if ptr == len(data) {
					break
				}
				state = HeaderReception
				bytesReceived = 0
				ptr++

			case HeaderReception:
				switch bytesReceived {
				case 0, 1:
					pktBuf[bytesReceived+1] = data[ptr]
					bytesReceived++
					ptr++
				case 2:
					pktBuf[posChecksum] = data[ptr]
					if verifyCksum(pktBuf[posFlags], pktBuf[posLength], data[ptr]) {
						if !isDatalessPacket(pktBuf[posFlags]) && pktBuf[posLength] > 0 {
							state = DataReception
							bytesReceived = 0
						} else {
							state = SynchDetection
							bytesReceived = 0
							var retBuf [headerSize]byte
							copy(retBuf[:], pktBuf[:])
							ptr++
							return packet(retBuf[:]), ptr
						}
					} else {
						if pktBuf[posFlags] == synchLeader {
							pktBuf[posFlags] = pktBuf[posLength]
							pktBuf[posLength] = pktBuf[posChecksum]
						} else if pktBuf[posLength] == synchLeader {
							pktBuf[posFlags] = pktBuf[posChecksum]
							bytesReceived = 1
						} else if pktBuf[posChecksum] == synchLeader {
							bytesReceived = 0
						} else {
							state = SynchDetection
						}
					}
					ptr++
				}

			case DataReception:
				s := copy(pktBuf[bytesReceived+headerSize:int(pktBuf[posLength])+headerSize],
					data[ptr:])
				ptr += s
				bytesReceived += s
				if bytesReceived == int(pktBuf[posLength]) {
					state = DataChecksumReception
					bytesReceived = 0
				}

			case DataChecksumReception:
				pktBuf[posData+int(pktBuf[posLength])+int(bytesReceived)] = data[ptr]
				ptr++
				bytesReceived++
				if bytesReceived == crcSize {
					state = SynchDetection
					crc := uint16(pktBuf[posData+int(pktBuf[posLength])]) << 8
					crc |= uint16(pktBuf[posData+int(pktBuf[posLength])+1])
					if checkCRC16(pktBuf[posData:posData+int(pktBuf[posLength])], crc) {
						retBuf := make([]byte, int(pktBuf[posLength])+headerSize+crcSize)
						copy(retBuf, pktBuf[:])
						return retBuf[:], ptr
					}
				}
			}
		}
		return nil, ptr
	}
}
