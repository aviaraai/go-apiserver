package id

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
)

func GenerateGodhaarID(stateCode, districtCode, breedType string) (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	var padded [8]byte
	copy(padded[2:], b[:])
	n := binary.BigEndian.Uint64(padded[:])

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
	breed, ok := breeds[breedType]
	if !ok {
		breed = "OT"
	}

	return stateCode + district + breed + suffix, nil
}
