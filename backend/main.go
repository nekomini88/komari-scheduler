package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Event struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Message     string    `json:"message"`
	ChatID      string    `json:"chat_id"`
	TGToken     string    `json:"tg_token"`
	Enabled     bool      `json:"enabled"`
	NextRun     time.Time `json:"next_run"`
	TriggerDays int64     `json:"trigger_days"`
	CreatedAt   time.Time `json:"created_at"`
}

var db *sql.DB

func mustDB() *sql.DB {
	d, err := sql.Open("sqlite3", "/app/data/scheduler.db")
	if err != nil {
		log.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		message TEXT NOT NULL,
		chat_id TEXT NOT NULL,
		tg_token TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		trigger_days INTEGER NOT NULL DEFAULT 0,
		first_triggered DATETIME,
		next_run DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Fatal(err)
	}
	_, _ = d.Exec(`ALTER TABLE events ADD COLUMN trigger_days INTEGER NOT NULL DEFAULT 0`)
	_, _ = d.Exec(`ALTER TABLE events ADD COLUMN first_triggered DATETIME`)
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		log.Fatal(err)
	}
	return d
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id,name,message,chat_id,tg_token,enabled,COALESCE(next_run,''),trigger_days,COALESCE(first_triggered,''),created_at FROM events ORDER BY id DESC`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var nextRunStr, firstTriggeredStr string
		if err := rows.Scan(&e.ID, &e.Name, &e.Message, &e.ChatID, &e.TGToken, &e.Enabled, &nextRunStr, &e.TriggerDays, &firstTriggeredStr, &e.CreatedAt); err != nil {
			continue
		}
		if nextRunStr != "" {
			if t, err := time.Parse(time.RFC3339, nextRunStr); err == nil {
				e.NextRun = t
			}
		}
		out = append(out, e)
	}
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func createHandler(w http.ResponseWriter, r *http.Request) {
	var e Event
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if e.TriggerDays < 0 {
		e.TriggerDays = 0
	}
	res, err := db.Exec(`INSERT INTO events(name,message,chat_id,tg_token,enabled,trigger_days) VALUES(?,?,?,?,?,?)`,
		e.Name, e.Message, e.ChatID, e.TGToken, b2i(e.Enabled), e.TriggerDays)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	e.ID, _ = res.LastInsertId()
	if e.TriggerDays > 0 && e.Enabled {
		next := time.Now().UTC().Add(time.Duration(e.TriggerDays) * 24 * time.Hour)
		_, _ = db.Exec(`UPDATE events SET next_run = ? WHERE id = ?`, next, e.ID)
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(e)
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/events/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		http.Error(w, "missing id", 400)
		return
	}
	var e Event
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if e.TriggerDays < 0 {
		e.TriggerDays = 0
	}
	_, _ = db.Exec(`UPDATE events SET name=?,message=?,chat_id=?,tg_token=?,enabled=?,trigger_days=? WHERE id=?`,
		e.Name, e.Message, e.ChatID, e.TGToken, b2i(e.Enabled), e.TriggerDays, idStr)
	if e.TriggerDays > 0 && e.Enabled {
		next := time.Now().UTC().Add(time.Duration(e.TriggerDays) * 24 * time.Hour)
		_, _ = db.Exec(`UPDATE events SET next_run=? WHERE id=?`, next, idStr)
	} else {
		_, _ = db.Exec(`UPDATE events SET next_run=NULL WHERE id=?`, idStr)
	}
	w.WriteHeader(204)
}

func toggleHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/events/")
	idStr = strings.TrimSuffix(idStr, "/toggle")
	if idStr == "" {
		http.Error(w, "missing id", 400)
		return
	}
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	_, _ = db.Exec(`UPDATE events SET enabled = NOT enabled WHERE id = ?`, idStr)
	w.WriteHeader(204)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/events/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		http.Error(w, "missing id", 400)
		return
	}
	_, _ = db.Exec(`DELETE FROM events WHERE id = ?`, idStr)
	w.WriteHeader(204)
}

func settingsGetHandler(w http.ResponseWriter, r *http.Request) {
	row := db.QueryRow(`SELECT value FROM settings WHERE key='background_url'`)
	var v string
	_ = row.Scan(&v)
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"background_url": v})
}

func settingsPutHandler(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	u := strings.TrimSpace(body["background_url"])
	_, _ = db.Exec(`INSERT INTO settings(key,value) VALUES('background_url',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, u)
	w.WriteHeader(204)
}

func sendTG(token, chatID, text string) {
	body := map[string]string{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": "false"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("TG send error:", err)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log.Println("TG sent:", chatID)
}

func runScheduler() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().UTC()
		rows, err := db.Query(`SELECT id,message,chat_id,tg_token,enabled,trigger_days,first_triggered,next_run FROM events WHERE enabled=1 AND trigger_days>0 AND next_run IS NOT NULL AND next_run<=?`, now.Format(time.RFC3339))
		if err != nil {
			log.Println("scheduler query:", err)
			continue
		}
		for rows.Next() {
			var e Event
			var ftStr, nextStr string
			if err := rows.Scan(&e.ID, &e.Message, &e.ChatID, &e.TGToken, &e.Enabled, &e.TriggerDays, &ftStr, &nextStr); err != nil {
				continue
			}
			var ft time.Time
			if t, err := time.Parse(time.RFC3339, ftStr); err == nil {
				ft = t
			}
			if ft.IsZero() {
				ft = now
				_, _ = db.Exec(`UPDATE events SET first_triggered=? WHERE id=?`, ft.Format(time.RFC3339), e.ID)
			}
			sendTG(e.TGToken, e.ChatID, e.Message)
			newNext := now.Add(time.Duration(e.TriggerDays) * 24 * time.Hour)
			_, _ = db.Exec(`UPDATE events SET next_run=? WHERE id=?`, newNext.Format(time.RFC3339), e.ID)
		}
		rows.Close()
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	db = mustDB()
	go runScheduler()

	http.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listHandler(w, r)
		case http.MethodPost:
			createHandler(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})

	http.HandleFunc("/api/settings/background", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			settingsGetHandler(w, r)
		case http.MethodPut:
			settingsPutHandler(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})

	http.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/toggle") {
			toggleHandler(w, r)
			return
		}
		switch r.Method {
		case http.MethodPut:
			updateHandler(w, r)
		case http.MethodDelete:
			deleteHandler(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})

	http.Handle("/", http.FileServer(http.Dir("./frontend/dist")))
	port := env("PORT", "8080")
	log.Println("listen :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
