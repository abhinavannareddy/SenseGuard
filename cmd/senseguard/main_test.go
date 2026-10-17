package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testApp(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	opa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var body struct {
			Input struct {
				Consumer string `json:"consumer"`
				Purpose  string `json:"purpose"`
				Mode     string `json:"mode"`
			} `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		allow := body.Input.Consumer == "researcher" && body.Input.Purpose == "crowd-analysis" &&
			(body.Input.Mode == "coarse" || body.Input.Mode == "noisy" || body.Input.Mode == "aggregate")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": allow})
	}))
	t.Cleanup(opa.Close)
	app := newTestApplication(t, Config{OPAURL: opa.URL, DefaultBudget: 2, RequestCost: 1, GridSize: 10, NoiseScale: 1})
	return app, opa
}

func newTestApplication(t *testing.T, cfg Config) *App {
	t.Helper()
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(t.TempDir(), "senseguard.db")
	}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("create test application: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("close test application: %v", err)
		}
	})
	return app
}

func newTestLedger(t *testing.T, initial float64) *BudgetLedger {
	t.Helper()
	ledger, err := OpenBudgetLedger(filepath.Join(t.TempDir(), "budgets.db"), initial)
	if err != nil {
		t.Fatalf("open test ledger: %v", err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Errorf("close test ledger: %v", err)
		}
	})
	return ledger
}

func TestAllowedRequestReturnsProtectedData(t *testing.T) {
	app, _ := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode=coarse", nil)
	req.Header.Set("X-Consumer-ID", "researcher")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Data []SensingData `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) == 0 {
		t.Fatal("expected simulated data")
	}
	for _, d := range result.Data {
		if d.ObjectID != "object-redacted" {
			t.Fatalf("object ID should be redacted, got %q", d.ObjectID)
		}
		if d.X != 10 && d.X != 20 && d.X != 30 {
			t.Fatalf("x coordinate not coarsened: %v", d.X)
		}
	}
}

func TestDeniedRequestDoesNotReturnData(t *testing.T) {
	app, _ := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=track-person&mode=coarse", nil)
	req.Header.Set("X-Consumer-ID", "unknown")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestBudgetExhaustion(t *testing.T) {
	app, _ := testApp(t)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode=aggregate", nil)
		req.Header.Set("X-Consumer-ID", "researcher")
		rec := httptest.NewRecorder()
		app.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i+1, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode=aggregate", nil)
	req.Header.Set("X-Consumer-ID", "researcher")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rec.Code)
	}
}

func TestMissingConsumerRejected(t *testing.T) {
	app, _ := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis", nil)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestSensingRejectsRequestBodyAndWrongMethod(t *testing.T) {
	app, _ := testApp(t)
	withBody := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis", strings.NewReader("unexpected"))
	withBody.Header.Set("X-Consumer-ID", "researcher")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, withBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("body: want 400, got %d", rec.Code)
	}

	wrongMethod := httptest.NewRequest(http.MethodPost, "/sensing?purpose=crowd-analysis", nil)
	wrongMethod.Header.Set("X-Consumer-ID", "researcher")
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, wrongMethod)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method: want 405, got %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("want Allow: GET, got %q", got)
	}
}

func TestInvalidRequestsAreRejected(t *testing.T) {
	app, _ := testApp(t)
	for _, tc := range []struct{ name, consumer, query string }{
		{"missing purpose", "researcher", "/sensing"},
		{"unsupported mode", "researcher", "/sensing?purpose=crowd-analysis&mode=raw"},
		{"oversized consumer", strings.Repeat("x", 129), "/sensing?purpose=crowd-analysis"},
		{"oversized purpose", "researcher", "/sensing?purpose=" + strings.Repeat("x", 65)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.query, nil)
			req.Header.Set("X-Consumer-ID", tc.consumer)
			rec := httptest.NewRecorder()
			app.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d", rec.Code)
			}
		})
	}
}

func TestInvalidBudgetNumbersCannotBeCharged(t *testing.T) {
	ledger := newTestLedger(t, 2)
	for _, cost := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, charged, _, err := ledger.Charge("researcher", cost); err == nil || charged {
			t.Fatalf("invalid cost %v was not rejected: charged=%v err=%v", cost, charged, err)
		}
	}
	if got, _, err := ledger.Snapshot("researcher"); err != nil || got != 2 {
		t.Fatalf("invalid costs changed budget: got %v, err=%v", got, err)
	}
}

func TestCostTooSmallToChangeBalanceIsRejected(t *testing.T) {
	ledger := newTestLedger(t, 1e308)
	if _, charged, _, err := ledger.Charge("researcher", 1); err != nil || charged {
		t.Fatalf("cost too small to reduce the balance was accepted: charged=%v err=%v", charged, err)
	}
	if got, _, err := ledger.Snapshot("researcher"); err != nil || got != 1e308 {
		t.Fatalf("balance unexpectedly changed: %v err=%v", got, err)
	}
}

func TestInvalidConfigUsesSafeDefaults(t *testing.T) {
	app := newTestApplication(t, Config{DefaultBudget: math.NaN(), RequestCost: math.Inf(1), GridSize: math.SmallestNonzeroFloat64, NoiseScale: math.Inf(-1)})
	if app.cfg.DefaultBudget != 5 || app.cfg.RequestCost != 1 || app.cfg.GridSize != 10 || app.cfg.NoiseScale != 3 {
		t.Fatalf("invalid settings were not replaced with defaults: %+v", app.cfg)
	}
}

func TestEnvironmentNumberParserRejectsNonFiniteAndNonPositiveValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf", "0", "-1", "not-a-number"} {
		t.Setenv("SENSEGUARD_TEST_NUMBER", value)
		if got := envFloat("SENSEGUARD_TEST_NUMBER", 7); got != 7 {
			t.Errorf("%q: want fallback 7, got %v", value, got)
		}
	}
	t.Setenv("SENSEGUARD_TEST_NUMBER", "1.5")
	if got := envFloat("SENSEGUARD_TEST_NUMBER", 7); got != 1.5 {
		t.Fatalf("valid setting changed: got %v", got)
	}
}

type zeroRandomSource struct{}

func (zeroRandomSource) Int63() int64 { return 0 }
func (zeroRandomSource) Seed(int64)   {}

func TestLaplaceNoiseRemainsFiniteAtRandomEndpoint(t *testing.T) {
	app := newTestApplication(t, Config{})
	app.rng = rand.New(zeroRandomSource{})
	if got := app.laplace(1); math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("noise is not finite: %v", got)
	}
}

func TestDeniedAndUnavailableAuthorizationDoNotSpendBudget(t *testing.T) {
	app, opa := testApp(t)
	denied := httptest.NewRequest(http.MethodGet, "/sensing?purpose=track-person&mode=coarse", nil)
	denied.Header.Set("X-Consumer-ID", "researcher")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, denied)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
	if got, _, err := app.ledger.Snapshot("researcher"); err != nil || got != 2 {
		t.Fatalf("denied request spent budget: got %v, err=%v", got, err)
	}

	opa.Close()
	app.cfg.OPAURL = opa.URL
	unavailable := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode=coarse", nil)
	unavailable.Header.Set("X-Consumer-ID", "researcher")
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, unavailable)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	if got, _, err := app.ledger.Snapshot("researcher"); err != nil || got != 2 {
		t.Fatalf("unavailable OPA spent budget: got %v, err=%v", got, err)
	}
}

func TestOPAResponseMustContainValidDecision(t *testing.T) {
	for _, response := range []string{"{}", "not-json", `{"result":true} {"result":false}`} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(response)) }))
			defer server.Close()
			app := newTestApplication(t, Config{OPAURL: server.URL})
			if allowed, err := app.authorize("researcher", "crowd-analysis", "coarse"); err == nil || allowed {
				t.Fatalf("malformed OPA response accepted: allowed=%v err=%v", allowed, err)
			}
		})
	}
}

func TestConcurrentRequestsCannotOverspend(t *testing.T) {
	app, _ := testApp(t)
	const requests = 20
	var wg sync.WaitGroup
	var succeeded atomic.Int32
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode=aggregate", nil)
			req.Header.Set("X-Consumer-ID", "researcher")
			rec := httptest.NewRecorder()
			app.routes().ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				succeeded.Add(1)
			} else if rec.Code != http.StatusTooManyRequests {
				t.Errorf("unexpected status: %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if got := succeeded.Load(); got != 2 {
		t.Fatalf("want exactly 2 requests to fit budget, got %d", got)
	}
	if got, _, err := app.ledger.Snapshot("researcher"); err != nil || got != 0 {
		t.Fatalf("want zero remaining, got %v, err=%v", got, err)
	}
}

func TestPrivacyModesDoNotReturnRawIDs(t *testing.T) {
	for _, mode := range []string{"noisy", "aggregate"} {
		t.Run(mode, func(t *testing.T) {
			app, _ := testApp(t)
			req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode="+mode, nil)
			req.Header.Set("X-Consumer-ID", "researcher")
			rec := httptest.NewRecorder()
			app.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "vehicle-101") || strings.Contains(rec.Body.String(), "person-201") {
				t.Fatal("response contains a raw object ID")
			}
			if mode == "aggregate" && strings.Contains(rec.Body.String(), `"data"`) {
				t.Fatal("aggregate response contains individual records")
			}
			if mode == "noisy" {
				var result struct {
					Data []SensingData `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				for _, d := range result.Data {
					if math.IsNaN(d.X) || math.IsInf(d.X, 0) || math.IsNaN(d.Y) || math.IsInf(d.Y, 0) {
						t.Fatalf("non-finite noisy coordinate: %+v", d)
					}
				}
			}
		})
	}
}

func TestOPARequestTimeoutFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"result":true}`))
	}))
	defer server.Close()
	app := newTestApplication(t, Config{OPAURL: server.URL})
	app.client = &http.Client{Timeout: time.Millisecond}
	if allowed, err := app.authorize("researcher", "crowd-analysis", "coarse"); err == nil || allowed {
		t.Fatalf("timeout should fail closed, allowed=%v err=%v", allowed, err)
	}
}

func TestLedgerRefusesNewConsumersAtCapacity(t *testing.T) {
	ledger := newTestLedger(t, 1)
	tx, err := ledger.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare("INSERT INTO privacy_budgets (consumer_id, remaining) VALUES (?, ?)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLedgerConsumers; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("consumer-%d", i), 0); err != nil {
			t.Fatalf("seed consumer %d: %v", i, err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, charged, full, err := ledger.Charge("one-too-many", 1); err != nil || charged || !full {
		t.Fatalf("expected capacity rejection, charged=%v full=%v err=%v", charged, full, err)
	}
	if remaining, full, err := ledger.Snapshot("one-too-many"); err != nil || remaining != 0 || !full {
		t.Fatalf("new consumer snapshot: remaining=%v full=%v err=%v", remaining, full, err)
	}
	if remaining, full, err := ledger.Snapshot("consumer-0"); err != nil || remaining != 0 || full {
		t.Fatalf("existing consumer snapshot: remaining=%v full=%v err=%v", remaining, full, err)
	}

	app := &App{ledger: ledger}
	req := httptest.NewRequest(http.MethodGet, "/budget", nil)
	req.Header.Set("X-Consumer-ID", "one-too-many")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("full ledger budget lookup: want 503, got %d", rec.Code)
	}
}

func TestSQLiteBudgetPersistsAfterCloseAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	first, err := OpenBudgetLedger(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	remaining, charged, full, err := first.Charge("researcher", 1)
	if err != nil || !charged || full || remaining != 1 {
		t.Fatalf("first charge: remaining=%v charged=%v full=%v err=%v", remaining, charged, full, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := OpenBudgetLedger(path, 99)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close reopened ledger: %v", err)
		}
	})
	remaining, full, err = second.Snapshot("researcher")
	if err != nil || full || remaining != 1 {
		t.Fatalf("reopened balance reset: remaining=%v full=%v err=%v", remaining, full, err)
	}
	remaining, charged, full, err = second.Charge("researcher", 1)
	if err != nil || !charged || full || remaining != 0 {
		t.Fatalf("second charge: remaining=%v charged=%v full=%v err=%v", remaining, charged, full, err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	third, err := OpenBudgetLedger(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := third.Close(); err != nil {
			t.Errorf("close final ledger: %v", err)
		}
	})
	remaining, full, err = third.Snapshot("researcher")
	if err != nil || full || remaining != 0 {
		t.Fatalf("exhausted balance did not persist: remaining=%v full=%v err=%v", remaining, full, err)
	}
}

func TestSQLiteAPIBudgetPersistsAcrossApplicationRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-persistence.db")
	opa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true}`))
	}))
	defer opa.Close()
	cfg := Config{DatabasePath: path, OPAURL: opa.URL, DefaultBudget: 5, RequestCost: 1}

	first := newTestApplication(t, cfg)
	request := func(app *App, target string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("X-Consumer-ID", "researcher")
		rec := httptest.NewRecorder()
		app.routes().ServeHTTP(rec, req)
		return rec
	}
	assertBudget := func(app *App, want float64) {
		t.Helper()
		rec := request(app, "/budget")
		if rec.Code != http.StatusOK {
			t.Fatalf("budget endpoint: want 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var response struct {
			Remaining float64 `json:"remaining_budget"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Remaining != want {
			t.Fatalf("budget: want %v, got %v", want, response.Remaining)
		}
	}

	assertBudget(first, 5)
	if rec := request(first, "/sensing?purpose=crowd-analysis&mode=aggregate"); rec.Code != http.StatusOK {
		t.Fatalf("first authorized request: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertBudget(first, 4)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := newTestApplication(t, cfg)
	assertBudget(second, 4)
	if rec := request(second, "/sensing?purpose=crowd-analysis&mode=aggregate"); rec.Code != http.StatusOK {
		t.Fatalf("request after restart: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertBudget(second, 3)
}

func TestAppRejectsDatabaseInitializationFailure(t *testing.T) {
	parentIsFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentIsFile, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := NewApp(Config{DatabasePath: filepath.Join(parentIsFile, "senseguard.db")})
	if err == nil {
		t.Fatal("expected startup to fail when the database directory is not usable")
	}
}

func TestDatabaseWriteFailureDoesNotReturnProtectedData(t *testing.T) {
	app, _ := testApp(t)
	if _, err := app.ledger.db.Exec("PRAGMA query_only = ON"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sensing?purpose=crowd-analysis&mode=coarse", nil)
	req.Header.Set("X-Consumer-ID", "researcher")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("database write failure: want 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "object-redacted") || strings.Contains(rec.Body.String(), `"data"`) {
		t.Fatalf("protected data returned without confirmed charge: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "privacy budget service unavailable") {
		t.Fatalf("unexpected safe error response: %s", rec.Body.String())
	}
}

func TestClosedDatabaseFailsBudgetAndReadinessChecks(t *testing.T) {
	app := newTestApplication(t, Config{})
	if err := app.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/budget", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Consumer-ID", "researcher")
			rec := httptest.NewRecorder()
			app.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("want 503, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLedgerPingDoesNotWaitBehindBudgetOperation(t *testing.T) {
	ledger := newTestLedger(t, 1)
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if err := ledger.Ping(context.Background()); err == nil {
		t.Fatal("Ping should report a busy ledger without blocking")
	}
}

func TestOverBudgetNewConsumerDoesNotUseLedgerCapacity(t *testing.T) {
	ledger := newTestLedger(t, 1)
	if _, charged, full, err := ledger.Charge("too-expensive", 2); err != nil || charged || full {
		t.Fatalf("over-budget charge: charged=%v full=%v err=%v", charged, full, err)
	}
	var count int
	if err := ledger.db.QueryRow("SELECT COUNT(*) FROM privacy_budgets").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected request created a ledger row: count=%d", count)
	}
}

func TestLedgerRejectsInvalidConsumerIDs(t *testing.T) {
	ledger := newTestLedger(t, 1)
	for _, consumer := range []string{"", strings.Repeat("x", 129)} {
		if _, charged, _, err := ledger.Charge(consumer, 1); err == nil || charged {
			t.Fatalf("invalid consumer accepted: len=%d charged=%v err=%v", len(consumer), charged, err)
		}
	}
}

func TestReadinessReturnsOKWhenDatabaseIsAvailable(t *testing.T) {
	app, _ := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Fatalf("ready check: want 200 ready, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHealthzAndSecurityHeaders(t *testing.T) {
	app, _ := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health check: want 200 ok, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options security header")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing Cache-Control: no-store header")
	}
}

func TestReadinessFailsWhenOPABecomesUnavailable(t *testing.T) {
	app, opa := testApp(t)
	opa.Close()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when OPA is unavailable, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), opa.URL) {
		t.Fatalf("readiness response exposed the OPA address: %s", rec.Body.String())
	}
}

func TestHealthcheckAcceptsOnlySuccessfulReadiness(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			err := checkReadiness(server.Client(), server.URL)
			if status == http.StatusOK && err != nil {
				t.Fatalf("healthy response rejected: %v", err)
			}
			if status != http.StatusOK && err == nil {
				t.Fatal("unhealthy response accepted")
			}
		})
	}
}

func TestNewConsumersHaveIndependentBudgets(t *testing.T) {
	ledger := newTestLedger(t, 3)
	if _, charged, _, err := ledger.Charge("researcher", 1); err != nil || !charged {
		t.Fatalf("charge researcher: charged=%v err=%v", charged, err)
	}
	for consumer, want := range map[string]float64{"researcher": 2, "mall-manager": 3} {
		got, _, err := ledger.Snapshot(consumer)
		if err != nil || got != want {
			t.Errorf("%s balance: want %v, got %v, err=%v", consumer, want, got, err)
		}
	}
}

func TestMultipleLedgerInstancesCannotOverspend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-test.db")
	first, err := OpenBudgetLedger(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := OpenBudgetLedger(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	const requests = 24
	var wg sync.WaitGroup
	var succeeded atomic.Int32
	var unexpected atomic.Int32
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ledger := first
			if i%2 == 1 {
				ledger = second
			}
			_, charged, full, err := ledger.Charge("shared-consumer", 1)
			if err != nil || full {
				unexpected.Add(1)
				return
			}
			if charged {
				succeeded.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if got := succeeded.Load(); got != 4 {
		t.Fatalf("want exactly four charges, got %d", got)
	}
	if got := unexpected.Load(); got != 0 {
		t.Fatalf("unexpected database/capacity errors: %d", got)
	}
	remaining, _, err := first.Snapshot("shared-consumer")
	if err != nil || remaining != 0 {
		t.Fatalf("want zero remaining across ledger instances, got %v, err=%v", remaining, err)
	}
}

func TestConcurrentConsumerCreationCannotExceedCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capacity-test.db")
	first, err := OpenBudgetLedger(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	tx, err := first.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare("INSERT INTO privacy_budgets (consumer_id, remaining) VALUES (?, ?)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLedgerConsumers-1; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("filled-%d", i), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenBudgetLedger(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	var wg sync.WaitGroup
	type outcome struct {
		charged bool
		full    bool
		err     error
	}
	results := make(chan outcome, 2)
	for _, request := range []struct {
		ledger   *BudgetLedger
		consumer string
	}{{first, "last-slot-a"}, {second, "last-slot-b"}} {
		wg.Add(1)
		go func(ledger *BudgetLedger, consumer string) {
			defer wg.Done()
			_, charged, full, err := ledger.Charge(consumer, 1)
			results <- outcome{charged: charged, full: full, err: err}
		}(request.ledger, request.consumer)
	}
	wg.Wait()
	close(results)
	chargedCount, fullCount := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent capacity charge failed: %v", result.err)
		}
		if result.charged {
			chargedCount++
		}
		if result.full {
			fullCount++
		}
	}
	if chargedCount != 1 || fullCount != 1 {
		t.Fatalf("want one consumer admitted and one refused; charged=%d full=%d", chargedCount, fullCount)
	}
	var count int
	if err := first.db.QueryRow("SELECT COUNT(*) FROM privacy_budgets").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != maxLedgerConsumers {
		t.Fatalf("ledger exceeded capacity: count=%d", count)
	}
}
