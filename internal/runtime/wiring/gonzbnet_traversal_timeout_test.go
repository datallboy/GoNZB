package wiring

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

func TestTraversalAwareHTTPTimeoutIncludesNegotiationBudget(t *testing.T) {
	requestTimeout := 15 * time.Second
	appCtx := &app.Context{Config: &config.Config{}}
	if got := traversalAwareHTTPTimeout(appCtx, requestTimeout); got != requestTimeout {
		t.Fatalf("direct timeout = %s", got)
	}
	appCtx.Config.GoNZBNet.Traversal.Enabled = true
	appCtx.Config.GoNZBNet.Traversal.ConnectTimeoutSeconds = 25
	if got := traversalAwareHTTPTimeout(appCtx, requestTimeout); got != 40*time.Second {
		t.Fatalf("traversal timeout = %s", got)
	}
}
