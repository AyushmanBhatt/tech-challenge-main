package httpx

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("tech-challenge")

	requestCounter metric.Int64Counter
	errorCounter   metric.Int64Counter
	latencyHist    metric.Float64Histogram
)

func init() {
	var err error

	requestCounter, err = meter.Int64Counter(
		"http_requests_total",
	)
	if err != nil {
		panic(err)
	}

	errorCounter, err = meter.Int64Counter(
		"http_errors_total",
	)
	if err != nil {
		panic(err)
	}

	latencyHist, err = meter.Float64Histogram(
		"http_request_duration_ms",
	)
	if err != nil {
		panic(err)
	}
}

func Metrics() func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			saw := &statusAwareResponseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			start := time.Now()

			handler.ServeHTTP(saw, r)

			duration := float64(time.Since(start).Milliseconds())

			requestCounter.Add(
				r.Context(),
				1,
				metric.WithAttributes(
					attribute.String("method", r.Method),
					attribute.String("path", r.URL.Path),
				),
			)

			latencyHist.Record(
				r.Context(),
				duration,
				metric.WithAttributes(
					attribute.String("method", r.Method),
					attribute.String("path", r.URL.Path),
				),
			)

			if saw.status >= 400 {
				errorCounter.Add(
					r.Context(),
					1,
					metric.WithAttributes(
						attribute.String("method", r.Method),
						attribute.String("path", r.URL.Path),
						attribute.Int("status", saw.status),
					),
				)
			}
		})
	}
}
