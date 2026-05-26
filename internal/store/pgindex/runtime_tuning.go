package pgindex

import "context"

const defaultBinaryUpsertDBChunkSize = 250

type binaryUpsertChunkSizeContextKey struct{}

func WithBinaryUpsertChunkSize(ctx context.Context, size int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if size <= 0 {
		return ctx
	}
	return context.WithValue(ctx, binaryUpsertChunkSizeContextKey{}, size)
}

func binaryUpsertChunkSizeFromContext(ctx context.Context) int {
	if ctx != nil {
		if size, ok := ctx.Value(binaryUpsertChunkSizeContextKey{}).(int); ok && size > 0 {
			return size
		}
	}
	return defaultBinaryUpsertDBChunkSize
}
