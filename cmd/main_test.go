package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
