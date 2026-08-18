package main

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Event struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Message      string    `json:"message"`
	Channel      string    `json:"channel"` // tg | email | both
	ChatID       string    `json:"chat_id"`
	TGToken      string    `json:"tg_token"`
	EmailTo      string    `json:"email_to"`
	EmailSubject string    `json:"email_subject"`
	Enabled      bool      `json:"enabled"`
	NextRun      time.Time `json:"next_run"`
	TriggerDays  int64     `json:"trigger_days"`
	CreatedAt    time.Time `json:"created_at"`
}

// SMTP 配置（QQ 邮箱，复用 smtp-service 技能配置）
const (
	SMTPHost = "smtp.qq.com"
	SMTPPort = "465"
	SMTPUser = "1412360581@qq.com"
	SMTPPass = "aufaoyxceqkdieei"
	SMTPFrom = "Nekomini 事件推送 <1412360581@qq.com>"
)

var db *sql.DB

func mustDB() *sql.DB {
	d, err := sql.Open("sqlite3", "/app/data/scheduler.db")
	if err != nil {
		log.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	// v2: 全新表结构（渠道 tg/email/both），旧数据不迁移
	if _, err := d.Exec(`DROP TABLE IF EXISTS events`); err != nil {
		log.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		message TEXT NOT NULL,
		channel TEXT NOT NULL DEFAULT 'tg',
		chat_id TEXT NOT NULL DEFAULT '',
		tg_token TEXT NOT NULL DEFAULT '',
		email_to TEXT NOT NULL DEFAULT '',
		email_subject TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		trigger_days INTEGER NOT NULL DEFAULT 0,
		first_triggered DATETIME,
		next_run DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Fatal(err)
	}
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

func normalizeChannel(c string) string {
	switch c {
	case "email", "both":
		return c
	default:
		return "tg"
	}
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id,name,message,channel,chat_id,tg_token,email_to,email_subject,enabled,COALESCE(next_run,''),trigger_days,COALESCE(first_triggered,''),created_at FROM events ORDER BY id DESC`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var nextRunStr, firstTriggeredStr string
		if err := rows.Scan(&e.ID, &e.Name, &e.Message, &e.Channel, &e.ChatID, &e.TGToken, &e.EmailTo, &e.EmailSubject, &e.Enabled, &nextRunStr, &e.TriggerDays, &firstTriggeredStr, &e.CreatedAt); err != nil {
			continue
		}
		if nextRunStr != "" {
			if t, err := time.Parse(time.RFC3339, nextRunStr); err == nil {
				e.NextRun = t
			}
		}
		out = append(out, e)
	}
	if out == nil {
		out = []Event{}
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
	e.Channel = normalizeChannel(e.Channel)
	res, err := db.Exec(`INSERT INTO events(name,message,channel,chat_id,tg_token,email_to,email_subject,enabled,trigger_days) VALUES(?,?,?,?,?,?,?,?,?)`,
		e.Name, e.Message, e.Channel, e.ChatID, e.TGToken, e.EmailTo, e.EmailSubject, b2i(e.Enabled), e.TriggerDays)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	e.ID, _ = res.LastInsertId()
	e.CreatedAt = time.Now().UTC()
	if e.TriggerDays > 0 && e.Enabled {
		next := time.Now().UTC().Add(time.Duration(e.TriggerDays) * 24 * time.Hour)
		_, _ = db.Exec(`UPDATE events SET next_run = ? WHERE id = ?`, next.Format(time.RFC3339), e.ID)
		e.NextRun = next
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
	e.Channel = normalizeChannel(e.Channel)
	_, _ = db.Exec(`UPDATE events SET name=?,message=?,channel=?,chat_id=?,tg_token=?,email_to=?,email_subject=?,enabled=?,trigger_days=? WHERE id=?`,
		e.Name, e.Message, e.Channel, e.ChatID, e.TGToken, e.EmailTo, e.EmailSubject, b2i(e.Enabled), e.TriggerDays, idStr)
	if e.TriggerDays > 0 && e.Enabled {
		next := time.Now().UTC().Add(time.Duration(e.TriggerDays) * 24 * time.Hour)
		_, _ = db.Exec(`UPDATE events SET next_run=? WHERE id=?`, next.Format(time.RFC3339), idStr)
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
	var url string
	_ = row.Scan(&url)
	row2 := db.QueryRow(`SELECT value FROM settings WHERE key='background_mode'`)
	var mode string
	_ = row2.Scan(&mode)
	if mode == "" {
		mode = "cover"
	}
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"background_url": url, "background_mode": mode})
}

func settingsPutHandler(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	u := strings.TrimSpace(body["background_url"])
	m := strings.TrimSpace(body["background_mode"])
	if m == "" {
		m = "cover"
	}
	if m != "cover" && m != "stretch" && m != "auto" {
		m = "cover"
	}
	_, _ = db.Exec(`INSERT INTO settings(key,value) VALUES('background_url',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, u)
	_, _ = db.Exec(`INSERT INTO settings(key,value) VALUES('background_mode',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, m)
	w.WriteHeader(204)
}

// ---------- 发送 ----------

func sendTG(token, chatID, text string) error {
	body := map[string]string{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": "false"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("TG %s: %s", resp.Status, strings.TrimSpace(string(rb)))
	}
	return nil
}

func sendEmail(to, subject, body string) error {
	if to == "" {
		return fmt.Errorf("收件人为空")
	}
	if subject == "" {
		subject = "Nekomini 事件推送"
	}
	// QQ 邮箱 465 SSL 直连
	conn, err := tls.Dial("tcp", SMTPHost+":"+SMTPPort, &tls.Config{ServerName: SMTPHost})
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Auth(smtp.PlainAuth("", SMTPUser, SMTPPass, SMTPHost)); err != nil {
		return err
	}
	if err := client.Mail(SMTPUser); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		SMTPFrom, to, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// 测试发送端点：POST /api/test-send
// body: {channel, chat_id, tg_token, email_to, email_subject, message}
func testSendHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel      string `json:"channel"`
		ChatID       string `json:"chat_id"`
		TGToken      string `json:"tg_token"`
		EmailTo      string `json:"email_to"`
		EmailSubject string `json:"email_subject"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	msg := body.Message
	if msg == "" {
		msg = "这是一条测试消息 ✅ 如果你收到了，说明事件推送配置正确。"
	}
	ch := normalizeChannel(body.Channel)
	results := []string{}
	var firstErr error
	if ch == "tg" || ch == "both" {
		if body.TGToken == "" || body.ChatID == "" {
			results = append(results, "TG: 跳过（缺少 bot_token 或 chat_id）")
		} else if err := sendTG(body.TGToken, body.ChatID, msg); err != nil {
			results = append(results, "TG: 失败 - "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
		} else {
			results = append(results, "TG: ✅ 已发送")
		}
	}
	if ch == "email" || ch == "both" {
		if body.EmailTo == "" {
			results = append(results, "邮箱: 跳过（缺少收件人）")
		} else if err := sendEmail(body.EmailTo, body.EmailSubject, msg); err != nil {
			results = append(results, "邮箱: 失败 - "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
		} else {
			results = append(results, "邮箱: ✅ 已发送")
		}
	}
	w.Header().Set("content-type", "application/json")
	if firstErr != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "results": results})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": results})
}

func runScheduler() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().UTC()
		rows, err := db.Query(`SELECT id,name,message,channel,chat_id,tg_token,email_to,email_subject,enabled,trigger_days,first_triggered,next_run FROM events WHERE enabled=1 AND trigger_days>0 AND next_run IS NOT NULL AND next_run<=?`, now.Format(time.RFC3339))
		if err != nil {
			log.Println("scheduler query:", err)
			continue
		}
		for rows.Next() {
			var e Event
			var ftStr, nextStr string
			if err := rows.Scan(&e.ID, &e.Name, &e.Message, &e.Channel, &e.ChatID, &e.TGToken, &e.EmailTo, &e.EmailSubject, &e.Enabled, &e.TriggerDays, &ftStr, &nextStr); err != nil {
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
			// 按渠道发送
			switch e.Channel {
			case "email":
				if err := sendEmail(e.EmailTo, e.EmailSubject, e.Message); err != nil {
					log.Printf("event %d email send error: %v", e.ID, err)
				}
			case "both":
				if err := sendTG(e.TGToken, e.ChatID, e.Message); err != nil {
					log.Printf("event %d tg send error: %v", e.ID, err)
				}
				if err := sendEmail(e.EmailTo, e.EmailSubject, e.Message); err != nil {
					log.Printf("event %d email send error: %v", e.ID, err)
				}
			default: // tg
				if err := sendTG(e.TGToken, e.ChatID, e.Message); err != nil {
					log.Printf("event %d tg send error: %v", e.ID, err)
				}
			}
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

	http.HandleFunc("/api/test-send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		testSendHandler(w, r)
	})

	http.HandleFunc("/api/client-ip", func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("CF-Connecting-IP")
		if ip == "" {
			xff := r.Header.Get("X-Forwarded-For")
			if xff != "" {
				parts := strings.Split(xff, ",")
				ip = strings.TrimSpace(parts[0])
			}
		}
		if ip == "" {
			ip = strings.Split(r.RemoteAddr, ":")[0]
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ip":%q}`, ip)
	})

	http.Handle("/", http.FileServer(http.Dir("./frontend/dist")))
	port := env("PORT", "8080")
	log.Println("listen :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
