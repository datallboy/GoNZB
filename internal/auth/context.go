package auth

import "context"

type principalContextKey struct{}

func ContextWithPrincipal(ctx context.Context, principal *Principal) context.Context {
	if ctx == nil || principal == nil {
		return ctx
	}
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	if ctx == nil {
		return nil, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(*Principal)
	return principal, ok && principal != nil
}
