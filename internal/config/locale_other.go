//go:build !windows

package config

import (
	"os"
	"strings"
)

func defaultUILanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := os.Getenv(key); locale != "" {
			if strings.HasPrefix(strings.ToLower(locale), "ru") {
				return "ru"
			}
			return "en"
		}
	}
	return "en"
}
