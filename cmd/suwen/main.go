// Suwen — open-source AI search engine.
// Phase 1: minimum viable search pipeline (query → retrieve → rank → generate).
// Phase 2: LLM query understanding, Cross-Encoder reranking, SSE streaming.
// Phase 3: query cache, rate limiting, auth, monitoring, production readiness.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/wzhongyou/suwen/internal/cache"
	"github.com/wzhongyou/suwen/internal/config"
	"github.com/wzhongyou/suwen/internal/gateway"
	"github.com/wzhongyou/suwen/internal/generation"
	"github.com/wzhongyou/suwen/internal/middleware"
	"github.com/wzhongyou/suwen/internal/query"
	"github.com/wzhongyou/suwen/internal/ranking"
	"github.com/wzhongyou/suwen/internal/retrieval"
)

var startTime = time.Now()

func main() {
	configPath := flag.String("config", "conf/suwen.toml", "path to TOML config file")
	vortexAddr := flag.String("vortex-addr", "", "Vortex search server address (overrides config)")
	proximiaAddr := flag.String("proximia-addr", "", "Proximia vector server address (overrides config)")
	listenAddr := flag.String("listen", "", "HTTP listen address (overrides config)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if *vortexAddr != "" {
		cfg.Vortex.Addr = *vortexAddr
	}
	if *proximiaAddr != "" {
		cfg.Proximia.Addr = *proximiaAddr
	}
	if *listenAddr != "" {
		cfg.Server.Addr = *listenAddr
	}

	log.Printf("suwen starting...")
	log.Printf("  vortex:    %s", cfg.Vortex.Addr)
	log.Printf("  proximia:  %s", cfg.Proximia.Addr)
	log.Printf("  llm:       %s/%s", cfg.LLM.Provider, cfg.LLM.Model)
	log.Printf("  listen:    %s", cfg.Server.Addr)

	// ---- Build the pipeline ----

	// Query understanding: use LLM parser if enabled, otherwise simple pass-through.
	generator := generation.New(cfg)
	var parser query.Parser
	if cfg.Query.Enabled {
		log.Printf("  query:     LLM parser (%s, timeout=%s)", cfg.Query.Model, cfg.Query.Timeout)
		qGateway := generation.NewGateway(cfg)
		parser = query.NewLLMParser(qGateway, cfg.Query.Model, config.TimeoutDuration(cfg.Query.Timeout))
	} else {
		parser = query.NewSimpleParser()
	}

	searcher := retrieval.NewHybridSearcher(cfg)
	ranker := ranking.NewRanker(cfg)

	// Query cache (Phase 3).
	var queryCache *cache.Cache
	if cfg.Cache.Enabled {
		queryCache = cache.New(cfg.Cache.MaxSize, config.TimeoutDuration(cfg.Cache.TTL))
		log.Printf("  cache:     enabled (max=%d, ttl=%s)", cfg.Cache.MaxSize, cfg.Cache.TTL)
	} else {
		log.Printf("  cache:     disabled")
	}

	// Metrics (Phase 3).
	metrics := middleware.NewMetrics()

	handler := gateway.NewHandler(parser, searcher, ranker, generator, queryCache, metrics)

	// ---- Middleware stack ----

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/api/v1/search", handler.HandleSearch)
	mux.HandleFunc("/api/v1/search/debug", handler.HandleSearchDebug)
	mux.HandleFunc("/api/v1/search/stream", handler.HandleSearchStream)
	mux.HandleFunc("/health", handleHealth(metrics))
	mux.HandleFunc("/metrics", handleMetrics(metrics))

	// Wrap with middleware (innermost first).
	var wrapped http.Handler = mux

	// Logging.
	wrapped = middleware.RequestLog(wrapped)

	// Metrics tracking.
	wrapped = middleware.MetricsMiddleware(metrics)(wrapped)

	// Auth (Phase 3).
	if cfg.Auth.Enabled && len(cfg.Auth.Keys) > 0 {
		auth := middleware.NewAuth(cfg.Auth.Keys)
		wrapped = middleware.RequireAuth(auth)(wrapped)
		log.Printf("  auth:      enabled (%d keys)", len(cfg.Auth.Keys))
	} else {
		log.Printf("  auth:      disabled")
	}

	// Rate limiting (Phase 3).
	if cfg.RateLimit.Enabled {
		rl := middleware.NewRateLimiter(
			cfg.RateLimit.Rate,
			config.TimeoutDuration(cfg.RateLimit.Interval),
			cfg.RateLimit.Burst,
		)
		wrapped = middleware.RateLimit(rl)(wrapped)
		log.Printf("  ratelimit: %d req/%s (burst=%d)", cfg.RateLimit.Rate, cfg.RateLimit.Interval, cfg.RateLimit.Burst)
	} else {
		log.Printf("  ratelimit: disabled")
	}

	// CORS (outermost, for development).
	wrapped = corsMiddleware(wrapped)

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      wrapped,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("suwen ready on %s", cfg.Server.Addr)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// handleRoot serves a simple API info JSON response.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"service":"suwen","version":"1.0","docs":"https://github.com/wzhongyou/suwen"}`)
}

// handleHealth returns a health check endpoint with metrics summary.
func handleHealth(m *middleware.Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := m.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "ok",
			"uptime_sec": time.Since(startTime).Seconds(),
			"metrics":    snap,
		})
	}
}

// handleMetrics returns a Prometheus-compatible /metrics endpoint.
func handleMetrics(m *middleware.Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := m.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP suwen_requests_total Total number of HTTP requests.\n")
		fmt.Fprintf(w, "# TYPE suwen_requests_total counter\n")
		fmt.Fprintf(w, "suwen_requests_total %d\n", snap.TotalRequests)

		fmt.Fprintf(w, "# HELP suwen_errors_total Total number of HTTP errors.\n")
		fmt.Fprintf(w, "# TYPE suwen_errors_total counter\n")
		fmt.Fprintf(w, "suwen_errors_total %d\n", snap.TotalErrors)

		fmt.Fprintf(w, "# HELP suwen_latency_ms_avg Average request latency in milliseconds.\n")
		fmt.Fprintf(w, "# TYPE suwen_latency_ms_avg gauge\n")
		fmt.Fprintf(w, "suwen_latency_ms_avg %.2f\n", snap.AvgLatencyMS)

		fmt.Fprintf(w, "# HELP suwen_llm_calls_total Total number of LLM calls.\n")
		fmt.Fprintf(w, "# TYPE suwen_llm_calls_total counter\n")
		fmt.Fprintf(w, "suwen_llm_calls_total %d\n", snap.LLMCalls)

		fmt.Fprintf(w, "# HELP suwen_llm_cost_usd_total Total estimated LLM cost in USD.\n")
		fmt.Fprintf(w, "# TYPE suwen_llm_cost_usd_total counter\n")
		fmt.Fprintf(w, "suwen_llm_cost_usd_total %.6f\n", snap.LLMCostUSD)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
