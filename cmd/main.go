package main

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/Envek/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

var templates = template.Must(template.New("main").ParseGlob("templates/*.html"))
var db *sql.DB

type Payload struct {
	ShortID string `json:"short_id"`
	GA      string `json:"ga"`
	FBP     string `json:"fbp"`
	UA      string `json:"ua"`
	Origin  string `json:"origin"`
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
	log.Println("Collecting data from:", r.RemoteAddr)
	var payload Payload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if payload.Origin == "" {
		payload.Origin = r.Referer()
	}
	// Salva no banco
	_, err := db.Exec(`INSERT INTO payloads (short_id, ga, fbp, ua, origin) VALUES (?, ?, ?, ?, ?)`,
		payload.ShortID, payload.GA, payload.FBP, payload.UA, payload.Origin)
	if err != nil {
		log.Println("Erro ao salvar payload:", err)
		http.Error(w, "Erro ao salvar dados", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{"status": "success", "redirect_to": lookupTargetByShortID(payload.ShortID)}
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
	rows, err := db.Query(`SELECT short_id, ga, fbp, ua, origin FROM payloads WHERE short_id = ?`, shortID)
	if err != nil {
		log.Println("Erro ao buscar payloads:", err)
		http.Error(w, "Erro ao buscar dados", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var filtered []Payload
	for rows.Next() {
		var p Payload
		if err := rows.Scan(&p.ShortID, &p.GA, &p.FBP, &p.UA, &p.Origin); err == nil {
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
	rows, err := db.Query(`SELECT ga, fbp, ua, origin FROM payloads WHERE short_id = ?`, shortID)
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
	for rows.Next() {
		var ga, fbp, ua, origin string
		if err := rows.Scan(&ga, &fbp, &ua, &origin); err == nil {
			total++
			gaCount[ga]++
			fbpCount[fbp]++
			uaCount[ua]++
			originCount[origin]++
		}
	}
	stats := map[string]interface{}{
		"total":        total,
		"ga_count":     gaCount,
		"fbp_count":    fbpCount,
		"ua_count":     uaCount,
		"origin_count": originCount,
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

func main() {
	log.Println("Iniciando url-insight...")
	_ = godotenv.Load()

	dbPath := os.Getenv("URLINSIGHT_DB")
	if dbPath == "" {
		dbPath = ":memory:"
	}
	log.Println("Usando banco de dados em:", dbPath)
	var err error
	log.Println("Conectado ao banco de dados.")
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal("Erro ao abrir banco de dados:", err)
	}
	defer db.Close()
	// Cria tabelas se não existirem
	log.Println("Criando tabelas se não existirem...")
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS shortids (
		short_id TEXT PRIMARY KEY,
		url TEXT
	);`)
	if err != nil {
		log.Fatal("Erro ao criar tabela shortids:", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS payloads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		short_id TEXT,
		ga TEXT,
		fbp TEXT,
		ua TEXT,
		origin TEXT,
		FOREIGN KEY(short_id) REFERENCES shortids(short_id) ON DELETE CASCADE
	);`)
	if err != nil {
		log.Fatal("Erro ao criar tabela payloads:", err)
	}

	log.Println("Configurando handlers...")
	http.HandleFunc("/", handleHome)
	http.HandleFunc("/r/", handleRedirect)
	http.HandleFunc("/collect", handleCollect)
	http.HandleFunc("/show-data", handleShowData)
	http.HandleFunc("/analytics", handleAnalytics)
	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Println("Server failed:", err)
	}
}
