package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	_ "github.com/mattn/go-sqlite3"
)

type Event struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CronExpr  string    `json:"cron_expr"`
	Message   string    `json:"message"`
	ChatID    string    `json:"chat_id"`
	TGToken   string    `json:"tg_token"`
	Enabled   bool      `json:"enabled"`
	NextRun   time.Time `json:"next_run"`
	CreatedAt time.Time `json:"created_at"`
}

var db *sql.DB
var c *cron.Cron

func mustDB() *sql.DB {
	d, err := sql.Open("sqlite3", "./scheduler.db")
	if err != nil {
		log.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		cron_expr TEXT NOT NULL,
		message TEXT NOT NULL,
		chat_id TEXT NOT NULL,
		tg_token TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		next_run DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

func i2b(i int64) bool {
	return i != 0
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id,name,cron_expr,message,chat_id,tg_token,enabled,COALESCE(next_run,''),created_at FROM events ORDER BY id DESC`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var nextRunStr string
		if err := rows.Scan(&e.ID, &e.Name, &e.CronExpr, &e.Message, &e.ChatID, &e.TGToken, &e.Enabled, &nextRunStr, &e.CreatedAt); err != nil {
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
	res, err := db.Exec(`INSERT INTO events(name,cron_expr,message,chat_id,tg_token,enabled) VALUES(?,?,?,?,?,?)`,
		e.Name, e.CronExpr, e.Message, e.ChatID, e.TGToken, b2i(e.Enabled))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	e.ID, _ = res.LastInsertId()
	_ = addJobToCron(e.ID, e.CronExpr, e.Message, e.ChatID, e.TGToken, e.Enabled)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(e)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/events/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		http.Error(w, "missing id", 400)
		return
	}
	_, _ = db.Exec(`DELETE FROM events WHERE id = ?`, idStr)
	if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
		c.Remove(cron.EntryID(id))
		_, _ = db.Exec(`UPDATE events SET next_run = NULL WHERE id = ?`, id)
	}
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

func addJobToCron(id int64, cronExpr, message, chatID, token string, enabled bool) error {
	entryID := cron.EntryID(id)
	c.Remove(entryID)
	if !enabled {
		_, _ = db.Exec(`UPDATE events SET next_run = NULL WHERE id = ?`, id)
		return nil
	}
	newID, err := c.AddFunc(cronExpr, func() {
		sendTG(token, chatID, message)
	})
	if err != nil {
		log.Println("bad cron:", id, err)
		_, _ = db.Exec(`UPDATE events SET next_run = NULL WHERE id = ?`, id)
		return err
	}
	next := c.Entry(newID).Next
	_, _ = db.Exec(`UPDATE events SET next_run = ? WHERE id = ?`, next, id)
	return nil
}

func loadJobs() {
	rows, err := db.Query(`SELECT id,cron_expr,message,chat_id,tg_token,enabled FROM events`)
	if err != nil {
		log.Println(err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.CronExpr, &e.Message, &e.ChatID, &e.TGToken, &e.Enabled); err != nil {
			continue
		}
		_ = addJobToCron(e.ID, e.CronExpr, e.Message, e.ChatID, e.TGToken, e.Enabled)
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
	c = cron.New(cron.WithSeconds())
	loadJobs()
	c.Start()

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
	http.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
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
