package httpapi

import "context"

type tenantContextKey struct{}

func contextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

func tenantFromContext(ctx context.Context) string {
	value, _ := ctx.Value(tenantContextKey{}).(string)
	return value
}
