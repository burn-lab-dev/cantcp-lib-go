package cantcp

// crc8 is a table-driven CRC-8 calculator with a configurable polynomial.
// It implements the CRC-8/SMBus model with the default polynomial 0x07:
// init 0x00, no input/output reflection, no final XOR.
type crc8 struct {
	tab [256]byte
}

// newCRC8 builds the lookup table for the given polynomial.
// The table belongs only to the returned value; nothing is shared globally.
func newCRC8(poly byte) crc8 {
	var c crc8
	for i := range c.tab {
		v := byte(i)
		for range 8 {
			if v&0x80 != 0 {
				v = v<<1 ^ poly
			} else {
				v <<= 1
			}
		}
		c.tab[i] = v
	}
	return c
}

// sum returns the CRC-8 of b.
func (c *crc8) sum(b []byte) byte {
	var v byte
	for _, x := range b {
		v = c.tab[v^x]
	}
	return v
}
