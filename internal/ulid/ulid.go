// Package ulid generates ULIDs using only the standard library: 48 bits
// of millisecond time + 80 bits of randomness, 26 characters of Crockford
// base32. Strings sort by creation time and are generated without coordination.
package ulid

import (
	"crypto/rand"
	"fmt"
	"time"
)

// Crockford base32 alphabet: no I, L, O, U — unambiguous to read.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New returns a new ULID.
func New() string {
	var bin [16]byte

	ms := uint64(time.Now().UnixMilli())
	for i := range 6 {
		bin[5-i] = byte(ms >> (8 * i))
	}
	if _, err := rand.Read(bin[6:]); err != nil {
		// crypto/rand never fails on supported platforms; if it does,
		// there is no meaningful way to continue.
		panic(fmt.Sprintf("ulid: crypto/rand: %v", err))
	}

	// 128 bits → 26 characters of 5 bits each (the top character uses 3).
	var out [26]byte
	for i := range 26 {
		hi := i * 5 / 8
		shift := 11 - (i*5)%8 // position of the 5-bit window within a byte pair
		var window uint16
		window = uint16(bin[hi]) << 8
		if hi+1 < 16 {
			window |= uint16(bin[hi+1])
		}
		out[i] = alphabet[(window>>shift)&0x1F]
	}
	return string(out[:])
}
