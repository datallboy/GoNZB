Refactor Recommendations Beyond The Plan
Yes, there are still a few worthwhile cleanup areas outside controllers, but I would keep them tightly scoped.

The biggest remaining foundation issues I see are:

app.Context is still carrying too many concrete package types and too many responsibilities.

Example: context.go still imports concrete aggregator source and store packages to build runtime composition.
Recommendation:
keep Context as the composition container
but continue pushing runtime construction into internal/runtime/wiring
avoid growing Context further
internal/app/context.go still mixes:

interface definitions
concrete runtime creation
storage creation
lifecycle behavior
Recommendation:
over time split this into:
internal/app/contracts.go
internal/app/context.go
leave actual construction in runtime/wiring
This is a good follow-up refactor, but not required before merge.
internal/runtime/wiring/indexer.go is doing a lot.

It builds runtime, owns restart logic, watches settings changes, and manages closers.
Recommendation:
keep as-is for this branch unless it is actively slowing you down
if you do one more runtime cleanup, split:
runtime builder
restart/watch loop
But I would not block merge on this.
Package naming could be improved later.

If you keep adding UI/API features, I’d consider eventually grouping API code by feature package rather than one large controllers package.
Example future shape:
internal/api/queueapi
internal/api/adminapi
internal/api/compatapi

