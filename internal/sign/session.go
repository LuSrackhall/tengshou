// Package sign implements the ephemeral signature-capture sessions used by the
// LAN QR-code signing page, plus normalization of the handwritten PNG.
//
// Sessions live only in memory: the PC creates one, shows a QR code with a
// one-time token, and the phone submits against that token. Tokens are never
// written to disk, expire quickly, and can be consumed exactly once. Losing
// sessions on restart is acceptable because the PC can always create a new one.
package sign

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session states.
const (
	StatePending    = "pending"
	StateSubmitting = "submitting"
	StateCompleted  = "completed"
	StateExpired    = "expired"
)

// Session kinds. Signature sessions capture a signature for a contract or a
// party archive; enroll sessions let a tenant fill a complete archive on a
// phone over the LAN.
const (
	KindSignature = "signature"
	KindEnroll    = "enroll"
)

var (
	// ErrNotFound means the token or session id is unknown (never issued,
	// cancelled, or already purged).
	ErrNotFound = errors.New("sign: session not found")
	// ErrExpired means the session passed its expiry time.
	ErrExpired = errors.New("sign: session expired")
	// ErrConsumed means the session already produced a signature.
	ErrConsumed = errors.New("sign: session already submitted")
	// ErrBusy means another submission for the same session is in flight.
	ErrBusy = errors.New("sign: session submission in progress")
)

// Session is one pending or completed signature capture. It carries no token:
// the token is returned once by Create and only its holder (the QR code) can
// submit.
type Session struct {
	ID              string
	Kind            string
	ContractID      string
	ContractNo      string
	ContractTitle   string
	ContentSHA256   string
	PartyID         string
	SignerName      string
	URLs            []string
	State           string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	CompletedAt     time.Time
	SignatureID     string
	SignaturePath   string
	SignatureWidth  int
	SignatureHeight int
}

// CreateInput describes a new capture session.
type CreateInput struct {
	// Kind selects the phone page: signature (default) or enroll.
	Kind          string
	ContractID    string
	ContractNo    string
	ContractTitle string
	ContentSHA256 string
	PartyID       string
	SignerName    string
	// URLPrefixes are signing-page bases such as "http://192.168.1.5:8765/sign/".
	// Create appends the one-time token to each.
	URLPrefixes []string
}

// Manager keeps sessions in memory. It is safe for concurrent use.
type Manager struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	byToken map[string]*Session
	byID    map[string]*Session
}

// NewManager creates a manager whose sessions expire after ttl. A nil clock
// uses time.Now (tests inject a fake clock).
func NewManager(ttl time.Duration, now func() time.Time) *Manager {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &Manager{
		ttl:     ttl,
		now:     now,
		byToken: map[string]*Session{},
		byID:    map[string]*Session{},
	}
}

// TTL returns the session lifetime.
func (m *Manager) TTL() time.Duration { return m.ttl }

// Create registers a new session and returns it together with its one-time
// token. The token is 256 bits of randomness rendered as hex.
func (m *Manager) Create(in CreateInput) (Session, string, error) {
	token, err := randomHex(32)
	if err != nil {
		return Session{}, "", err
	}
	id, err := modelID("ssn")
	if err != nil {
		return Session{}, "", err
	}
	now := m.now().UTC().Truncate(time.Second)
	urls := make([]string, 0, len(in.URLPrefixes))
	for _, prefix := range in.URLPrefixes {
		urls = append(urls, strings.TrimRight(prefix, "/")+"/"+token)
	}
	s := &Session{
		ID:            id,
		Kind:          kindOrDefault(in.Kind),
		ContractID:    in.ContractID,
		ContractNo:    in.ContractNo,
		ContractTitle: in.ContractTitle,
		ContentSHA256: in.ContentSHA256,
		PartyID:       in.PartyID,
		SignerName:    in.SignerName,
		URLs:          urls,
		State:         StatePending,
		CreatedAt:     now,
		ExpiresAt:     now.Add(m.ttl),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked(now)
	m.byToken[token] = s
	m.byID[id] = s
	return *s, token, nil
}

// Get returns a copy of the session for token. Expired sessions are returned
// with StateExpired instead of an error so the phone page can explain what
// happened.
func (m *Manager) Get(token string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byToken[token]
	if !ok {
		return Session{}, ErrNotFound
	}
	copy := *s
	if copy.State != StateCompleted && !m.now().Before(copy.ExpiresAt) {
		copy.State = StateExpired
	}
	return copy, nil
}

// GetByID returns a copy of the session for id.
func (m *Manager) GetByID(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	copy := *s
	if copy.State != StateCompleted && !m.now().Before(copy.ExpiresAt) {
		copy.State = StateExpired
	}
	return copy, nil
}

// Claim atomically moves a pending session to submitting. Only one caller can
// hold a session at a time, which makes submission one-shot even when the
// phone retries over a flaky connection.
func (m *Manager) Claim(token string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byToken[token]
	if !ok {
		return Session{}, ErrNotFound
	}
	switch s.State {
	case StateCompleted:
		return *s, ErrConsumed
	case StateSubmitting:
		return *s, ErrBusy
	}
	if !m.now().Before(s.ExpiresAt) {
		return *s, ErrExpired
	}
	s.State = StateSubmitting
	return *s, nil
}

// Release returns a submitting session to pending after a recoverable failure
// (too-light handwriting, disk error). It is a no-op for other states.
func (m *Manager) Release(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.byToken[token]; ok && s.State == StateSubmitting {
		s.State = StatePending
	}
}

// Complete marks a session as submitted and records the stored signature.
func (m *Manager) Complete(token, signatureID, assetPath string, width, height int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.byToken[token]; ok && s.State == StateSubmitting {
		s.State = StateCompleted
		s.CompletedAt = m.now().UTC().Truncate(time.Second)
		s.SignatureID = signatureID
		s.SignaturePath = assetPath
		s.SignatureWidth = width
		s.SignatureHeight = height
	}
}

// Cancel forgets a session so its token stops working immediately. Completed
// sessions are kept (they are already consumed) but removed from the id index
// only after expiry.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok || s.State == StateCompleted {
		return false
	}
	for token, candidate := range m.byToken {
		if candidate == s {
			delete(m.byToken, token)
			break
		}
	}
	delete(m.byID, id)
	return true
}

// purgeLocked drops sessions that expired more than one TTL ago. Completed
// sessions are retained for the same grace period so a late status poll still
// sees the result.
func (m *Manager) purgeLocked(now time.Time) {
	for token, s := range m.byToken {
		if now.After(s.ExpiresAt.Add(m.ttl)) {
			delete(m.byToken, token)
			delete(m.byID, s.ID)
		}
	}
}

func kindOrDefault(kind string) string {
	switch strings.TrimSpace(kind) {
	case KindEnroll:
		return KindEnroll
	default:
		return KindSignature
	}
}

func randomHex(n int) (string, error) {
	var b [64]byte
	if n > len(b) {
		return "", fmt.Errorf("sign: random length %d too large", n)
	}
	if _, err := rand.Read(b[:n]); err != nil {
		return "", fmt.Errorf("sign: generate token: %w", err)
	}
	return hex.EncodeToString(b[:n]), nil
}

func modelID(prefix string) (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("sign: generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

// ValidToken reports whether token has the shape issued by Create.
func ValidToken(token string) bool {
	if len(token) != 64 || strings.TrimSpace(token) != token {
		return false
	}
	for _, r := range token {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
