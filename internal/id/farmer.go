package id

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
)

func GeneratePublicID(stateCode, districtCode string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	n := binary.BigEndian.Uint64(b[:])
	suffix := toBase(n)

	if len(suffix) < 6 {
		suffix = strings.Repeat("0", 6-len(suffix)) + suffix
	} else if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}

	district, ok := districts[districtCode]
	if !ok {
		district = districtCode[:2]
	}

	return stateCode + district + suffix, nil
}
