package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) {
	t.Helper()
	var err error
	db, err = openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
}

func do(h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestShortenAndCollect(t *testing.T) {
	setup(t)

	if rec := do(handleShorten, "POST", "/shorten", `{"url":"https://example.com","short_id":"abc"}`); rec.Code != http.StatusCreated {
		t.Fatalf("shorten: %d %s", rec.Code, rec.Body)
	}
	if rec := do(handleShorten, "POST", "/shorten", `{"url":"https://example.com","short_id":"abc"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicado deveria dar 409, deu %d", rec.Code)
	}
	if rec := do(handleShorten, "POST", "/shorten", `{"url":"https://example.com"}`); rec.Code != http.StatusCreated {
		t.Fatalf("id gerado: %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"url":"javascript:alert(1)"}`, `{"url":"/relativa"}`, `{"url":"https://x.com","short_id":"a b"}`} {
		if rec := do(handleShorten, "POST", "/shorten", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s deveria dar 400, deu %d", body, rec.Code)
		}
	}
	if rec := do(handleShorten, "GET", "/shorten", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET deveria dar 405, deu %d", rec.Code)
	}

	if rec := do(handleCollect, "POST", "/collect", `{"short_id":"abc","ga":"g"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://example.com") {
		t.Fatalf("collect: %d %s", rec.Code, rec.Body)
	}
	if rec := do(handleCollect, "POST", "/collect", `{"short_id":"naoexiste"}`); rec.Code != http.StatusNotFound {
		t.Errorf("short_id inexistente deveria dar 404, deu %d", rec.Code)
	}
	if rec := do(handleCollect, "GET", "/collect", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET deveria dar 405, deu %d", rec.Code)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	setup(t)
	if _, err := db.Exec(`INSERT INTO payloads (short_id) VALUES ('fantasma')`); err == nil {
		t.Fatal("chave estrangeira deveria ser aplicada")
	}
}

func TestAdminToken(t *testing.T) {
	setup(t)
	t.Setenv("URLINSIGHT_ADMIN_TOKEN", "segredo")
	if rec := do(handleShorten, "POST", "/shorten", `{"url":"https://example.com"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sem token deveria dar 401, deu %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/shorten", strings.NewReader(`{"url":"https://example.com"}`))
	req.Header.Set("Authorization", "Bearer segredo")
	rec := httptest.NewRecorder()
	handleShorten(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("com token: %d", rec.Code)
	}
}

func seed(t *testing.T, id, target string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO shortids (short_id, url) VALUES (?, ?)", id, target); err != nil {
		t.Fatal(err)
	}
}

func TestHandleRedirect(t *testing.T) {
	setup(t)
	seed(t, "abc", "https://example.com/destino")

	rec := do(handleRedirect, "GET", "/r/abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", rec.Code)
	}
	// html/template escapa as barras ao inserir o valor em um contexto JavaScript.
	if !strings.Contains(rec.Body.String(), `const target = "https:\/\/example.com\/destino";`) {
		t.Errorf("página intermediária deveria conter o destino: %s", rec.Body)
	}

	seed(t, "aspas", `https://example.com/?q="</script><script>alert(1)</script>`)
	rec = do(handleRedirect, "GET", "/r/aspas", "")
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Errorf("destino com aspas escapou do contexto do script: %s", rec.Body)
	}
	if rec := do(handleRedirect, "GET", "/r/naoexiste", ""); rec.Code != http.StatusNotFound {
		t.Errorf("esperava 404, veio %d", rec.Code)
	}
}

func TestHandleHomeAndMetrics(t *testing.T) {
	setup(t)
	for name, h := range map[string]http.HandlerFunc{"home": handleHome, "metrics": handleMetrics} {
		rec := do(h, "GET", "/", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

func TestShowDataAndAnalytics(t *testing.T) {
	setup(t)
	seed(t, "abc", "https://example.com")
	seed(t, "outro", "https://example.org")
	for _, body := range []string{
		`{"short_id":"abc","ga":"g1","fbp":"f1","ua":"ua1","origin":"o1"}`,
		`{"short_id":"abc","ga":"g1","fbp":"f2","ua":"ua1","origin":"o2"}`,
		`{"short_id":"outro","ga":"g9"}`,
	} {
		if rec := do(handleCollect, "POST", "/collect", body); rec.Code != http.StatusOK {
			t.Fatalf("collect: %d %s", rec.Code, rec.Body)
		}
	}

	rec := do(handleShowData, "GET", "/show-data?short_id=abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("show-data: %d", rec.Code)
	}
	var payloads []Payload
	if err := json.Unmarshal(rec.Body.Bytes(), &payloads); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 || payloads[0].ShortID != "abc" || payloads[0].GA != "g1" {
		t.Errorf("payloads inesperados: %+v", payloads)
	}

	rec = do(handleAnalytics, "GET", "/analytics?short_id=abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("analytics: %d", rec.Code)
	}
	var stats struct {
		Total       int            `json:"total"`
		GACount     map[string]int `json:"ga_count"`
		FBPCount    map[string]int `json:"fbp_count"`
		OriginCount map[string]int `json:"origin_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || stats.GACount["g1"] != 2 || stats.FBPCount["f1"] != 1 || len(stats.OriginCount) != 2 {
		t.Errorf("estatísticas inesperadas: %+v", stats)
	}

	// short_id sem registros: lista vazia / total zero.
	if rec := do(handleAnalytics, "GET", "/analytics?short_id=vazio", ""); !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Errorf("esperava total 0: %s", rec.Body)
	}
}

func TestShortIDRequired(t *testing.T) {
	setup(t)
	for name, h := range map[string]http.HandlerFunc{"show-data": handleShowData, "analytics": handleAnalytics} {
		if rec := do(h, "GET", "/"+name, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s sem short_id deveria dar 400, deu %d", name, rec.Code)
		}
	}
}

func TestInvalidAndOversizedBodies(t *testing.T) {
	setup(t)
	huge := `{"url":"https://example.com/` + strings.Repeat("a", maxBodyBytes) + `"}`
	for name, h := range map[string]http.HandlerFunc{"shorten": handleShorten, "collect": handleCollect} {
		if rec := do(h, "POST", "/"+name, `{invalido`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s com JSON inválido deveria dar 400, deu %d", name, rec.Code)
		}
		if rec := do(h, "POST", "/"+name, huge); rec.Code != http.StatusBadRequest {
			t.Errorf("%s com body gigante deveria dar 400, deu %d", name, rec.Code)
		}
	}
}

func TestCollectFallsBackToReferer(t *testing.T) {
	setup(t)
	seed(t, "abc", "https://example.com")
	req := httptest.NewRequest("POST", "/collect", strings.NewReader(`{"short_id":"abc"}`))
	req.Header.Set("Referer", "https://origem.example")
	rec := httptest.NewRecorder()
	handleCollect(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("collect: %d", rec.Code)
	}
	var origin string
	if err := db.QueryRow("SELECT origin FROM payloads").Scan(&origin); err != nil || origin != "https://origem.example" {
		t.Errorf("origin = %q, err = %v", origin, err)
	}
}

func TestSQLiteDSN(t *testing.T) {
	if got := sqliteDSN("a.db"); got != "a.db?_foreign_keys=1" {
		t.Errorf("got %q", got)
	}
	if got := sqliteDSN("file:a.db?cache=shared"); got != "file:a.db?cache=shared&_foreign_keys=1" {
		t.Errorf("got %q", got)
	}
}

func TestReadEndpointsRequireTokenWhenSet(t *testing.T) {
	setup(t)
	seed(t, "abc", "https://example.com")
	t.Setenv("URLINSIGHT_ADMIN_TOKEN", "segredo")

	for name, h := range map[string]http.HandlerFunc{
		"show-data": requireAuth(handleShowData),
		"analytics": requireAuth(handleAnalytics),
	} {
		if rec := do(h, "GET", "/"+name+"?short_id=abc", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s sem token deveria dar 401, deu %d", name, rec.Code)
		}
		req := httptest.NewRequest("GET", "/"+name+"?short_id=abc", nil)
		req.Header.Set("Authorization", "Bearer errado")
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s com token errado deveria dar 401, deu %d", name, rec.Code)
		}
		req.Header.Set("Authorization", "Bearer segredo")
		rec = httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s com token deveria dar 200, deu %d", name, rec.Code)
		}
	}
}

func TestReadEndpointsOpenWithoutToken(t *testing.T) {
	setup(t)
	if rec := do(requireAuth(handleAnalytics), "GET", "/analytics?short_id=abc", ""); rec.Code != http.StatusOK {
		t.Errorf("sem token configurado deveria dar 200, deu %d", rec.Code)
	}
}

func TestCreatedAtAndClicksByDay(t *testing.T) {
	setup(t)
	seed(t, "abc", "https://example.com")
	for i := 0; i < 2; i++ {
		if rec := do(handleCollect, "POST", "/collect", `{"short_id":"abc","created_at":"1999-01-01"}`); rec.Code != http.StatusOK {
			t.Fatalf("collect: %d", rec.Code)
		}
	}

	var payloads []Payload
	rec := do(handleShowData, "GET", "/show-data?short_id=abc", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &payloads); err != nil || len(payloads) != 2 {
		t.Fatalf("payloads: %v %s", err, rec.Body)
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, p := range payloads {
		if !strings.HasPrefix(p.CreatedAt, today) {
			t.Errorf("created_at deveria ser definido pelo servidor (%s), veio %q", today, p.CreatedAt)
		}
	}

	var stats struct {
		ClicksByDay map[string]int `json:"clicks_by_day"`
	}
	rec = do(handleAnalytics, "GET", "/analytics?short_id=abc", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil || stats.ClicksByDay[today] != 2 {
		t.Errorf("clicks_by_day inesperado: %v %+v", err, stats)
	}
}

func TestOpenDBMigratesCreatedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "antigo.db")
	old, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE shortids (short_id TEXT PRIMARY KEY, url TEXT)`,
		`CREATE TABLE payloads (id INTEGER PRIMARY KEY AUTOINCREMENT, short_id TEXT, ga TEXT, fbp TEXT, ua TEXT, origin TEXT)`,
		`INSERT INTO shortids VALUES ('abc', 'https://example.com')`,
		`INSERT INTO payloads (short_id) VALUES ('abc')`,
	} {
		if _, err := old.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	conn, err := openDB(path)
	if err != nil {
		t.Fatalf("migração falhou: %v", err)
	}
	defer conn.Close()
	var createdAt sql.NullString
	if err := conn.QueryRow("SELECT created_at FROM payloads").Scan(&createdAt); err != nil || createdAt.Valid {
		t.Errorf("registro antigo deveria ter created_at NULL: %v %v", err, createdAt)
	}
	// Reabrir não deve falhar (coluna já existe).
	conn2, err := openDB(path)
	if err != nil {
		t.Fatalf("reabrir falhou: %v", err)
	}
	conn2.Close()
}
