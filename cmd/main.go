package main

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Envek/godotenv"
	"github.com/mattn/go-sqlite3"
)

var templates = template.Must(template.New("main").ParseGlob(templatesGlob()))
var db *sql.DB

type Payload struct {
	ShortID string `json:"short_id"`
	GA      string `json:"ga"`
	FBP     string `json:"fbp"`
	UA      string `json:"ua"`
	Origin  string `json:"origin"`
	// CreatedAt é preenchido pelo servidor (UTC); ignorado no corpo do /collect.
	CreatedAt string `json:"created_at,omitempty"`
}

// templatesGlob permite rodar os testes de dentro de cmd/.
func templatesGlob() string {
	if _, err := os.Stat("templates"); err != nil {
		return "../templates/*.html"
	}
	return "templates/*.html"
}

const maxBodyBytes = 1 << 20

var shortIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

func validateTargetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("a URL deve começar com http:// ou https:// e conter um host")
	}
	return nil
}

func generateShortID() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

func authorized(r *http.Request) bool {
	token := os.Getenv("URLINSIGHT_ADMIN_TOKEN")
	if token == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// requireAuth protege um handler com o mesmo token de administração do /shorten.
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "Não autorizado", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func lookupTargetByShortID(shortID string) string {
	var url string
	err := db.QueryRow("SELECT url FROM shortids WHERE short_id = ?", shortID).Scan(&url)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Println("Erro ao buscar short_id:", err)
		}
		return ""
	}
	return url
}

func handleShorten(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "Não autorizado", http.StatusUnauthorized)
		return
	}
	var req struct {
		URL     string `json:"url"`
		ShortID string `json:"short_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if err := validateTargetURL(req.URL); err != nil {
		http.Error(w, "URL inválida: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ShortID != "" && !shortIDPattern.MatchString(req.ShortID) {
		http.Error(w, "short_id deve ter de 3 a 64 caracteres (letras, números, _ ou -)", http.StatusBadRequest)
		return
	}
	for attempt := 0; attempt < 5; attempt++ {
		id := req.ShortID
		if id == "" {
			var err error
			if id, err = generateShortID(); err != nil {
				http.Error(w, "Erro ao gerar short_id", http.StatusInternalServerError)
				return
			}
		}
		_, err := db.Exec("INSERT INTO shortids (short_id, url) VALUES (?, ?)", id, req.URL)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"short_id": id, "url": req.URL, "path": "/r/" + id})
			return
		}
		var sqlErr sqlite3.Error
		if errors.As(err, &sqlErr) && sqlErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
			if req.ShortID != "" {
				http.Error(w, "short_id já existe", http.StatusConflict)
				return
			}
			continue // colisão em ID gerado: tenta outro
		}
		log.Println("Erro ao salvar short_id:", err)
		http.Error(w, "Erro ao salvar link", http.StatusInternalServerError)
		return
	}
	http.Error(w, "Não foi possível gerar um short_id único", http.StatusInternalServerError)
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	log.Println("Received request for:", r.URL.Path)
	shortID := r.URL.Path[len("/r/"):]
	target := lookupTargetByShortID(shortID)
	if target == "" {
		http.NotFound(w, r)
		return
	}
	templates.ExecuteTemplate(w, "intermediate.html", map[string]string{"Target": target})
}

func handleCollect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	log.Println("Collecting data from:", r.RemoteAddr)
	var payload Payload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	target := lookupTargetByShortID(payload.ShortID)
	if target == "" {
		http.Error(w, "short_id não encontrado", http.StatusNotFound)
		return
	}
	if payload.Origin == "" {
		payload.Origin = r.Referer()
	}
	// Salva no banco
	_, err := db.Exec(`INSERT INTO payloads (short_id, ga, fbp, ua, origin, created_at) VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		payload.ShortID, payload.GA, payload.FBP, payload.UA, payload.Origin)
	if err != nil {
		log.Println("Erro ao salvar payload:", err)
		http.Error(w, "Erro ao salvar dados", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{"status": "success", "redirect_to": target}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Erro ao enviar resposta", http.StatusInternalServerError)
	}
}

func handleShowData(w http.ResponseWriter, r *http.Request) {
	shortID := r.URL.Query().Get("short_id")
	if shortID == "" {
		http.Error(w, "short_id é obrigatório", http.StatusBadRequest)
		return
	}
	log.Println("Showing collected data for short_id:", shortID)
	rows, err := db.Query(`SELECT short_id, ga, fbp, ua, origin, COALESCE(created_at, '') FROM payloads WHERE short_id = ? ORDER BY id`, shortID)
	if err != nil {
		log.Println("Erro ao buscar payloads:", err)
		http.Error(w, "Erro ao buscar dados", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var filtered []Payload
	for rows.Next() {
		var p Payload
		if err := rows.Scan(&p.ShortID, &p.GA, &p.FBP, &p.UA, &p.Origin, &p.CreatedAt); err == nil {
			filtered = append(filtered, p)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(filtered); err != nil {
		http.Error(w, "Erro ao exibir dados", http.StatusInternalServerError)
	}
}

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	shortID := r.URL.Query().Get("short_id")
	if shortID == "" {
		http.Error(w, "short_id é obrigatório", http.StatusBadRequest)
		return
	}
	log.Println("Generating analytics for short_id:", shortID)
	rows, err := db.Query(`SELECT ga, fbp, ua, origin, COALESCE(substr(created_at, 1, 10), '') FROM payloads WHERE short_id = ?`, shortID)
	if err != nil {
		log.Println("Erro ao buscar payloads:", err)
		http.Error(w, "Erro ao buscar dados", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	total := 0
	gaCount := make(map[string]int)
	fbpCount := make(map[string]int)
	uaCount := make(map[string]int)
	originCount := make(map[string]int)
	dayCount := make(map[string]int)
	for rows.Next() {
		var ga, fbp, ua, origin, day string
		if err := rows.Scan(&ga, &fbp, &ua, &origin, &day); err == nil {
			if day != "" {
				dayCount[day]++
			}
			total++
			gaCount[ga]++
			fbpCount[fbp]++
			uaCount[ua]++
			originCount[origin]++
		}
	}
	stats := map[string]interface{}{
		"total":         total,
		"ga_count":      gaCount,
		"fbp_count":     fbpCount,
		"ua_count":      uaCount,
		"origin_count":  originCount,
		"clicks_by_day": dayCount,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, "Erro ao exibir estatísticas", http.StatusInternalServerError)
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	err := templates.ExecuteTemplate(w, "index.html", nil)
	if err != nil {
		http.Error(w, "Erro ao carregar página inicial", http.StatusInternalServerError)
	}
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	err := templates.ExecuteTemplate(w, "metrics.html", nil)
	if err != nil {
		http.Error(w, "Erro ao carregar métricas", http.StatusInternalServerError)
	}
}

// sqliteDSN habilita as chaves estrangeiras (desligadas por padrão no SQLite).
func sqliteDSN(path string) string {
	if strings.Contains(path, "?") {
		return path + "&_foreign_keys=1"
	}
	return path + "?_foreign_keys=1"
}

func openDB(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	// Uma única conexão: com ":memory:" cada conexão do pool teria um banco separado,
	// e o SQLite só admite um escritor por vez.
	conn.SetMaxOpenConns(1)
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS shortids (
			short_id TEXT PRIMARY KEY,
			url TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS payloads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			short_id TEXT,
			ga TEXT,
			fbp TEXT,
			ua TEXT,
			origin TEXT,
			created_at TEXT,
			FOREIGN KEY(short_id) REFERENCES shortids(short_id) ON DELETE CASCADE
		);`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if err := addColumnIfMissing(conn, "payloads", "created_at", "TEXT"); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// addColumnIfMissing migra bancos criados antes da coluna existir.
func addColumnIfMissing(conn *sql.DB, table, column, typ string) error {
	rows, err := conn.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if found {
		return nil
	}
	_, err = conn.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + typ)
	return err
}

func main() {
	log.Println("Iniciando url-insight...")
	_ = godotenv.Load()

	dbPath := os.Getenv("URLINSIGHT_DB")
	if dbPath == "" {
		dbPath = ":memory:"
	}
	log.Println("Usando banco de dados em:", dbPath)
	var err error
	db, err = openDB(dbPath)
	if err != nil {
		log.Fatal("Erro ao inicializar banco de dados:", err)
	}
	defer db.Close()
	log.Println("Banco de dados pronto.")

	log.Println("Configurando handlers...")
	http.HandleFunc("/{$}", handleHome)
	http.HandleFunc("/shorten", handleShorten)
	http.HandleFunc("/r/", handleRedirect)
	http.HandleFunc("/collect", handleCollect)
	http.HandleFunc("/show-data", requireAuth(handleShowData))
	http.HandleFunc("/analytics", requireAuth(handleAnalytics))
	http.HandleFunc("/metrics", handleMetrics)
	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Println("Server failed:", err)
	}
}
