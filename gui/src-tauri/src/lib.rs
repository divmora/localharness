use prost::Message;
use serde::Serialize;
use std::io::Write;
use tauri_plugin_shell::{process::CommandEvent, ShellExt};

mod localharness;
mod resolver;
use localharness::v1::{InputConfig, OutputConfig, ConversationState, SessionInfo, SessionList};

#[derive(Serialize)]
pub struct HarnessConnection {
    pub port: i32,
    pub api_key: String,
}

#[tauri::command]
async fn list_files(dir: Option<String>) -> Result<Vec<String>, String> {
    let mut target = dir.unwrap_or_else(|| ".".to_string());
    if target.starts_with("~/") {
        if let Some(home) = dirs::home_dir() {
            target = target.replacen("~/", &format!("{}/", home.display()), 1);
        }
    }
    
    let mut files = Vec::new();
    if let Ok(entries) = std::fs::read_dir(&target) {
        for entry in entries.flatten() {
            if let Ok(name) = entry.file_name().into_string() {
                if !name.starts_with('.') && name != "node_modules" && name != "target" && name != "bin" {
                    let is_dir = entry.file_type().map(|t| t.is_dir()).unwrap_or(false);
                    if is_dir {
                        files.push(format!("{}/", name));
                    } else {
                        files.push(name);
                    }
                }
            }
        }
    }
    // Sort directories first, then alphabetically
    files.sort_by(|a, b| {
        let a_is_dir = a.ends_with('/');
        let b_is_dir = b.ends_with('/');
        if a_is_dir && !b_is_dir {
            std::cmp::Ordering::Less
        } else if !a_is_dir && b_is_dir {
            std::cmp::Ordering::Greater
        } else {
            a.cmp(b)
        }
    });
    Ok(files)
}

#[tauri::command]
async fn read_file(path: String) -> Result<String, String> {
    let mut target = path;
    if target.starts_with("~/") {
        if let Some(home) = dirs::home_dir() {
            target = target.replacen("~/", &format!("{}/", home.display()), 1);
        }
    }
    std::fs::read_to_string(target).map_err(|e| e.to_string())
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
                let mut status = localharness::v1::SessionStatus::Ready;
                
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
                        
                        // Check last message for status
                        if let Some(last_msg) = state.messages.last() {
                            if last_msg.role == "model" && !last_msg.tool_calls.is_empty() {
                                // Model made tool calls but no response yet
                                let has_question = last_msg.tool_calls.iter().any(|t| t.name == "ask_question");
                                if has_question {
                                    status = localharness::v1::SessionStatus::Blocked;
                                } else {
                                    status = localharness::v1::SessionStatus::Running;
                                }
                            }
                        }
                    }
                }
                
                sessions.push(SessionInfo {
                    id: id.clone(),
                    name,
                    updated_at,
                    status: status.into(),
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

#[derive(serde::Deserialize, Clone)]
pub struct ConnectionTarget {
    pub kind: String, // "local" or "ssh"
    pub host: Option<String>,
    pub user: Option<String>,
    pub port: Option<u16>,
    pub key_path: Option<String>,
}

#[tauri::command]
async fn start_harness(app: tauri::AppHandle, target: Option<ConnectionTarget>) -> Result<HarnessConnection, String> {
    let target = target.unwrap_or(ConnectionTarget {
        kind: "local".to_string(),
        host: None,
        user: None,
        port: None,
        key_path: None,
    });

    let (mut rx, mut child) = if target.kind == "ssh" {
        let host = target.host.as_ref().ok_or("SSH host required")?;
        
        let mut args = vec!["-T".to_string()];
        
        if let Some(ssh_port) = target.port {
            args.push("-p".to_string());
            args.push(ssh_port.to_string());
        }
        
        if let Some(key) = target.key_path.as_ref() {
            if !key.is_empty() {
                args.push("-i".to_string());
                args.push(key.clone());
            }
        }
        
        if let Some(user) = target.user.as_ref() {
            if !user.is_empty() {
                args.push(format!("{}@{}", user, host));
            } else {
                args.push(host.clone());
            }
        } else {
            args.push(host.clone());
        }
        
        // Read version from manifest
        let manifest_str = include_str!("../../../.release-please-manifest.json");
        let manifest: serde_json::Value = serde_json::from_str(manifest_str)
            .map_err(|e| format!("Failed to parse manifest: {}", e))?;
        let version = manifest["."].as_str().ok_or("No version in manifest")?;
        let tag_name = format!("v{}", version);

        // Auto-deploy script over SSH
        let deploy_script = format!(
            "mkdir -p ~/.divmora/localharness/bin/{tag_name} && \
             if [ ! -f ~/.divmora/localharness/bin/{tag_name}/localharness ]; then \
               curl -sL https://github.com/divmora/localharness/releases/download/{tag_name}/localharness-linux-amd64.tar.gz | tar xz -C ~/.divmora/localharness/bin/{tag_name}; \
             fi && \
             ~/.divmora/localharness/bin/{tag_name}/localharness"
        );
        args.push(deploy_script);

        app.shell()
            .command("ssh")
            .args(args)
            .spawn()
            .map_err(|e| format!("Failed to spawn ssh: {}", e))?
    } else {
        let binary_path = resolver::resolve_localharness().await?;
        app.shell()
            .command(binary_path.to_string_lossy().to_string())
            .spawn()
            .map_err(|e| format!("Failed to spawn localharness: {}", e))?
    };

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

                        let mut local_port = output_cfg.port;
                        if target.kind == "ssh" {
                            local_port = setup_ssh_tunnel(&app, &target, output_cfg.port).await?;
                        }

                        return Ok(HarnessConnection {
                            port: local_port,
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

async fn setup_ssh_tunnel(app: &tauri::AppHandle, target: &ConnectionTarget, remote_port: i32) -> Result<i32, String> {
    let listener = std::net::TcpListener::bind("127.0.0.1:0").map_err(|e| format!("Bind failed: {}", e))?;
    let local_port = listener.local_addr().unwrap().port() as i32;
    drop(listener);

    let host = target.host.as_ref().unwrap();
    
    let mut args = vec![
        "-N".to_string(),
        "-L".to_string(), format!("{}:127.0.0.1:{}", local_port, remote_port),
    ];
    
    if let Some(ssh_port) = target.port {
        args.push("-p".to_string());
        args.push(ssh_port.to_string());
    }
    
    if let Some(key) = target.key_path.as_ref() {
        if !key.is_empty() {
            args.push("-i".to_string());
            args.push(key.clone());
        }
    }
    
    if let Some(user) = target.user.as_ref() {
        if !user.is_empty() {
            args.push(format!("{}@{}", user, host));
        } else {
            args.push(host.clone());
        }
    } else {
        args.push(host.clone());
    }

    app.shell()
        .command("ssh")
        .args(args)
        .spawn()
        .map_err(|e| format!("Failed to spawn ssh tunnel: {}", e))?;

    tokio::time::sleep(std::time::Duration::from_millis(500)).await;

    Ok(local_port)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_websocket::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![start_harness, list_sessions, list_files, read_file])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
