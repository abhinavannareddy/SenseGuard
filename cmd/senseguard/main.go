package main

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type SensingData struct {
	ObjectID string  `json:"object_id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Type     string  `json:"type"`
}

type Config struct {
	DatabasePath      string
	OPAURL            string
	DefaultBudget     float64
	RequestCost       float64
	GridSize          float64
	NoiseScale        float64
	ConsumerTokens    map[string]string
	TLSCertFile       string
	TLSKeyFile        string
	RequireTLS        bool
	OPACertFile       string
	OPAClientCertFile string
	OPAClientKeyFile  string
}

type App struct {
	cfg                 Config
	ledger              *BudgetLedger
	data                []SensingData
	randomFloat         func() (float64, error)
	client              *http.Client
	consumerTokenHashes map[[32]byte]string
}

func NewApp(cfg Config) (*App, error) {
	if !finitePositive(cfg.DefaultBudget) {
		cfg.DefaultBudget = 5
	}
	if !finitePositive(cfg.RequestCost) {
		cfg.RequestCost = 1
	}
	if !finitePositive(cfg.GridSize) || cfg.GridSize < 1e-6 || cfg.GridSize > 1e6 {
		cfg.GridSize = 10
	}
	if !finitePositive(cfg.NoiseScale) || cfg.NoiseScale < 1e-9 || cfg.NoiseScale > 1e6 {
		cfg.NoiseScale = 3
	}
	if strings.TrimSpace(cfg.DatabasePath) == "" {
		cfg.DatabasePath = filepath.Join("data", "senseguard.db")
	}
	if err := validateOPAConfig(cfg); err != nil {
		return nil, err
	}
	ledger, err := OpenBudgetLedger(cfg.DatabasePath, cfg.DefaultBudget)
	if err != nil {
		return nil, err
	}
	tokenHashes := make(map[[32]byte]string, len(cfg.ConsumerTokens))
	for consumer, token := range cfg.ConsumerTokens {
		if !validConsumerID(consumer) || len(token) < 32 || strings.TrimSpace(token) != token {
			_ = ledger.Close()
			return nil, errors.New("consumer credentials are invalid")
		}
		hash := sha256.Sum256([]byte(token))
		if _, exists := tokenHashes[hash]; exists {
			_ = ledger.Close()
			return nil, errors.New("consumer credentials must be unique")
		}
		tokenHashes[hash] = consumer
	}
	client := &http.Client{Timeout: 2 * time.Second}
	if cfg.OPACertFile != "" || cfg.OPAClientCertFile != "" || cfg.OPAClientKeyFile != "" {
		if (cfg.OPAClientCertFile == "") != (cfg.OPAClientKeyFile == "") {
			_ = ledger.Close()
			return nil, errors.New("OPA client certificate and key must both be configured")
		}
		caPEM, err := os.ReadFile(cfg.OPACertFile)
		if err != nil {
			_ = ledger.Close()
			return nil, errors.New("could not load OPA CA certificate")
		}
		// Trust only the configured CA for OPA. Adding system roots would let
		// any generally trusted public CA issue a certificate for this endpoint.
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			_ = ledger.Close()
			return nil, errors.New("OPA CA file contains no valid certificates")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
		if cfg.OPAClientCertFile != "" {
			clientCert, err := tls.LoadX509KeyPair(cfg.OPAClientCertFile, cfg.OPAClientKeyFile)
			if err != nil {
				_ = ledger.Close()
				return nil, errors.New("could not load OPA client certificate")
			}
			tlsConfig.Certificates = []tls.Certificate{clientCert}
		}
		transport.TLSClientConfig = tlsConfig
		client.Transport = transport
	}
	cfg.ConsumerTokens = nil
	return &App{
		cfg:    cfg,
		ledger: ledger,
		data: []SensingData{
			{ObjectID: "vehicle-101", X: 10.4, Y: 20.7, Type: "vehicle"},
			{ObjectID: "vehicle-102", X: 12.1, Y: 22.2, Type: "vehicle"},
			{ObjectID: "person-201", X: 11.6, Y: 21.4, Type: "person"},
			{ObjectID: "person-202", X: 30.2, Y: 35.8, Type: "person"},
			{ObjectID: "vehicle-103", X: 31.5, Y: 36.1, Type: "vehicle"},
		},
		randomFloat:         secureUniform53,
		client:              client,
		consumerTokenHashes: tokenHashes,
	}, nil
}

func (a *App) Close() error {
	if a.ledger == nil {
		return nil
	}
	return a.ledger.Close()
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use GET"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", a.readinessHandler)
	mux.Handle("/sensing", a.authenticated(http.HandlerFunc(a.sensingHandler)))
	mux.Handle("/budget", a.authenticated(http.HandlerFunc(a.budgetHandler)))
	return securityHeaders(mux)
}

type consumerContextKey struct{}

func (a *App) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		authorizationValues := r.Header.Values("Authorization")
		if len(authorizationValues) != 1 || !strings.HasPrefix(header, prefix) || len(header) <= len(prefix) || strings.ContainsAny(header[len(prefix):], " \t\r\n") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		hash := sha256.Sum256([]byte(header[len(prefix):]))
		consumer := ""
		for expectedHash, identity := range a.consumerTokenHashes {
			if subtle.ConstantTimeCompare(hash[:], expectedHash[:]) == 1 {
				consumer = identity
			}
		}
		if consumer == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		claimed := strings.TrimSpace(r.Header.Get("X-Consumer-ID"))
		if claimed != "" && claimed != consumer {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "consumer identity does not match credentials"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), consumerContextKey{}, consumer)))
	})
}

func authenticatedConsumer(r *http.Request) string {
	consumer, _ := r.Context().Value(consumerContextKey{}).(string)
	return consumer
}

func validConsumerID(consumer string) bool {
	return consumer != "" && len(consumer) <= 128 && strings.TrimSpace(consumer) == consumer
}

func validateTLSConfig(certFile, keyFile string, required bool) error {
	if (certFile == "") != (keyFile == "") || (required && certFile == "") {
		return errors.New("TLS certificate and key must both be configured")
	}
	return nil
}

func parseRequireTLS(raw string) (bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.EqualFold(value, "false") {
		return false, nil
	}
	if strings.EqualFold(value, "true") {
		return true, nil
	}
	return false, errors.New("REQUIRE_TLS must be true or false")
}

func validateOPAURL(raw string, requireTLS bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("OPA_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("OPA_URL must use HTTP or HTTPS")
	}
	if requireTLS && parsed.Scheme != "https" {
		return errors.New("OPA_URL must use HTTPS when OPA TLS is configured or REQUIRE_TLS is true")
	}
	return nil
}

func validateOPAConfig(cfg Config) error {
	clientCertConfigured := cfg.OPAClientCertFile != ""
	clientKeyConfigured := cfg.OPAClientKeyFile != ""
	if clientCertConfigured != clientKeyConfigured {
		return errors.New("OPA client certificate and key must both be configured")
	}
	tlsMaterialConfigured := cfg.OPACertFile != "" || clientCertConfigured || clientKeyConfigured
	if tlsMaterialConfigured && cfg.OPACertFile == "" {
		return errors.New("OPA_CA_FILE is required when OPA TLS credentials are configured")
	}
	if cfg.RequireTLS && (!tlsMaterialConfigured || !clientCertConfigured) {
		return errors.New("REQUIRE_TLS requires the OPA CA certificate and client certificate and key")
	}
	if cfg.OPAURL == "" && !cfg.RequireTLS && !tlsMaterialConfigured {
		return nil
	}
	return validateOPAURL(cfg.OPAURL, cfg.RequireTLS || tlsMaterialConfigured)
}

func parseConsumerTokens(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("CONSUMER_TOKENS is required")
	}
	var tokens map[string]string
	if err := json.Unmarshal([]byte(raw), &tokens); err != nil || len(tokens) == 0 {
		return nil, errors.New("CONSUMER_TOKENS must be a non-empty JSON object")
	}
	seen := make(map[[32]byte]struct{}, len(tokens))
	for consumer, token := range tokens {
		if !validConsumerID(consumer) || len(token) < 32 || strings.TrimSpace(token) != token {
			return nil, errors.New("CONSUMER_TOKENS contains an invalid identity or token")
		}
		hash := sha256.Sum256([]byte(token))
		if _, ok := seen[hash]; ok {
			return nil, errors.New("CONSUMER_TOKENS must use unique tokens")
		}
		seen[hash] = struct{}{}
	}
	return tokens, nil
}

func (a *App) readinessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use GET"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := a.ledger.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database unavailable"})
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OPAURL, "/")+"/health", nil)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "policy service unavailable"})
		return
	}
	response, err := a.client.Do(request)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "policy service unavailable"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "policy service unavailable"})
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *App) sensingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use GET"})
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GET requests must not include a body"})
		return
	}
	consumer := authenticatedConsumer(r)
	purpose := strings.TrimSpace(r.URL.Query().Get("purpose"))
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if consumer == "" || purpose == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "authenticated consumer and purpose query parameter are required"})
		return
	}
	if len(purpose) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "purpose must be at most 64 bytes"})
		return
	}
	if mode == "" {
		mode = "coarse"
	}
	if mode != "coarse" && mode != "noisy" && mode != "aggregate" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be coarse, noisy, or aggregate"})
		return
	}
	allowed, err := a.authorize(consumer, purpose, mode)
	if err != nil {
		log.Printf("event=authorization_error consumer=%q purpose=%q", consumer, purpose)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "policy service unavailable"})
		return
	}
	if !allowed {
		log.Printf("event=access_denied consumer=%q purpose=%q mode=%q", consumer, purpose, mode)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request denied by privacy policy"})
		return
	}
	out := make([]SensingData, 0, len(a.data))
	for _, d := range a.data {
		switch mode {
		case "coarse":
			d.X = math.Round(d.X/a.cfg.GridSize) * a.cfg.GridSize
			d.Y = math.Round(d.Y/a.cfg.GridSize) * a.cfg.GridSize
			d.ObjectID = "object-redacted"
		case "noisy":
			xNoise, err := a.laplace(a.cfg.NoiseScale)
			if err != nil {
				log.Printf("event=noise_generation_error consumer=%q", consumer)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "privacy transform unavailable"})
				return
			}
			yNoise, err := a.laplace(a.cfg.NoiseScale)
			if err != nil {
				log.Printf("event=noise_generation_error consumer=%q", consumer)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "privacy transform unavailable"})
				return
			}
			d.X += xNoise
			d.Y += yNoise
			d.ObjectID = "object-redacted"
		case "aggregate":
			// Aggregate output is handled below; do not return individual records.
		}
		if mode != "aggregate" {
			out = append(out, d)
		}
	}
	if mode == "aggregate" {
		counts := map[string]int{}
		for _, d := range a.data {
			counts[d.Type]++
		}
		remaining, ok, full, err := a.ledger.Charge(consumer, a.cfg.RequestCost)
		if err != nil {
			budgetUnavailable(w, consumer)
			return
		}
		if full {
			ledgerFull(w)
			return
		}
		if !ok {
			budgetDenied(w, consumer, purpose, remaining)
			return
		}
		log.Printf("event=access_allowed consumer=%q purpose=%q mode=%q budget_remaining=%.2f", consumer, purpose, mode, remaining)
		writeJSON(w, http.StatusOK, map[string]any{
			"mode": "aggregate", "counts": counts,
			"privacy":          "counts only; no object IDs or coordinates",
			"budget_remaining": remaining,
		})
		return
	}
	remaining, ok, full, err := a.ledger.Charge(consumer, a.cfg.RequestCost)
	if err != nil {
		budgetUnavailable(w, consumer)
		return
	}
	if full {
		ledgerFull(w)
		return
	}
	if !ok {
		budgetDenied(w, consumer, purpose, remaining)
		return
	}
	log.Printf("event=access_allowed consumer=%q purpose=%q mode=%q budget_remaining=%.2f", consumer, purpose, mode, remaining)
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": mode, "data": out, "budget_remaining": remaining,
		"notice": "simulated data; demo privacy controls, not a certified differential-privacy guarantee",
	})
}

func budgetDenied(w http.ResponseWriter, consumer, purpose string, remaining float64) {
	log.Printf("event=budget_denied consumer=%q purpose=%q", consumer, purpose)
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "privacy budget exhausted", "remaining_budget": remaining})
}

func budgetUnavailable(w http.ResponseWriter, consumer string) {
	log.Printf("event=budget_store_error consumer=%q", consumer)
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "privacy budget service unavailable"})
}

func ledgerFull(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "privacy budget ledger is at capacity"})
}

func (a *App) budgetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use GET"})
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GET requests must not include a body"})
		return
	}
	consumer := authenticatedConsumer(r)
	if consumer == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	remaining, full, err := a.ledger.Snapshot(consumer)
	if err != nil {
		budgetUnavailable(w, consumer)
		return
	}
	if full {
		ledgerFull(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"consumer":         consumer,
		"remaining_budget": remaining,
		"note":             "stored in the configured SQLite database",
	})
}

func (a *App) authorize(consumer, purpose, mode string) (bool, error) {
	payload := map[string]any{"input": map[string]any{
		"consumer": consumer, "purpose": purpose, "mode": mode,
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(a.cfg.OPAURL, "/")+"/v1/data/senseguard/allow", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("OPA returned %s", resp.Status)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return false, err
	}
	if len(responseBody) > 1<<20 {
		return false, errors.New("OPA response exceeds 1 MiB")
	}
	var result struct {
		Result *bool `json:"result"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return false, err
	}
	if result.Result == nil {
		return false, errors.New("OPA response is missing result")
	}
	return *result.Result, nil
}

func (a *App) laplace(scale float64) (float64, error) {
	if !finitePositive(scale) || scale > 1e6 {
		return 0, errors.New("noise scale must be finite and positive")
	}
	if a.randomFloat == nil {
		return 0, errors.New("secure random source is unavailable")
	}
	u, err := a.randomFloat()
	if err != nil {
		return 0, errors.New("secure random source failed")
	}
	if !(u > 0 && u < 1) {
		return 0, errors.New("secure random source returned an invalid value")
	}
	centered := u - 0.5
	noise := -scale * math.Copysign(1, centered) * math.Log1p(-2*math.Abs(centered))
	if math.IsNaN(noise) || math.IsInf(noise, 0) {
		return 0, errors.New("generated noise is invalid")
	}
	return noise, nil
}

func secureUniform53() (float64, error) {
	// Draw from an interior 53-bit uniform interval so the inverse transform
	// never evaluates log(0). crypto/rand.Int samples without modulo bias.
	const denominator = uint64(1) << 53
	n, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetUint64(denominator-1))
	if err != nil {
		return 0, err
	}
	return (float64(n.Uint64()) + 1) / float64(denominator), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func checkReadiness(client *http.Client, url string) error {
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness endpoint returned %s", response.Status)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return nil
}

func envFloat(name string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || !finitePositive(n) {
		log.Printf("invalid %s; using default", name)
		return fallback
	}
	return n
}

func finitePositive(n float64) bool { return n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 2 * time.Second}
		if err := checkReadiness(client, "http://127.0.0.1:8080/readyz"); err != nil {
			log.Printf("health check failed: %v", err)
			os.Exit(1)
		}
		return
	}
	tokens, err := parseConsumerTokens(os.Getenv("CONSUMER_TOKENS"))
	if err != nil {
		log.Printf("SenseGuard startup failed: %v", err)
		os.Exit(1)
	}
	requireTLS, err := parseRequireTLS(os.Getenv("REQUIRE_TLS"))
	if err != nil {
		log.Printf("SenseGuard startup failed: %v", err)
		os.Exit(1)
	}
	cfg := Config{
		OPAURL:            os.Getenv("OPA_URL"),
		DatabasePath:      os.Getenv("DATABASE_PATH"),
		DefaultBudget:     envFloat("DEFAULT_PRIVACY_BUDGET", 5),
		RequestCost:       envFloat("REQUEST_PRIVACY_COST", 1),
		GridSize:          envFloat("COARSE_GRID_SIZE", 10),
		NoiseScale:        envFloat("NOISE_SCALE", 3),
		ConsumerTokens:    tokens,
		TLSCertFile:       os.Getenv("TLS_CERT_FILE"),
		TLSKeyFile:        os.Getenv("TLS_KEY_FILE"),
		RequireTLS:        requireTLS,
		OPACertFile:       os.Getenv("OPA_CA_FILE"),
		OPAClientCertFile: os.Getenv("OPA_CLIENT_CERT_FILE"),
		OPAClientKeyFile:  os.Getenv("OPA_CLIENT_KEY_FILE"),
	}
	if cfg.OPAURL == "" {
		cfg.OPAURL = "http://localhost:8181"
	}
	if err := validateOPAConfig(cfg); err != nil {
		log.Printf("SenseGuard startup failed: %v", err)
		os.Exit(1)
	}
	if err := validateTLSConfig(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.RequireTLS); err != nil {
		log.Printf("SenseGuard startup failed: %v", err)
		os.Exit(1)
	}
	app, err := NewApp(cfg)
	for consumer := range tokens {
		delete(tokens, consumer)
	}
	if err != nil {
		log.Printf("SenseGuard startup failed: %v", err)
		os.Exit(1)
	}
	addr := ":8080"
	log.Printf("SenseGuard listening on %s; TLS=%t", addr, cfg.TLSCertFile != "")
	server := &http.Server{Addr: addr, Handler: app.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	stop, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()
	var serveErr error
	if cfg.TLSCertFile != "" {
		serveErr = server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	} else {
		serveErr = server.ListenAndServe()
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		<-shutdownDone
	} else if serveErr != nil {
		log.Printf("HTTP server failed: %v", serveErr)
	}
	if err := app.Close(); err != nil {
		log.Printf("close budget database: %v", err)
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		os.Exit(1)
	}
}
