//go:build windows && desktop && (!desktopdebug || production)

package desktop

func debugBrowserArgs() []string { return nil }

func desktopInstanceID() string { return "memplua.desktop" }
