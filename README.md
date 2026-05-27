# DWService Clone - Remote Desktop Access

A self-hosted alternative to DWService providing secure remote desktop access through WebRTC.

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Web Frontend                          │
│  ┌─────────────────────────────────────────┐           │
│  │ React + TypeScript + WebRTC API         │           │
│  │ • Device dashboard                      │           │
│  │ • WebRTC viewer + input handler         │           │
│  └─────────────────────────────────────────┘           │
└─────────────────┬───────────────────────────────────────┘
                  │ HTTPS / WSS
                  ▼
┌─────────────────────────────────────────────────────────┐
│                 Signaling Server (Go)                    │
│  ┌─────────────────────────────────────────┐           │
│  │ Go + Pion WebRTC + WebSocket            │           │
│  │ • Agent registration                    │           │
│  │ • ICE/STUN coordination                 │           │
│  │ • Session routing                       │           │
│  │ • PostgreSQL + Redis                    │           │
│  └─────────────────────────────────────────┘           │
└─────────────────┬───────────────────────────────────────┘
                  │ WebSocket / WebRTC
        ┌─────────┴─────────┐
        ▼                   ▼
┌───────────────┐ ┌─────────────────┐
│   Agent       │ │   Browser       │
│  (Rust)       │ │   Client        │
│ • Screen cap  │ │ • Display video │
│ • Input hook  │ │ • Send input    │
│ • Clipboard   │ │                 │
└───────────────┘ └─────────────────┘
```

## 📦 Components

### 1. Signaling Server (Go)
- **Location**: `/signaling`
- **Tech Stack**: Go, Pion WebRTC, Gorilla WebSocket, PostgreSQL, Redis
- **Features**:
  - WebSocket-based signaling for WebRTC
  - Device registration and session management
  - ICE candidate exchange
  - Session cleanup and heartbeat monitoring

### 2. Agent (Rust)
- **Location**: `/agent`
- **Tech Stack**: Rust, scap (screen capture), rdev (input simulation)
- **Features**:
  - Cross-platform screen capture (Windows, macOS, Linux)
  - Mouse and keyboard input simulation
  - Clipboard synchronization
  - WebSocket connection to signaling server

### 3. Web Frontend (React)
- **Location**: `/frontend`
- **Tech Stack**: React, TypeScript, Vite, native WebRTC API
- **Features**:
  - Device dashboard with online/offline status
  - WebRTC video streaming
  - Real-time input handling (mouse, keyboard, clipboard)
  - Connection status monitoring

## 🚀 Quick Start

### Prerequisites
- Go 1.21+
- Rust 1.70+
- Node.js 18+
- PostgreSQL 14+
- Redis 7+

### 1. Setup Database

```bash
# PostgreSQL
createdb dwservice

# Redis (ensure it's running)
redis-server
```

### 2. Build Signaling Server

```bash
cd signaling
go mod download
go build -o signaling ./cmd/signaling

# Generate self-signed certificate for development
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes

# Run server
./signaling -addr :8443 -db "postgres://localhost/dwservice?sslmode=disable" -redis localhost:6379
```

### 3. Build Agent

```bash
cd agent
cargo build --release

# Run agent (replace DEVICE_ID with your device identifier)
DEVICE_ID=my-device-001 SIGNALING_URL=wss://localhost:8443/ws cargo run --release
```

### 4. Start Frontend

```bash
cd frontend
npm install
npm run dev

# Open http://localhost:3000 in browser
```

## 🔐 Security Features

1. **TLS 1.3** for all connections (HTTPS, WSS, DTLS-SRTP)
2. **End-to-end encryption** via WebRTC
3. **Device authentication** with unique IDs
4. **Session management** with automatic cleanup
5. **CORS protection** on signaling server
6. **Input validation** on all endpoints

## 🛠️ Configuration

### Signaling Server Flags

```
-addr       HTTPS listen address (default: :8443)
-cert       TLS certificate file (default: cert.pem)
-key        TLS key file (default: key.pem)
-db         PostgreSQL DSN
-redis      Redis address (default: localhost:6379)
```

### Agent Environment Variables

```
DEVICE_ID         Unique device identifier
SIGNALING_URL     WebSocket URL of signaling server
```

## 📝 Development

### Running Tests

```bash
# Signaling server
cd signaling && go test ./...

# Agent
cd agent && cargo test

# Frontend
cd frontend && npm test
```

### Building for Production

```bash
# Signaling server
cd signaling
go build -ldflags="-s -w" -o signaling ./cmd/signaling

# Agent
cd agent
cargo build --release

# Frontend
cd frontend
npm run build
```

## 📄 License

MIT License - see LICENSE file for details

## 🤝 Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.

## 🙏 Acknowledgments

- [Pion WebRTC](https://github.com/pion/webrtc) - Pure Go WebRTC implementation
- [scap](https://github.com/waydab/scap) - Cross-platform screen capture
- [rdev](https://github.com/NicolasConstant/rdev) - Input simulation library
- [DWService](https://www.dwservice.net/) - Inspiration for this project
