package wiring

import (
	"fmt"

	"github.com/datallboy/gonzb/internal/app"
)

// Build current effective Usenet/NZB Indexer runtime from app context.
func BuildUsenetIndexer(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.UsenetIndexer = nil
		return nil
	}

	rt, err := buildUsenetIndexerRuntime(appCtx)
	if err != nil {
		return err
	}

	appCtx.UsenetIndexer = rt.service
	if rt.scrapeProvider != nil {
		appCtx.AddCloser(rt.scrapeProvider)
	}

	return nil
}
