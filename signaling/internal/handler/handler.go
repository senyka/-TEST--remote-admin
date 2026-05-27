package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/yourorg/dwservice-clone/signaling/internal/store"
)

// SignalMessage represents a WebSocket signaling message
type SignalMessage struct {
	Type    string          `json:"type"` // "register", "offer", "answer", "ice", "heartbeat"
	From    string          `json:"from"`
	To      string          `json:"to"`
	Payload json.RawMessage `json:"payload"`
}

// ActiveSession manages WebRTC peer connection and WebSocket connections
type ActiveSession struct {
	mu              sync.RWMutex
	DeviceID        string
	AgentConn       *websocket.Conn
	ClientConn      *websocket.Conn
	PeerConnection  *webrtc.PeerConnection
	DataChannel     *webrtc.DataChannel
	CreatedAt       time.Time
	LastActivity    time.Time
}

// Handler manages WebSocket connections and WebRTC signaling
type Handler struct {
	store      store.Store
	redis      *store.RedisStore
	sessions   sync.Map // map[string]*ActiveSession
	upgrader   websocket.Upgrader
	mediaEngine *webrtc.MediaEngine
	api        *webrtc.API
}

// NewHandler creates a new signaling handler
func NewHandler(dbStore store.Store, redisStore *store.RedisStore) *Handler {
	// Initialize MediaEngine for codec support
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		log.Printf("Warning: Failed to register default codecs: %v", err)
	}

	// Create API with custom settings
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	return &Handler{
		store: dbStore,
		redis: redisStore,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// In production, validate against allowed origins
				origin := r.Header.Get("Origin")
				allowedOrigins := []string{
					"https://localhost:3000",
					"https://dash-panel.tech",
				}
				for _, allowed := range allowedOrigins {
					if origin == allowed {
						return true
					}
				}
				// Allow localhost for development
				if origin == "" || 
				   origin == "http://localhost:3000" || 
				   origin == "http://127.0.0.1:3000" {
					return true
				}
				return false
			},
		},
		mediaEngine: mediaEngine,
		api:        api,
	}
}

// WSHandler handles WebSocket connections for signaling
func (h *Handler) WSHandler(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("id")
	if deviceID == "" {
		http.Error(w, "Missing device ID", http.StatusBadRequest)
		return
	}

	role := r.URL.Query().Get("role")
	if role != "agent" && role != "client" {
		http.Error(w, "Invalid role (must be 'agent' or 'client')", http.StatusBadRequest)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	log.Printf("New %s connection for device %s", role, deviceID)

	if role == "agent" {
		h.handleAgentConnection(deviceID, conn)
	} else {
		h.handleClientConnection(deviceID, conn)
	}
}

// handleAgentConnection processes agent (remote device) connections
func (h *Handler) handleAgentConnection(deviceID string, conn *websocket.Conn) {
	// Check if session already exists
	if val, ok := h.sessions.Load(deviceID); ok {
		session := val.(*ActiveSession)
		session.mu.Lock()
		oldConn := session.AgentConn
		session.AgentConn = conn
		session.LastActivity = time.Now()
		session.mu.Unlock()

		if oldConn != nil {
			oldConn.Close()
		}
		log.Printf("Reconnected agent for device %s", deviceID)
	} else {
		// Create new session for agent
		session := &ActiveSession{
			DeviceID:     deviceID,
			AgentConn:    conn,
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
		h.sessions.Store(deviceID, session)
		log.Printf("New agent registered for device %s", deviceID)
	}

	// Update device status in database
	ctx := context.Background()
	h.store.UpdateDeviceStatus(ctx, deviceID, true)

	// Handle incoming messages
	h.handleAgentMessages(deviceID, conn)
}

// handleClientConnection processes browser client connections
func (h *Handler) handleClientConnection(deviceID string, conn *websocket.Conn) {
	// Find existing agent session
	val, ok := h.sessions.Load(deviceID)
	if !ok {
		conn.WriteJSON(SignalMessage{
			Type: "error",
			Payload: json.RawMessage(`{"message": "Device not found or offline"}`),
		})
		conn.Close()
		log.Printf("Client connected but device %s not found", deviceID)
		return
	}

	session := val.(*ActiveSession)
	session.mu.Lock()
	session.ClientConn = conn
	session.LastActivity = time.Now()
	session.mu.Unlock()

	log.Printf("Client connected to device %s", deviceID)

	// Notify agent that client is waiting
	session.AgentConn.WriteJSON(SignalMessage{
		Type: "client_waiting",
		To:   deviceID,
	})

	// Handle incoming messages from client
	h.handleClientMessages(deviceID, session, conn)
}

// handleAgentMessages processes messages from the agent
func (h *Handler) handleAgentMessages(deviceID string, conn *websocket.Conn) {
	defer func() {
		h.store.UpdateDeviceStatus(context.Background(), deviceID, false)
		conn.Close()
		log.Printf("Agent disconnected for device %s", deviceID)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var signal SignalMessage
		if err := json.Unmarshal(msg, &signal); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		switch signal.Type {
		case "heartbeat":
			// Update last activity
			if val, ok := h.sessions.Load(deviceID); ok {
				session := val.(*ActiveSession)
				session.mu.Lock()
				session.LastActivity = time.Now()
				session.mu.Unlock()
			}

		case "offer":
			// Forward SDP offer to client
			if val, ok := h.sessions.Load(deviceID); ok {
				session := val.(*ActiveSession)
				session.mu.RLock()
				clientConn := session.ClientConn
				session.mu.RUnlock()

				if clientConn != nil {
					clientConn.WriteJSON(signal)
				}
			}

		case "ice":
			// Forward ICE candidate to client
			if val, ok := h.sessions.Load(deviceID); ok {
				session := val.(*ActiveSession)
				session.mu.RLock()
				clientConn := session.ClientConn
				session.mu.RUnlock()

				if clientConn != nil {
					clientConn.WriteJSON(signal)
				}
			}

		case "input_response":
			// Acknowledge input event processed
			log.Printf("Input event processed on device %s", deviceID)
		}
	}

	// Cleanup session
	h.cleanupSession(deviceID)
}

// handleClientMessages processes messages from the browser client
func (h *Handler) handleClientMessages(deviceID string, session *ActiveSession, conn *websocket.Conn) {
	defer func() {
		conn.Close()
		log.Printf("Client disconnected from device %s", deviceID)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var signal SignalMessage
		if err := json.Unmarshal(msg, &signal); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		switch signal.Type {
		case "answer":
			// Forward SDP answer to agent
			session.mu.RLock()
			agentConn := session.AgentConn
			session.mu.RUnlock()

			if agentConn != nil {
				agentConn.WriteJSON(signal)
			}

		case "ice":
			// Forward ICE candidate to agent
			session.mu.RLock()
			agentConn := session.AgentConn
			session.mu.RUnlock()

			if agentConn != nil {
				agentConn.WriteJSON(signal)
			}

		case "input":
			// Forward input event to agent via DataChannel or WebSocket
			session.mu.RLock()
			dataChannel := session.DataChannel
			session.mu.RUnlock()

			if dataChannel != nil && dataChannel.ReadyState() == webrtc.DataChannelStateOpen {
				dataChannel.Send(signal.Payload)
			} else if session.AgentConn != nil {
				// Fallback to WebSocket if DataChannel not ready
				session.AgentConn.WriteJSON(SignalMessage{
					Type:    "input",
					To:      deviceID,
					Payload: signal.Payload,
				})
			}
		}
	}
}

// setupWebRTC initializes WebRTC peer connection for the session
func (h *Handler) setupWebRTC(session *ActiveSession) error {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			// Add TURN servers for production
			// {URLs: []string{"turn:your-turn-server.com:3478"}, Username: "user", Credential: "pass"},
		},
	}

	pc, err := h.api.NewPeerConnection(config)
	if err != nil {
		return err
	}

	session.PeerConnection = pc

	// Handle incoming media tracks (screen share, audio)
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Received track: %s %s", track.Kind().String(), track.ID())
		// Track is automatically sent to the other peer in P2P mode
	})

	// Handle DataChannel for input events
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Printf("Data channel opened: %s", dc.Label())
		session.DataChannel = dc

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			// Forward input from client to agent
			session.mu.RLock()
			agentConn := session.AgentConn
			session.mu.RUnlock()

			if agentConn != nil {
				agentConn.WriteMessage(websocket.TextMessage, msg.Data)
			}
		})
	})

	// Handle ICE connection state changes
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("ICE connection state changed: %s", state.String())
		if state == webrtc.ICEConnectionStateFailed || 
		   state == webrtc.ICEConnectionStateClosed || 
		   state == webrtc.ICEConnectionStateDisconnected {
			h.cleanupSession(session.DeviceID)
		}
	})

	// Handle connection close
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("Peer connection state: %s", state.String())
		if state == webrtc.PeerConnectionStateClosed || 
		   state == webrtc.PeerConnectionStateFailed {
			h.cleanupSession(session.DeviceID)
		}
	})

	return nil
}

// cleanupSession removes and closes all resources for a session
func (h *Handler) cleanupSession(deviceID string) {
	if val, ok := h.sessions.Load(deviceID); ok {
		session := val.(*ActiveSession)
		session.mu.Lock()
		defer session.mu.Unlock()

		if session.AgentConn != nil {
			session.AgentConn.Close()
		}
		if session.ClientConn != nil {
			session.ClientConn.Close()
		}
		if session.PeerConnection != nil {
			session.PeerConnection.Close()
		}

		h.sessions.Delete(deviceID)
		log.Printf("Session cleaned up for device %s", deviceID)
	}
}

// StartSessionCleanup starts a background goroutine to clean up inactive sessions
func (h *Handler) StartSessionCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sessions.Range(func(key, value interface{}) bool {
				session := value.(*ActiveSession)
				session.mu.RLock()
				lastActivity := session.LastActivity
				session.mu.RUnlock()

				if time.Since(lastActivity) > 5*time.Minute {
					log.Printf("Cleaning up inactive session for device %s", key.(string))
					h.cleanupSession(key.(string))
				}
				return true
			})
		}
	}
}
