// Phone push delivers opt-in Web Push with durable subscriptions and retries.
package storage

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	_ "github.com/glebarez/go-sqlite"
)

type PushDevice struct {
	webpush.Subscription
	Reminders bool `json:"reminders"`
	Jobs      bool `json:"jobs"`
}
type PushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}
type PhonePush struct {
	db         *sql.DB
	PublicKey  string
	privateKey string
	client     *http.Client
}

func OpenPhonePush(path string) (*PhonePush, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &PhonePush{db: db, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS keys (id INTEGER PRIMARY KEY, private TEXT, public TEXT);
 CREATE TABLE IF NOT EXISTS devices (id TEXT PRIMARY KEY, owner TEXT NOT NULL, data TEXT NOT NULL, created INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS deliveries (device TEXT NOT NULL, event TEXT NOT NULL, payload TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0, next INTEGER NOT NULL DEFAULT 0, created INTEGER NOT NULL, PRIMARY KEY(device,event));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	err = db.QueryRow("SELECT private,public FROM keys WHERE id=1").Scan(&s.privateKey, &s.PublicKey)
	if errors.Is(err, sql.ErrNoRows) {
		s.privateKey, s.PublicKey, err = webpush.GenerateVAPIDKeys()
		if err == nil {
			_, err = db.Exec("INSERT INTO keys VALUES(1,?,?)", s.privateKey, s.PublicKey)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *PhonePush) Close() error { return s.db.Close() }

func (s *PhonePush) HasDevices() (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT count(*) FROM devices").Scan(&n)
	return n > 0, err
}
func PushID(endpoint string) string {
	v := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(v[:])
}

// Restrict outbound destinations to known browser push services, including
// redirects, rather than accepting arbitrary client-supplied server URLs.
func ValidatePushDevice(d PushDevice) error {
	u, e := url.Parse(d.Endpoint)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return errors.New("invalid push endpoint")
	}
	switch u.Hostname() {
	case "fcm.googleapis.com", "updates.push.services.mozilla.com", "web.push.apple.com":
	default:
		return errors.New("unsupported push service")
	}
	key, e := base64.RawURLEncoding.DecodeString(d.Keys.P256dh)
	if e != nil {
		return errors.New("invalid push key")
	}
	if _, e = ecdh.P256().NewPublicKey(key); e != nil {
		return errors.New("invalid push key")
	}
	auth, e := base64.RawURLEncoding.DecodeString(d.Keys.Auth)
	if e != nil || len(auth) != 16 {
		return errors.New("invalid push secret")
	}
	return nil
}
func (s *PhonePush) Save(owner string, d PushDevice) error {
	if err := ValidatePushDevice(d); err != nil {
		return err
	}
	b, _ := json.Marshal(d)
	// A device cannot be silently reassigned to another signed-in identity.
	result, err := s.db.Exec(`INSERT INTO devices VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data WHERE devices.owner=excluded.owner`, PushID(d.Endpoint), owner, string(b), time.Now().Unix())
	if err == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			return errors.New("device belongs to another account; disable notifications there first")
		}
	}
	return err
}
func (s *PhonePush) Remove(owner, endpoint string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("DELETE FROM deliveries WHERE device IN (SELECT id FROM devices WHERE id=? AND owner=?)", PushID(endpoint), owner)
	if e != nil {
		return e
	}
	_, e = tx.Exec("DELETE FROM devices WHERE id=? AND owner=?", PushID(endpoint), owner)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *PhonePush) Device(owner, endpoint string) (*PushDevice, error) {
	var b string
	e := s.db.QueryRow("SELECT data FROM devices WHERE id=? AND owner=?", PushID(endpoint), owner).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var d PushDevice
	e = json.Unmarshal([]byte(b), &d)
	return &d, e
}

// Enqueue is idempotent per event and device. Empty owner means installation
// reminders: subscriptions are admitted only for full-access administrators.
func (s *PhonePush) Enqueue(owner, kind, event string, at time.Time, m PushMessage) error {
	if time.Since(at) > 24*time.Hour {
		return nil
	}
	if title := []rune(m.Title); len(title) > 160 {
		m.Title = string(title[:160])
	}
	if body := []rune(m.Body); len(body) > 500 {
		m.Body = string(body[:500])
	}
	rows, e := s.db.Query("SELECT id,owner,data,created FROM devices")
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id, o, b string
		var created int64
		if e = rows.Scan(&id, &o, &b, &created); e != nil {
			rows.Close()
			return e
		}
		var d PushDevice
		if e = json.Unmarshal([]byte(b), &d); e != nil {
			continue
		}
		if at.Unix() < created || (owner != "" && owner != o) {
			continue
		}
		if kind == "reminder" && !d.Reminders || kind == "job" && !d.Jobs {
			continue
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	b, _ := json.Marshal(m)
	for _, id := range ids {
		if _, e = s.db.Exec("INSERT OR IGNORE INTO deliveries(device,event,payload,created) VALUES(?,?,?,?)", id, event, string(b), time.Now().Unix()); e != nil {
			return e
		}
	}
	return nil
}
func (s *PhonePush) Test(ctx context.Context, owner, endpoint string) error {
	d, e := s.Device(owner, endpoint)
	if e != nil {
		return e
	}
	if d == nil {
		return errors.New("enable notifications first")
	}
	b, _ := json.Marshal(PushMessage{Title: "Brain", Body: "Phone notifications are connected. You can lock your screen and test a reminder next.", URL: "/", Tag: "brain-test"})
	status, e := s.send(ctx, d, b)
	if e != nil {
		return errors.New("push service unavailable; please retry")
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("push service returned %d", status)
	}
	return nil
}
func (s *PhonePush) send(ctx context.Context, d *PushDevice, b []byte) (int, error) {
	res, e := webpush.SendNotificationWithContext(ctx, b, &d.Subscription, &webpush.Options{HTTPClient: s.client, Subscriber: "https://github.com/huynle/brain-api", VAPIDPublicKey: s.PublicKey, VAPIDPrivateKey: s.privateKey, TTL: 86400, Urgency: webpush.UrgencyHigh})
	if e != nil {
		return 0, e
	}
	defer res.Body.Close()
	return res.StatusCode, nil
}
func (s *PhonePush) Deliver(ctx context.Context) error {
	// Sources replay only the last day; retain dedupe rows for two days.
	if _, err := s.db.Exec("DELETE FROM deliveries WHERE created < ?", time.Now().Add(-48*time.Hour).Unix()); err != nil {
		return err
	}
	rows, e := s.db.Query(`SELECT d.device,d.event,d.payload,d.attempts,s.data FROM deliveries d JOIN devices s ON s.id=d.device WHERE d.state='pending' AND d.next<=? LIMIT 32`, time.Now().Unix())
	if e != nil {
		return e
	}
	type item struct {
		id, event, payload, data string
		attempts                 int
	}
	var items []item
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.id, &i.event, &i.payload, &i.attempts, &i.data); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, i := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var d PushDevice
		if e = json.Unmarshal([]byte(i.data), &d); e != nil {
			return e
		}
		// Preferences may have changed since enqueueing.
		if len(i.event) > 9 && i.event[:9] == "reminder:" && !d.Reminders || len(i.event) > 4 && i.event[:4] == "job:" && !d.Jobs {
			_, e = s.db.Exec("UPDATE deliveries SET state='cancelled' WHERE device=? AND event=?", i.id, i.event)
			if e != nil {
				return e
			}
			continue
		}
		status, sendErr := s.send(ctx, &d, []byte(i.payload))
		state := "pending"
		if status == 404 || status == 410 {
			_, e = s.db.Exec("DELETE FROM devices WHERE id=?", i.id)
			if e != nil {
				return e
			}
			state = "expired"
		} else if sendErr == nil && status >= 200 && status < 300 {
			state = "sent"
		} else if i.attempts >= 10 {
			state = "failed"
		}
		if state == "pending" || state == "failed" {
			slog.Warn("phone push delivery retry", "status", status, "attempt", i.attempts+1, "state", state)
		}
		delay := time.Minute * time.Duration(1<<min(i.attempts, 8))
		_, e = s.db.Exec("UPDATE deliveries SET state=?,attempts=attempts+1,next=? WHERE device=? AND event=?", state, time.Now().Add(delay).Unix(), i.id, i.event)
		if e != nil {
			return e
		}
	}
	return nil
}
