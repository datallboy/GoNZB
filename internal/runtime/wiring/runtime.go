package wiring

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/app"
)

// Central runtime assembly entrypoint so cmd/main stays thin
func BuildInitialRuntime(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}

	if err := app.LoadAndApplyEffectiveConfig(context.Background(), appCtx); err != nil {
		return err
	}

	if err := BuildDownloader(appCtx); err != nil {
		return err
	}

	if err := BuildUsenetIndexer(appCtx); err != nil {
		return err
	}

	return nil
}
