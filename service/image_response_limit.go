package service

import (
	"context"
	"errors"
	"io"
)

var ErrImageStudioResponseTooLarge = errors.New("image studio upstream response exceeds the configured size limit")

type imageStudioResponseLimitContextKey struct{}

func WithImageStudioResponseLimit(ctx context.Context, maximum int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maximum <= 0 {
		return ctx
	}
	return context.WithValue(ctx, imageStudioResponseLimitContextKey{}, maximum)
}

func ImageStudioResponseLimit(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	maximum, ok := ctx.Value(imageStudioResponseLimitContextKey{}).(int64)
	return maximum, ok && maximum > 0
}

// ReadRelayResponseBody applies a hard limit only to image-studio requests.
// Other relay callers retain the existing behavior.
func ReadRelayResponseBody(ctx context.Context, body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, io.ErrUnexpectedEOF
	}
	maximum, limited := ImageStudioResponseLimit(ctx)
	if !limited {
		return io.ReadAll(body)
	}
	responseBody, err := io.ReadAll(io.LimitReader(body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > maximum {
		return nil, ErrImageStudioResponseTooLarge
	}
	return responseBody, nil
}
