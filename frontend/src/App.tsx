import { useState, useEffect } from 'react';
import { RemoteDesktop } from './components/RemoteDesktop';

// Mock device list - in production, fetch from API
const MOCK_DEVICES = [
  { id: 'device-001', name: 'Office PC', platform: 'windows', online: true },
  { id: 'device-002', name: 'Home Laptop', platform: 'macos', online: false },
  { id: 'device-003', name: 'Linux Server', platform: 'linux', online: true },
];

function App() {
  const [selectedDevice, setSelectedDevice] = useState<string | null>(null);
  const [devices, setDevices] = useState(MOCK_DEVICES);

  // In production, fetch devices from backend API
  useEffect(() => {
    // Example: fetch('/api/devices').then(res => res.json()).then(setDevices);
    console.log('App mounted, devices:', devices);
  }, []);

  return (
    <div className="app">
      <header className="app-header">
        <h1>🖥️ DWService Clone</h1>
        <p>Remote Desktop Access</p>
      </header>

      <main className="app-main">
        {!selectedDevice ? (
          <div className="device-list">
            <h2>Your Devices</h2>
            <div className="device-grid">
              {devices.map((device) => (
                <div
                  key={device.id}
                  className={`device-card ${device.online ? 'online' : 'offline'}`}
                  onClick={() => device.online && setSelectedDevice(device.id)}
                  style={{
                    cursor: device.online ? 'pointer' : 'not-allowed',
                    opacity: device.online ? 1 : 0.6,
                  }}
                >
                  <div className="device-icon">
                    {device.platform === 'windows' && '🪟'}
                    {device.platform === 'macos' && '🍎'}
                    {device.platform === 'linux' && '🐧'}
                  </div>
                  <h3>{device.name}</h3>
                  <p className="device-id">{device.id}</p>
                  <span className={`status-badge ${device.online ? 'online' : 'offline'}`}>
                    {device.online ? '● Online' : '○ Offline'}
                  </span>
                </div>
              ))}
            </div>

            <div className="add-device-section">
              <h3>Add New Device</h3>
              <p>Download and run the agent on your remote device:</p>
              <code>curl -sSL https://dash-panel.tech/install.sh | sh</code>
            </div>
          </div>
        ) : (
          <div className="remote-session">
            <button 
              className="back-button"
              onClick={() => setSelectedDevice(null)}
            >
              ← Back to Devices
            </button>
            <RemoteDesktop deviceId={selectedDevice} />
          </div>
        )}
      </main>

      <footer className="app-footer">
        <p>DWService Clone © 2024 | Secure Remote Desktop Access</p>
      </footer>
    </div>
  );
}

export default App;
