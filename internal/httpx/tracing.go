package httpx

import (
	"net/http"

	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("tech-challenge")

func Tracing() func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			ctx, span := tracer.Start(
				r.Context(),
				r.Method+" "+r.URL.Path,
			)
			defer span.End()

			handler.ServeHTTP(
				w,
				r.WithContext(ctx),
			)
		})
	}
}
