package id

import "strings"

const baseChars = "0123456789"

var districts = map[string]string{
	"DEH": "DE",
}

var breeds = map[string]string{
	"Shahiwal": "SH",
	"Deoni":    "DN",
	"HF":       "HF",
	"Jersey":   "JS",
	"Gir":      "GR",
}

func toBase(n uint64) string {
	if n == 0 {
		return "0"
	}
	var sb strings.Builder
	for n > 0 {
		r := n % 10
		n /= 10
		sb.WriteByte(baseChars[r])
	}

	result := sb.String()
	runes := []byte(result)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
