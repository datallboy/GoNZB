package controllers

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

func TestGoNZBNetTraversalAwareTimeoutIncludesNegotiationBudget(t *testing.T) {
	requestTimeout := 15 * time.Second
	appCtx := &app.Context{Config: &config.Config{}}
	if got := gonzbnetTraversalAwareTimeout(appCtx, requestTimeout); got != requestTimeout {
		t.Fatalf("direct timeout = %s", got)
	}
	appCtx.Config.GoNZBNet.Traversal.Enabled = true
	appCtx.Config.GoNZBNet.Traversal.ConnectTimeoutSeconds = 25
	if got := gonzbnetTraversalAwareTimeout(appCtx, requestTimeout); got != 40*time.Second {
		t.Fatalf("traversal timeout = %s", got)
	}
}
