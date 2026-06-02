package shortener

import (
	"errors"
	"strings"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const base = uint64(62)

// Encode converts an unsigned integer into its base62 string representation.
func Encode(n uint64) string {
	if n == 0 {
		return string(alphabet[0])
	}

	var sb strings.Builder
	for n > 0 {
		remainder := n % base
		sb.WriteByte(alphabet[remainder])
		n = n / base
	}

	return reverse(sb.String())
}

// Decode converts a base62 string back into its integer value.
func Decode(s string) (uint64, error) {
	var n uint64
	for _, char := range s {
		idx := strings.IndexRune(alphabet, char)
		if idx == -1 {
			return 0, errors.New("invalid character in base62 string")
		}
		n = n*base + uint64(idx)
	}
	return n, nil
}

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
