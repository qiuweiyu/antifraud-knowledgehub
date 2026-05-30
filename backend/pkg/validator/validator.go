package validator

import "strings"

func Required(value string) bool {
	return strings.TrimSpace(value) != ""
}

func Normalize(value string) string {
	return strings.TrimSpace(value)
}
