// Copyright 2023 Vincent Free
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package otelmiddleware

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.11.0"
	"go.opentelemetry.io/otel/trace"
)

// version is used as the instrumentation version.
const version = "0.1.0"

// TraceOption takes a traceConfig struct and applies changes.
// It can be passed to the TraceWithOptions function to configure a traceConfig struct.
type TraceOption func(*traceConfig)

// traceConfig contains all the configuration for the library.
type traceConfig struct {
	serviceName string
	tracer      trace.Tracer
	propagator  propagation.TextMapPropagator
	attributes  []attribute.KeyValue
}

// TraceWithOptions takes TraceOption's and initializes a new trace.Span.
func TraceWithOptions(opt ...TraceOption) func(next http.Handler) http.Handler {
	// initialize an empty traceConfig.
	config := &traceConfig{}

	// apply the configuration passed to the function.
	for _, o := range opt {
		o(config)
	}
	// check for the traceConfig.tracer if absent use a default value.
	if config.tracer == nil {
		config.tracer = otel.Tracer("github.com/vincentfree/opentelemetry/otelmiddleware", trace.WithInstrumentationVersion(version))
	}
	// check for the traceConfig.propagator if absent use a default value.
	if config.propagator == nil {
		config.propagator = otel.GetTextMapPropagator()
	}
	// check for the traceConfig.serviceName if absent use a default value.
	if config.serviceName == "" {
		config.serviceName = "TracedApplication"
	}
	// the handler that initializes the trace.Span.
	return func(next http.Handler) http.Handler {

		// assign the handler which creates the OpenTelemetry trace.Span.
		fn := func(w http.ResponseWriter, r *http.Request) {
			requestCtx := r.Context()
			// extract the OpenTelemetry span context from the context.Context object.
			ctx := config.propagator.Extract(requestCtx, propagation.HeaderCarrier(r.Header))
			// the standard trace.SpanStartOption options whom are applied to every server handler.
			opts := []trace.SpanStartOption{

				trace.WithAttributes(semconv.NetAttributesFromHTTPRequest("tcp", r)...),
				trace.WithAttributes(semconv.EndUserAttributesFromHTTPRequest(r)...),
				trace.WithAttributes(semconv.HTTPServerAttributesFromHTTPRequest(r.Host, extractRoute(r.RequestURI), r)...),
				trace.WithAttributes(semconv.HTTPClientAttributesFromHTTPRequest(r)...),
				trace.WithAttributes(semconv.TelemetrySDKLanguageGo),
				trace.WithSpanKind(trace.SpanKindServer),
			}
			// check for the traceConfig.attributes if present apply them to the trace.Span.
			if len(config.attributes) > 0 {
				opts = append(opts, trace.WithAttributes(config.attributes...))
			}
			// extract the route name which is used for setting a usable name of the span.
			spanName := extractRoute(r.RequestURI)
			if spanName == "" {
				// no path available
				spanName = r.Proto + " " + r.Method + " /"
			}

			// create a good name to recognize where the span originated.
			spanName = r.Method + " /" + spanName

			// start the actual trace.Span.
			ctx, span := config.tracer.Start(ctx, spanName, opts...)

			defer span.End()

			// pass the span through the request context.
			r = r.WithContext(ctx)
			carrier := propagation.HeaderCarrier(r.Header)
			otel.GetTextMapPropagator().Inject(ctx, carrier)

			// use a wrapper for the http.responseWriter to capture the response status code;
			// this information is added to the spans generated by the middleware
			wrapperRes := NewWrapResponseWriter(w, r.ProtoMajor)

			// serve the request to the next middleware.
			next.ServeHTTP(wrapperRes, r)
			// add the response status code to the span
			if span.IsRecording() {
				span.SetAttributes(semconv.HTTPAttributesFromHTTPStatusCode(wrapperRes.Status())...)
			}
		}

		return http.HandlerFunc(fn)
	}
}

// Trace uses the TraceWithOptions without additional options, this is a shorthand for TraceWithOptions().
func Trace(next http.Handler) http.Handler {
	return TraceWithOptions()(next)
}

// extract the route name.
func extractRoute(uri string) string {
	return uri[1:]
}

// WithTracer is a TraceOption to inject your own trace.Tracer.
func WithTracer(tracer trace.Tracer) TraceOption {
	return func(c *traceConfig) {
		c.tracer = tracer
	}
}

// WithPropagator is a TraceOption to inject your own propagation.
func WithPropagator(p propagation.TextMapPropagator) TraceOption {
	return func(c *traceConfig) {
		c.propagator = p
	}
}

// WithServiceName is a TraceOption to inject your own serviceName.
func WithServiceName(serviceName string) TraceOption {
	return func(c *traceConfig) {
		c.serviceName = serviceName
	}
}

// WithAttributes is a TraceOption to inject your own attributes.
// Attributes are applied to the trace.Span.
func WithAttributes(attributes ...attribute.KeyValue) TraceOption {
	return func(c *traceConfig) {
		c.attributes = attributes
	}
}
