//go:build windows && desktop && desktopdebug && !production

package desktop

import (
	"fmt"
	"os"
	"strconv"
)

// CDP is available only in explicit development builds, never in production.
func debugBrowserArgs() []string {
	port, err := strconv.Atoi(os.Getenv("MEMPLUA_DESKTOP_DEBUG_PORT"))
	if err != nil || port < 1024 || port > 65535 {
		return nil
	}
	return []string{fmt.Sprintf("--remote-debugging-port=%d", port), "--remote-debugging-address=127.0.0.1"}
}

func desktopInstanceID() string {
	port, err := strconv.Atoi(os.Getenv("MEMPLUA_DESKTOP_DEBUG_PORT"))
	if err != nil || port < 1024 || port > 65535 {
		return "memplua.desktop"
	}
	return fmt.Sprintf("memplua.desktop.debug.%d", port)
}
