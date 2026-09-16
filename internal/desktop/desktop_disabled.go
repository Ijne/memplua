//go:build !windows || !desktop

package desktop

import (
	appruntime "crawler/internal/runtime"
	"errors"
)

// Run reports that the current build cannot host the Windows desktop shell.
func Run(appruntime.Options) error {
	return errors.New("desktop UI requires the Windows desktop build; use scripts/dev/build-desktop.ps1, or run 'knowledgecrawler serve' for headless mode")
}
