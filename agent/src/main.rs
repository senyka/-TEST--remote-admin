use anyhow::{Context, Result};
use arboard::Clipboard;
use log::{debug, error, info, warn};
use rdev::{simulate, EventType, Key};
use scap::{
    capturer::{get_output_frame_size, Area, Capturer, Options, Point, Size},
    frame::{Frame, VideoFormat},
    Target,
};
use serde::{Deserialize, Serialize};
use std::env;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::mpsc;
use tokio_tungstenite::{connect_async, tungstenite::Message};
use url::Url;

/// Frame data structure for screen capture
#[derive(Debug, Clone, Serialize)]
pub struct ScreenFrame {
    pub timestamp: u64,
    pub width: u32,
    pub height: u32,
    pub format: String,
    pub data: Vec<u8>,
}

/// Input event types received from client
#[derive(Debug, Deserialize)]
#[serde(tag = "type")]
pub enum InputEvent {
    #[serde(rename = "mousemove")]
    MouseMove { x: f32, y: f32 },
    
    #[serde(rename = "mousedown")]
    MouseDown { button: u8, x: f32, y: f32 },
    
    #[serde(rename = "mouseup")]
    MouseUp { button: u8, x: f32, y: f32 },
    
    #[serde(rename = "keydown")]
    KeyDown { code: String },
    
    #[serde(rename = "keyup")]
    KeyUp { code: String },
    
    #[serde(rename = "wheel")]
    Wheel { delta_x: f32, delta_y: f32 },
    
    #[serde(rename = "clipboard_paste")]
    ClipboardPaste { text: String },
    
    #[serde(rename = "clipboard_copy")]
    ClipboardCopy,
}

/// Agent configuration
#[derive(Debug, Clone)]
pub struct AgentConfig {
    pub device_id: String,
    pub signaling_url: String,
    pub fps: u32,
    pub quality: u8,
    pub enable_audio: bool,
    pub enable_clipboard: bool,
}

impl Default for AgentConfig {
    fn default() -> Self {
        Self {
            device_id: env::var("DEVICE_ID").unwrap_or_else(|_| uuid::Uuid::new_v4().to_string()),
            signaling_url: env::var("SIGNALING_URL")
                .unwrap_or_else(|_| "wss://localhost:8443/ws".to_string()),
            fps: 30,
            quality: 80,
            enable_audio: false, // Audio capture requires additional permissions
            enable_clipboard: true,
        }
    }
}

/// Main agent struct
pub struct Agent {
    config: AgentConfig,
    running: Arc<AtomicBool>,
    clipboard_enabled: bool,
}

impl Agent {
    /// Create a new agent with the given configuration
    pub fn new(config: AgentConfig) -> Self {
        let clipboard_enabled = config.enable_clipboard;
        Self {
            config,
            running: Arc::new(AtomicBool::new(false)),
            clipboard_enabled,
        }
    }

    /// Start the agent
    pub async fn run(&mut self) -> Result<()> {
        env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info"))
            .init();

        info!("Starting DWService Agent v{}", env!("CARGO_PKG_VERSION"));
        info!("Device ID: {}", self.config.device_id);
        info!("Signaling URL: {}", self.config.signaling_url);

        self.running.store(true, Ordering::SeqCst);

        // Build WebSocket URL
        let ws_url = format!(
            "{}?id={}&role=agent",
            self.config.signaling_url, self.config.device_id
        );

        info!("Connecting to signaling server...");
        
        // Connect to WebSocket
        let url = Url::parse(&ws_url).context("Failed to parse WebSocket URL")?;
        let (ws_stream, _) = connect_async(url).await.context("WebSocket connection failed")?;
        
        info!("Connected to signaling server");

        let (mut write, mut read) = ws_stream.split();

        // Channel for input events
        let (input_tx, mut input_rx) = mpsc::channel::<InputEvent>(100);

        // Spawn screen capture task
        let capture_running = self.running.clone();
        let fps = self.config.fps;
        let quality = self.config.quality;
        let tx_clone = input_tx.clone();
        
        let capture_handle = tokio::spawn(async move {
            if let Err(e) = run_screen_capturer(capture_running, fps, quality, tx_clone).await {
                error!("Screen capturer error: {}", e);
            }
        });

        // Spawn input handler task
        let input_running = self.running.clone();
        let input_handle = tokio::spawn(async move {
            while input_running.load(Ordering::SeqCst) {
                tokio::select! {
                    Some(event) = input_rx.recv() => {
                        if let Err(e) = handle_input_event(event) {
                            warn!("Failed to handle input event: {}", e);
                        }
                    }
                    _ = tokio::time::sleep(Duration::from_millis(10)) => {}
                }
            }
        });

        // Main message loop - receive signaling messages
        let msg_running = self.running.clone();
        while msg_running.load(Ordering::SeqCst) {
            tokio::select! {
                msg = read.next() => {
                    match msg {
                        Some(Ok(Message::Text(text))) => {
                            if let Err(e) = self.handle_signaling_message(&text, &input_tx).await {
                                warn!("Failed to handle signaling message: {}", e);
                            }
                        }
                        Some(Ok(Message::Binary(data))) => {
                            debug!("Received binary message: {} bytes", data.len());
                            // Could be used for file transfer or other binary data
                        }
                        Some(Ok(Message::Ping(data))) => {
                            debug!("Received ping");
                            if let Err(e) = write.send(Message::Pong(data)).await {
                                error!("Failed to send pong: {}", e);
                                break;
                            }
                        }
                        Some(Ok(Message::Close(_))) => {
                            info!("Received close frame");
                            break;
                        }
                        Some(Err(e)) => {
                            error!("WebSocket error: {}", e);
                            break;
                        }
                        _ => {}
                    }
                }
                _ = tokio::time::sleep(Duration::from_secs(30)) => {
                    // Send heartbeat
                    let heartbeat = serde_json::json!({
                        "type": "heartbeat",
                        "timestamp": Instant::now().elapsed().as_millis()
                    });
                    if let Err(e) = write.send(Message::Text(heartbeat.to_string())).await {
                        error!("Failed to send heartbeat: {}", e);
                        break;
                    }
                }
            }
        }

        // Cleanup
        info!("Shutting down agent...");
        self.running.store(false, Ordering::SeqCst);
        
        let _ = tokio::join!(capture_handle, input_handle);
        
        Ok(())
    }

    /// Handle incoming signaling message
    async fn handle_signaling_message(
        &self,
        text: &str,
        input_tx: &mpsc::Sender<InputEvent>,
    ) -> Result<()> {
        let msg: serde_json::Value = serde_json::from_str(text)?;

        match msg["type"].as_str() {
            Some("input") => {
                if let Ok(event) = serde_json::from_value::<InputEvent>(msg["payload"].clone()) {
                    input_tx.send(event).await?;
                } else {
                    warn!("Invalid input event format");
                }
            }
            Some("clipboard_request") => {
                if self.clipboard_enabled {
                    if let Ok(text) = Clipboard::new()?.get_text() {
                        let response = serde_json::json!({
                            "type": "clipboard_response",
                            "text": text
                        });
                        // Send response back through WebSocket
                        // This would need access to the write stream
                        debug!("Clipboard content retrieved");
                    }
                }
            }
            Some("client_waiting") => {
                info!("Client is waiting for connection");
            }
            _ => {
                debug!("Unknown message type: {:?}", msg["type"]);
            }
        }

        Ok(())
    }

    /// Stop the agent
    pub fn stop(&self) {
        self.running.store(false, Ordering::SeqCst);
        info!("Stop signal sent");
    }
}

/// Run screen capture loop
async fn run_screen_capturer(
    running: Arc<AtomicBool>,
    fps: u32,
    quality: u8,
    _tx: mpsc::Sender<InputEvent>,
) -> Result<()> {
    // Get available targets (monitors/windows)
    let targets = scap::get_all_targets();
    if targets.is_empty() {
        return Err(anyhow::anyhow!("No capture targets available"));
    }

    // Use first monitor (typically primary display)
    let target = targets
        .iter()
        .find(|t| matches!(t, Target::Display(_)))
        .or(targets.first())
        .ok_or_else(|| anyhow::anyhow!("No valid capture target found"))?;

    info!("Capturing target: {:?}", target);

    // Configure capturer
    let options = Options {
        fps,
        show_cursor: true,
        show_highlight: true,
        output_type: scap::capturer::OutputType::Rgba,
        ..Default::default()
    };

    let mut capturer = Capturer::new(*target, options);
    capturer.start_capture();

    let frame_interval = Duration::from_millis(1000 / fps as u64);
    let mut frame_count = 0u64;
    let start_time = Instant::now();

    info!("Screen capture started at {} FPS", fps);

    while running.load(Ordering::SeqCst) {
        let frame_start = Instant::now();

        match capturer.get_next_frame() {
            Ok(frame) => {
                frame_count += 1;
                
                // Create frame data structure
                let screen_frame = ScreenFrame {
                    timestamp: frame_start.elapsed().as_millis() as u64,
                    width: frame.width as u32,
                    height: frame.height as u32,
                    format: "RGBA".to_string(),
                    data: frame.data,
                };

                // In production, encode and send frame via WebRTC
                // For now, just log statistics
                if frame_count % 100 == 0 {
                    let elapsed = start_time.elapsed();
                    let actual_fps = frame_count as f64 / elapsed.as_secs_f64();
                    debug!("Captured {} frames in {:.2}s ({:.1} FPS)", 
                           frame_count, elapsed.as_secs_f64(), actual_fps);
                }
            }
            Err(e) => {
                warn!("Failed to capture frame: {}", e);
                tokio::time::sleep(Duration::from_millis(100)).await;
            }
        }

        // Maintain target FPS
        let elapsed = frame_start.elapsed();
        if elapsed < frame_interval {
            tokio::time::sleep(frame_interval - elapsed).await;
        }
    }

    capturer.stop_capture();
    info!("Screen capture stopped. Total frames: {}", frame_count);

    Ok(())
}

/// Handle input event by simulating keyboard/mouse actions
fn handle_input_event(event: InputEvent) -> Result<()> {
    match event {
        InputEvent::MouseMove { x, y } => {
            // Convert normalized coordinates (0.0-1.0) to screen coordinates
            let (screen_width, screen_height) = get_screen_size()?;
            let px = (x * screen_width as f32) as i64;
            let py = (y * screen_height as f32) as i64;
            
            simulate(&EventType::MouseMove { x: px, y: py })
                .context("Failed to simulate mouse move")?;
            debug!("Mouse moved to ({}, {})", px, py);
        }

        InputEvent::MouseDown { button, x, y } => {
            // Move to position first
            let (screen_width, screen_height) = get_screen_size()?;
            let px = (x * screen_width as f32) as i64;
            let py = (y * screen_height as f32) as i64;
            simulate(&EventType::MouseMove { x: px, y: py }).ok();

            // Press button
            let key = match button {
                0 => Key::ButtonLeft,
                1 => Key::ButtonMiddle,
                2 => Key::ButtonRight,
                _ => Key::ButtonLeft, // Default to left button
            };
            simulate(&EventType::ButtonPress(key))
                .context("Failed to simulate mouse down")?;
            debug!("Mouse button {} pressed", button);
        }

        InputEvent::MouseUp { button, .. } => {
            let key = match button {
                0 => Key::ButtonLeft,
                1 => Key::ButtonMiddle,
                2 => Key::ButtonRight,
                _ => Key::ButtonLeft,
            };
            simulate(&EventType::ButtonRelease(key))
                .context("Failed to simulate mouse up")?;
            debug!("Mouse button {} released", button);
        }

        InputEvent::KeyDown { code } => {
            if let Some(key) = map_web_key_code(&code) {
                simulate(&EventType::KeyPress(key))
                    .context("Failed to simulate key down")?;
                debug!("Key pressed: {:?}", key);
            } else {
                debug!("Unmapped key code: {}", code);
            }
        }

        InputEvent::KeyUp { code } => {
            if let Some(key) = map_web_key_code(&code) {
                simulate(&EventType::KeyRelease(key))
                    .context("Failed to simulate key up")?;
                debug!("Key released: {:?}", key);
            }
        }

        InputEvent::Wheel { delta_x: _, delta_y } => {
            // Scroll simulation - note: rdev has limited scroll support
            // This is a simplified implementation
            let scroll_amount = (delta_y * 100.0) as i64;
            // Platform-specific scroll implementation would go here
            debug!("Wheel scrolled: {}", scroll_amount);
        }

        InputEvent::ClipboardPaste { text } => {
            if let Ok(mut clipboard) = Clipboard::new() {
                clipboard.set_text(text).context("Failed to set clipboard")?;
                debug!("Clipboard content pasted");
                
                // Simulate Ctrl+V to paste
                #[cfg(target_os = "windows")]
                {
                    simulate(&EventType::KeyPress(Key::ControlLeft)).ok();
                    simulate(&EventType::KeyPress(Key::KeyV)).ok();
                    simulate(&EventType::KeyRelease(Key::KeyV)).ok();
                    simulate(&EventType::KeyRelease(Key::ControlLeft)).ok();
                }
                
                #[cfg(not(target_os = "windows"))]
                {
                    simulate(&EventType::KeyPress(Key::MetaLeft)).ok(); // Cmd on macOS
                    simulate(&EventType::KeyPress(Key::KeyV)).ok();
                    simulate(&EventType::KeyRelease(Key::KeyV)).ok();
                    simulate(&EventType::KeyRelease(Key::MetaLeft)).ok();
                }
            }
        }

        InputEvent::ClipboardCopy => {
            // Trigger copy operation (Ctrl+C / Cmd+C)
            #[cfg(target_os = "windows")]
            {
                simulate(&EventType::KeyPress(Key::ControlLeft)).ok();
                simulate(&EventType::KeyPress(Key::KeyC)).ok();
                simulate(&EventType::KeyRelease(Key::KeyC)).ok();
                simulate(&EventType::KeyRelease(Key::ControlLeft)).ok();
            }
            
            #[cfg(not(target_os = "windows"))]
            {
                simulate(&EventType::KeyPress(Key::MetaLeft)).ok();
                simulate(&EventType::KeyPress(Key::KeyC)).ok();
                simulate(&EventType::KeyRelease(Key::KeyC)).ok();
                simulate(&EventType::KeyRelease(Key::MetaLeft)).ok();
            }
            debug!("Clipboard copy triggered");
        }
    }

    Ok(())
}

/// Get screen dimensions
fn get_screen_size() -> Result<(i64, i64)> {
    // Use scap to get display size
    let targets = scap::get_all_targets();
    if let Some(Target::Display(display)) = targets.first() {
        let attrs = scap::get_target_display_info(*display)?;
        return Ok((attrs.width as i64, attrs.height as i64));
    }
    
    // Fallback to common resolution
    Ok((1920, 1080))
}

/// Map Web KeyboardEvent.code to rdev::Key
fn map_web_key_code(code: &str) -> Option<Key> {
    match code {
        "Enter" => Some(Key::Return),
        "Backspace" => Some(Key::Backspace),
        "Tab" => Some(Key::Tab),
        "Escape" => Some(Key::Escape),
        "Space" => Some(Key::Space),
        "ArrowUp" => Some(Key::UpArrow),
        "ArrowDown" => Some(Key::DownArrow),
        "ArrowLeft" => Some(Key::LeftArrow),
        "ArrowRight" => Some(Key::RightArrow),
        
        "KeyA" => Some(Key::KeyA),
        "KeyB" => Some(Key::KeyB),
        "KeyC" => Some(Key::KeyC),
        "KeyD" => Some(Key::KeyD),
        "KeyE" => Some(Key::KeyE),
        "KeyF" => Some(Key::KeyF),
        "KeyG" => Some(Key::KeyG),
        "KeyH" => Some(Key::KeyH),
        "KeyI" => Some(Key::KeyI),
        "KeyJ" => Some(Key::KeyJ),
        "KeyK" => Some(Key::KeyK),
        "KeyL" => Some(Key::KeyL),
        "KeyM" => Some(Key::KeyM),
        "KeyN" => Some(Key::KeyN),
        "KeyO" => Some(Key::KeyO),
        "KeyP" => Some(Key::KeyP),
        "KeyQ" => Some(Key::KeyQ),
        "KeyR" => Some(Key::KeyR),
        "KeyS" => Some(Key::KeyS),
        "KeyT" => Some(Key::KeyT),
        "KeyU" => Some(Key::KeyU),
        "KeyV" => Some(Key::KeyV),
        "KeyW" => Some(Key::KeyW),
        "KeyX" => Some(Key::KeyX),
        "KeyY" => Some(Key::KeyY),
        "KeyZ" => Some(Key::KeyZ),
        
        "Digit0" => Some(Key::Num0),
        "Digit1" => Some(Key::Num1),
        "Digit2" => Some(Key::Num2),
        "Digit3" => Some(Key::Num3),
        "Digit4" => Some(Key::Num4),
        "Digit5" => Some(Key::Num5),
        "Digit6" => Some(Key::Num6),
        "Digit7" => Some(Key::Num7),
        "Digit8" => Some(Key::Num8),
        "Digit9" => Some(Key::Num9),
        
        "F1" => Some(Key::F1),
        "F2" => Some(Key::F2),
        "F3" => Some(Key::F3),
        "F4" => Some(Key::F4),
        "F5" => Some(Key::F5),
        "F6" => Some(Key::F6),
        "F7" => Some(Key::F7),
        "F8" => Some(Key::F8),
        "F9" => Some(Key::F9),
        "F10" => Some(Key::F10),
        "F11" => Some(Key::F11),
        "F12" => Some(Key::F12),
        
        "ControlLeft" | "ControlRight" => Some(Key::ControlLeft),
        "ShiftLeft" | "ShiftRight" => Some(Key::ShiftLeft),
        "AltLeft" | "AltRight" => Some(Key::Alt),
        "MetaLeft" | "MetaRight" => Some(Key::MetaLeft),
        
        _ => None,
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    let mut agent = Agent::new(AgentConfig::default());
    
    // Handle Ctrl+C for graceful shutdown
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.expect("Failed to listen for ctrl-c");
        info!("Received shutdown signal");
    });

    if let Err(e) = agent.run().await {
        error!("Agent failed: {}", e);
        std::process::exit(1);
    }

    Ok(())
}
