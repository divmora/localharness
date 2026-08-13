use prost::Message;
use serde::Serialize;
use std::io::Write;
use tauri_plugin_shell::{process::CommandEvent, ShellExt};

mod localharness;
use localharness::v1::{InputConfig, OutputConfig, ConversationState, SessionInfo, SessionList};
use prost::Message;

#[derive(Serialize)]
pub struct HarnessConnection {
    pub port: i32,
    pub api_key: String,
}

#[tauri::command]
async fn list_sessions() -> Result<Vec<u8>, String> {
    let mut sessions = Vec::new();
    
    // We expect conversations in ~/.divmora/localharness/conversations/
    let home = dirs::home_dir().ok_or("Could not find home dir")?;
    let conv_dir = home.join(".divmora/localharness/conversations");
    
    if let Ok(entries) = std::fs::read_dir(conv_dir) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|s| s.to_str()) == Some("pb") {
                let id = path.file_stem().and_then(|s| s.to_str()).unwrap_or("").to_string();
                
                let metadata = entry.metadata().ok();
                let updated_at = metadata.and_then(|m| m.modified().ok())
                    .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                    .map(|d| d.as_secs() as i64)
                    .unwrap_or(0);
                
                let mut name = format!("Session {}", &id[..std::cmp::min(8, id.len())]);
                
                if let Ok(buf) = std::fs::read(&path) {
                    if let Ok(state) = ConversationState::decode(buf.as_slice()) {
                        if let Some(first_msg) = state.messages.iter().find(|m| m.role == "user" && !m.content.is_empty()) {
                            let content = first_msg.content.trim();
                            let first_line = content.lines().next().unwrap_or("").trim();
                            
                            let title = if first_line.len() > 40 {
                                format!("{}...", &first_line[..37])
                            } else {
                                first_line.to_string()
                            };
                            
                            if !title.is_empty() {
                                name = title;
                            }
                        }
                    }
                }
                
                sessions.push(SessionInfo {
                    id: id.clone(),
                    name,
                    updated_at,
                });
            }
        }
    }
    
    // Sort newest first
    sessions.sort_by(|a, b| b.updated_at.cmp(&a.updated_at));
    
    let session_list = SessionList { sessions };
    let mut buf = Vec::new();
    session_list.encode(&mut buf).map_err(|e| e.to_string())?;
    
    Ok(buf)
}

#[tauri::command]
async fn start_harness(app: tauri::AppHandle) -> Result<HarnessConnection, String> {
    // 1. Spawn sidecar
    let (mut rx, mut child) = app
        .shell()
        .sidecar("localharness")
        .map_err(|e| format!("Failed to create sidecar command: {}", e))?
        .spawn()
        .map_err(|e| format!("Failed to spawn sidecar: {}", e))?;

    // 2. Prepare InputConfig
    let input_cfg = InputConfig {
        workspace: String::new(),
        debug: true,
    };

    let mut buf = Vec::new();
    input_cfg.encode(&mut buf).map_err(|e| e.to_string())?;

    let mut payload = Vec::new();
    payload
        .write_all(&(buf.len() as u32).to_le_bytes())
        .unwrap();
    payload.write_all(&buf).unwrap();

    // 3. Send InputConfig via stdin
    child
        .write(&payload)
        .map_err(|e| format!("Failed to write to sidecar stdin: {}", e))?;

    // 4. Read OutputConfig from stdout
    let mut stdout_buf = Vec::new();

    while let Some(event) = rx.recv().await {
        match event {
            CommandEvent::Stdout(data) => {
                stdout_buf.extend_from_slice(&data);

                // Try to parse if we have at least 4 bytes
                if stdout_buf.len() >= 4 {
                    let mut len_bytes = [0u8; 4];
                    len_bytes.copy_from_slice(&stdout_buf[0..4]);
                    let length = u32::from_le_bytes(len_bytes) as usize;

                    if stdout_buf.len() >= 4 + length {
                        let payload_bytes = &stdout_buf[4..4 + length];
                        let output_cfg = OutputConfig::decode(payload_bytes)
                            .map_err(|e| format!("Failed to decode OutputConfig: {}", e))?;

                        return Ok(HarnessConnection {
                            port: output_cfg.port,
                            api_key: output_cfg.api_key,
                        });
                    }
                }
            }
            CommandEvent::Stderr(line) => {
                let s = String::from_utf8_lossy(&line);
                println!("SIDECAR STDERR: {}", s);
            }
            CommandEvent::Error(err) => {
                return Err(format!("Sidecar error: {}", err));
            }
            CommandEvent::Terminated(payload) => {
                return Err(format!("Sidecar terminated prematurely: {:?}", payload));
            }
            _ => {}
        }
    }

    Err("Sidecar closed without returning OutputConfig".to_string())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_websocket::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![start_harness, list_sessions])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
