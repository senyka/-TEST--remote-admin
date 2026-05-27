package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Device represents a registered remote device
type Device struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	UserID       string    `json:"user_id"`
	LastSeen     time.Time `json:"last_seen"`
	IsOnline     bool      `json:"is_online"`
	Platform     string    `json:"platform"` // windows, linux, macos
	AgentVersion string    `json:"agent_version"`
}

// Session represents an active WebRTC session
type Session struct {
	ID              string    `json:"id"`
	DeviceID        string    `json:"device_id"`
	UserID          string    `json:"user_id"`
	PeerConnection  string    `json:"peer_connection"` // serialized SDP
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	RecordingEnabled bool     `json:"recording_enabled"`
}

// Store interface for data persistence
type Store interface {
	// Device operations
	SaveDevice(ctx context.Context, device *Device) error
	GetDevice(ctx context.Context, id string) (*Device, error)
	ListDevices(ctx context.Context, userID string) ([]*Device, error)
	UpdateDeviceStatus(ctx context.Context, id string, online bool) error

	// Session operations
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
}

// PostgresStore implements Store using PostgreSQL
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize schema
	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		user_id TEXT NOT NULL,
		last_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		is_online BOOLEAN DEFAULT FALSE,
		platform TEXT,
		agent_version TEXT
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		device_id TEXT REFERENCES devices(id),
		user_id TEXT NOT NULL,
		peer_connection TEXT,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		expires_at TIMESTAMP WITH TIME ZONE,
		recording_enabled BOOLEAN DEFAULT FALSE
	);

	CREATE INDEX IF NOT EXISTS idx_devices_user_id ON devices(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_device_id ON sessions(device_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	`

	_, err := db.Exec(schema)
	return err
}

func (s *PostgresStore) SaveDevice(ctx context.Context, device *Device) error {
	query := `
		INSERT INTO devices (id, name, user_id, last_seen, is_online, platform, agent_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			last_seen = EXCLUDED.last_seen,
			is_online = EXCLUDED.is_online,
			platform = EXCLUDED.platform,
			agent_version = EXCLUDED.agent_version
	`
	_, err := s.db.ExecContext(ctx, query,
		device.ID,
		device.Name,
		device.UserID,
		device.LastSeen,
		device.IsOnline,
		device.Platform,
		device.AgentVersion,
	)
	return err
}

func (s *PostgresStore) GetDevice(ctx context.Context, id string) (*Device, error) {
	query := `SELECT id, name, user_id, last_seen, is_online, platform, agent_version 
			  FROM devices WHERE id = $1`

	row := s.db.QueryRowContext(ctx, query, id)
	device := &Device{}
	err := row.Scan(
		&device.ID,
		&device.Name,
		&device.UserID,
		&device.LastSeen,
		&device.IsOnline,
		&device.Platform,
		&device.AgentVersion,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return device, err
}

func (s *PostgresStore) ListDevices(ctx context.Context, userID string) ([]*Device, error) {
	query := `SELECT id, name, user_id, last_seen, is_online, platform, agent_version 
			  FROM devices WHERE user_id = $1 ORDER BY last_seen DESC`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d := &Device{}
		if err := rows.Scan(
			&d.ID,
			&d.Name,
			&d.UserID,
			&d.LastSeen,
			&d.IsOnline,
			&d.Platform,
			&d.AgentVersion,
		); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *PostgresStore) UpdateDeviceStatus(ctx context.Context, id string, online bool) error {
	query := `UPDATE devices SET is_online = $1, last_seen = NOW() WHERE id = $2`
	_, err := s.db.ExecContext(ctx, query, online, id)
	return err
}

func (s *PostgresStore) CreateSession(ctx context.Context, session *Session) error {
	query := `INSERT INTO sessions (id, device_id, user_id, peer_connection, created_at, expires_at, recording_enabled)
			  VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := s.db.ExecContext(ctx, query,
		session.ID,
		session.DeviceID,
		session.UserID,
		session.PeerConnection,
		session.CreatedAt,
		session.ExpiresAt,
		session.RecordingEnabled,
	)
	return err
}

func (s *PostgresStore) GetSession(ctx context.Context, id string) (*Session, error) {
	query := `SELECT id, device_id, user_id, peer_connection, created_at, expires_at, recording_enabled 
			  FROM sessions WHERE id = $1`

	row := s.db.QueryRowContext(ctx, query, id)
	session := &Session{}
	err := row.Scan(
		&session.ID,
		&session.DeviceID,
		&session.UserID,
		&session.PeerConnection,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.RecordingEnabled,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return session, err
}

func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = $1`
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

// RedisStore provides caching and pub/sub functionality
type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr string) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}

// Cache device session mapping
func (r *RedisStore) SetDeviceConnection(ctx context.Context, deviceID, connID string) error {
	key := fmt.Sprintf("device:%s:connection", deviceID)
	return r.client.Set(ctx, key, connID, 5*time.Minute).Err()
}

func (r *RedisStore) GetDeviceConnection(ctx context.Context, deviceID string) (string, error) {
	key := fmt.Sprintf("device:%s:connection", deviceID)
	return r.client.Get(ctx, key).Result()
}

// Pub/Sub for signaling messages
func (r *RedisStore) PublishSignal(ctx context.Context, channel string, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, channel, data).Err()
}

func (r *RedisStore) SubscribeSignals(ctx context.Context, channels ...string) *redis.PubSub {
	return r.client.Subscribe(ctx, channels...)
}

// Generate unique session ID
func GenerateSessionID() string {
	return uuid.New().String()
}
