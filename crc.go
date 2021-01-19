package ratp

func calcCRC16(data []byte) uint16 {
	const poly = 0x1021
	var val uint16 = 0
	for _, b := range data {
		val ^= uint16(b) << 8
		for bit := 0; bit < 8; bit++ {
			msb := val & 0x8000
			val <<= 1
			if msb != 0 {
				val ^= poly
			}
		}
	}
	return val
}

func checkCRC16(data []byte, crc uint16) bool {
	return calcCRC16(data) == crc
}
