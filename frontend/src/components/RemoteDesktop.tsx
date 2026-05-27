import { useEffect, useRef, useState } from 'react';

interface RemoteDesktopProps {
  deviceId: string;
}

interface InputEvent {
  type: string;
  [key: string]: any;
}

export function RemoteDesktop({ deviceId }: RemoteDesktopProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const dataChannelRef = useRef<RTCDataChannel | null>(null);
  const [connectionStatus, setConnectionStatus] = useState('connecting');
  const [fps, setFps] = useState(0);

  useEffect(() => {
    const signalingUrl = `wss://localhost:8443/ws?id=${deviceId}&role=client`;
    
    // Initialize WebSocket connection to signaling server
    const ws = new WebSocket(signalingUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      console.log('Connected to signaling server');
      setConnectionStatus('signaling');
      initializeWebRTC();
    };

    ws.onmessage = (event) => {
      const message = JSON.parse(event.data);
      handleSignalingMessage(message);
    };

    ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      setConnectionStatus('error');
    };

    ws.onclose = () => {
      console.log('WebSocket closed');
      setConnectionStatus('disconnected');
      cleanup();
    };

    // Initialize WebRTC peer connection
    function initializeWebRTC() {
      const config: RTCConfiguration = {
        iceServers: [
          { urls: 'stun:stun.l.google.com:19302' },
          // Add TURN servers for production
          // { urls: 'turn:your-turn-server.com:3478', username: 'user', credential: 'pass' }
        ],
      };

      const pc = new RTCPeerConnection(config);
      pcRef.current = pc;

      // Handle incoming video/audio tracks
      pc.ontrack = (event) => {
        console.log('Received track:', event.track.kind);
        if (videoRef.current && event.streams[0]) {
          videoRef.current.srcObject = event.streams[0];
          setConnectionStatus('connected');
          
          // Start FPS monitoring
          monitorFPS();
        }
      };

      // Handle ICE candidates
      pc.onicecandidate = (event) => {
        if (event.candidate && ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({
            type: 'ice',
            payload: event.candidate,
          }));
        }
      };

      // Handle connection state changes
      pc.onconnectionstatechange = () => {
        console.log('Connection state:', pc.connectionState);
        if (pc.connectionState === 'failed') {
          setConnectionStatus('error');
        } else if (pc.connectionState === 'disconnected') {
          setConnectionStatus('disconnected');
        }
      };

      // Create DataChannel for input events
      const dc = pc.createDataChannel('input', {
        ordered: false,
        maxRetransmits: 0,
      });
      
      dc.onopen = () => {
        console.log('Data channel opened');
        dataChannelRef.current = dc;
      };

      dc.onclose = () => {
        console.log('Data channel closed');
        dataChannelRef.current = null;
      };

      // Create and send offer
      pc.createOffer()
        .then((offer) => pc.setLocalDescription(offer))
        .then(() => {
          if (ws.readyState === WebSocket.OPEN && pc.localDescription) {
            ws.send(JSON.stringify({
              type: 'offer',
              payload: pc.localDescription,
            }));
          }
        })
        .catch((err) => console.error('Failed to create offer:', err));
    }

    // Handle incoming signaling messages
    function handleSignalingMessage(message: any) {
      const pc = pcRef.current;
      if (!pc) return;

      switch (message.type) {
        case 'answer':
          if (message.payload) {
            pc.setRemoteDescription(new RTCSessionDescription(message.payload))
              .catch((err) => console.error('Failed to set remote description:', err));
          }
          break;

        case 'ice':
          if (message.payload) {
            pc.addIceCandidate(new RTCIceCandidate(message.payload))
              .catch((err) => console.error('Failed to add ICE candidate:', err));
          }
          break;

        case 'client_waiting':
          console.log('Waiting for agent to respond...');
          break;

        case 'error':
          console.error('Server error:', message.payload);
          setConnectionStatus('error');
          break;
      }
    }

    // Monitor FPS
    function monitorFPS() {
      let frameCount = 0;
      let lastTime = performance.now();

      function countFrames() {
        frameCount++;
        const now = performance.now();
        if (now - lastTime >= 1000) {
          setFps(frameCount);
          frameCount = 0;
          lastTime = now;
        }
        requestAnimationFrame(countFrames);
      }

      countFrames();
    }

    // Cleanup on unmount
    return () => {
      cleanup();
    };

    function cleanup() {
      if (dataChannelRef.current) {
        dataChannelRef.current.close();
        dataChannelRef.current = null;
      }
      if (pcRef.current) {
        pcRef.current.close();
        pcRef.current = null;
      }
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    }
  }, [deviceId]);

  // Send input events to remote device
  const sendInput = (event: InputEvent) => {
    const dc = dataChannelRef.current;
    if (dc && dc.readyState === 'open') {
      dc.send(JSON.stringify(event));
    } else if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'input',
        payload: event,
      }));
    }
  };

  // Mouse event handlers
  const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!canvasRef.current) return;
    
    const rect = canvasRef.current.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;

    sendInput({
      type: 'mousemove',
      x: Math.max(0, Math.min(1, x)),
      y: Math.max(0, Math.min(1, y)),
    });
  };

  const handleMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!canvasRef.current) return;
    
    const rect = canvasRef.current.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;
    const button = e.button;

    sendInput({
      type: 'mousedown',
      button,
      x: Math.max(0, Math.min(1, x)),
      y: Math.max(0, Math.min(1, y)),
    });
  };

  const handleMouseUp = (e: React.MouseEvent<HTMLDivElement>) => {
    sendInput({
      type: 'mouseup',
      button: e.button,
    });
  };

  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    sendInput({
      type: 'wheel',
      delta_x: e.deltaX,
      delta_y: e.deltaY,
    });
  };

  // Keyboard event handlers
  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    sendInput({
      type: 'keydown',
      code: e.code,
    });
  };

  const handleKeyUp = (e: React.KeyboardEvent<HTMLDivElement>) => {
    sendInput({
      type: 'keyup',
      code: e.code,
    });
  };

  return (
    <div className="remote-desktop-container">
      <div className="connection-status">
        <span className={`status-indicator ${connectionStatus}`}>
          {connectionStatus === 'connected' ? '●' : '○'}
        </span>
        <span>{connectionStatus.toUpperCase()}</span>
        {fps > 0 && <span className="fps-counter">{fps} FPS</span>}
      </div>

      <div
        className="remote-desktop-canvas"
        ref={canvasRef}
        tabIndex={0}
        onMouseMove={handleMouseMove}
        onMouseDown={handleMouseDown}
        onMouseUp={handleMouseUp}
        onWheel={handleWheel}
        onKeyDown={handleKeyDown}
        onKeyUp={handleKeyUp}
      >
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted
          className="remote-desktop-video"
        />
        
        {connectionStatus !== 'connected' && (
          <div className="connection-overlay">
            {connectionStatus === 'connecting' && <p>Connecting...</p>}
            {connectionStatus === 'signaling' && <p>Establishing connection...</p>}
            {connectionStatus === 'error' && <p>Connection failed. Please try again.</p>}
            {connectionStatus === 'disconnected' && <p>Disconnected</p>}
          </div>
        )}
      </div>

      <div className="toolbar">
        <button title="Fullscreen">⛶</button>
        <button title="Clipboard">📋</button>
        <button title="File Transfer">📁</button>
        <button title="Settings">⚙️</button>
      </div>
    </div>
  );
}
