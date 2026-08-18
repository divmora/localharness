use prost::Message;
use serde::Serialize;
use std::io::Write;
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::Manager;
use tauri_plugin_shell::{process::CommandEvent, ShellExt};
use std::collections::HashMap;
use tokio::sync::mpsc;
use std::sync::Mutex;
use tauri_plugin_dialog::DialogExt;
use std::fs;
use futures_util::{StreamExt, SinkExt};
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::protocol::Message as WsMessage;
use url::Url;

struct WsSenders(Mutex<HashMap<String, mpsc::Sender<Vec<u8>>>>);

mod db;
mod pty;
mod localharness;
mod resolver;
mod llm_config;
mod mcp_config;
mod settings;
mod assets;
use localharness::v1::{ConversationState, InputConfig, OutputConfig, SessionInfo, SessionList};

#[derive(Serialize)]
pub struct HarnessConnection {
    pub port: i32,
    pub api_key: String,
    pub session_id: String,
}

#[tauri::command]
async fn get_installation_id(target: Option<serde_json::Value>) -> Result<String, String> {
    let mut connection_target: Option<ConnectionTarget> = None;
    if let Some(v) = target {
        connection_target = Some(serde_json::from_value(v).map_err(|e| e.to_string())?);
    }

    if let Some(ct) = connection_target {
        if ct.kind == "ssh" {
            let host = ct.host.as_deref().unwrap_or("localhost");
            let user = ct.user.as_deref().unwrap_or("root");

            // SSH script to check and create installation_id
            let script = r#"
file="$HOME/.divmora/localharness/installation_id"
if [ ! -f "$file" ]; then
    mkdir -p "$HOME/.divmora/localharness"
    if command -v uuidgen > /dev/null; then
        uuidgen > "$file"
    elif [ -f /proc/sys/kernel/random/uuid ]; then
        cat /proc/sys/kernel/random/uuid > "$file"
    else
        echo "$(date +%s%N)" > "$file"
    fi
fi
cat "$file"
"#;
            let mut args = vec![
                "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
                "-o".to_string(), "BatchMode=yes".to_string(),
            ];
            if let Some(port) = ct.port {
                args.push("-p".to_string());
                args.push(port.to_string());
            }
            if let Some(key) = ct.key_path.as_deref() {
                if !key.is_empty() {
                    args.push("-i".to_string());
                    args.push(key.to_string());
                }
            }
            if let Some(user) = ct.user.as_deref() {
                if !user.is_empty() {
                    args.push(format!("{}@{}", user, host));
                } else {
                    args.push(host.to_string());
                }
            } else {
                args.push(host.to_string());
            }
            args.push(script.to_string());

            let output = std::process::Command::new("ssh")
                .args(&args)
                .output()
                .map_err(|e| format!("Failed to spawn ssh: {}", e))?;

            if output.status.success() {
                return Ok(String::from_utf8_lossy(&output.stdout).trim().to_string());
            } else {
                return Err(format!(
                    "Failed to get remote installation_id: {}",
                    String::from_utf8_lossy(&output.stderr)
                ));
            }
        }
    }

    // Local
    let mut path = dirs::home_dir().ok_or("No home dir")?;
    path.push(".divmora/localharness");
    std::fs::create_dir_all(&path).ok();
    path.push("installation_id");

    if !path.exists() {
        let new_id = uuid::Uuid::new_v4().to_string();
        std::fs::write(&path, &new_id).map_err(|e| e.to_string())?;
        return Ok(new_id);
    }

    let content = std::fs::read_to_string(&path).map_err(|e| e.to_string())?;
    Ok(content.trim().to_string())
}

#[tauri::command]
fn create_office(
    id: String,
    name: String,
    country: String,
    state: tauri::State<'_, crate::db::DbState>,
) -> Result<(), String> {
    state.create_office(&id, &name, &country).map_err(|e| e.to_string())
}

#[tauri::command]
fn get_office_manager(state: tauri::State<db::DbState>, office_id: String) -> Result<Option<String>, String> {
    state.get_office_manager(&office_id).map_err(|e| e.to_string())
}

#[tauri::command]
fn get_all_office_managers(state: tauri::State<db::DbState>) -> Result<std::collections::HashMap<String, String>, String> {
    state.get_all_office_managers().map_err(|e| e.to_string())
}

#[tauri::command]
fn set_office_manager(state: tauri::State<db::DbState>, office_id: String, manager_session_id: String) -> Result<(), String> {
    state.set_office_manager(&office_id, &manager_session_id).map_err(|e| e.to_string())
}

#[tauri::command]
fn get_offices(state: tauri::State<db::DbState>) -> Result<Vec<db::Office>, String> {
    state.get_offices().map_err(|e| e.to_string())
}

#[tauri::command]
fn create_space(
    state: tauri::State<db::DbState>,
    id: String,
    name: String,
    installation_id: String,
    office_id: String,
) -> Result<(), String> {
    state
        .create_space(&id, &name, &installation_id, &office_id)
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn get_spaces(
    state: tauri::State<db::DbState>,
    installation_id: String,
    office_id: String,
) -> Result<Vec<db::Space>, String> {
    state
        .get_spaces(&installation_id, &office_id)
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn move_session_to_space(
    state: tauri::State<db::DbState>,
    session_id: String,
    space_id: String,
) -> Result<(), String> {
    state
        .move_session_to_space(&session_id, &space_id)
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn get_session_spaces(
    state: tauri::State<db::DbState>,
) -> Result<std::collections::HashMap<String, String>, String> {
    state.get_session_spaces().map_err(|e| e.to_string())
}

#[tauri::command]
fn get_office_agents(
    state: tauri::State<db::DbState>,
    office_id: String,
) -> Result<Vec<db::OfficeAgent>, String> {
    state.get_office_agents(&office_id).map_err(|e| e.to_string())
}

#[tauri::command]
fn add_office_agent(
    state: tauri::State<db::DbState>,
    agent: db::OfficeAgent,
) -> Result<(), String> {
    state.add_office_agent(agent).map_err(|e| e.to_string())
}

#[tauri::command]
fn update_agent_tasks(
    state: tauri::State<db::DbState>,
    session_id: String,
    current_tasks: i32,
) -> Result<(), String> {
    state.update_agent_tasks(&session_id, current_tasks).map_err(|e| e.to_string())
}

#[tauri::command]
fn assign_task(
    state: tauri::State<db::DbState>,
    session_id: String,
) -> Result<Option<db::OfficeAgent>, String> {
    let agent = state.get_agent_by_session_id(&session_id)
        .map_err(|e| e.to_string())?
        .ok_or("Agent not found")?;

    let new_task_count = agent.current_tasks + 1;
    
    // Check limits
    let mut limit = 5; // Default for permanent hire
    if agent.role_description == "Office Manager" {
        limit = 10;
    } else if agent.employment_type == "consultancy" {
        limit = 2;
    }

    if new_task_count > limit {
        // Limit breached! Spawn a new agent to handle overflow
        // Reset the overflowing agent's tasks to limit or limit-1 to signify offloading
        state.update_agent_tasks(&session_id, limit).map_err(|e| e.to_string())?;

        let new_session_id = uuid::Uuid::new_v4().to_string();
        
        let new_agent = db::OfficeAgent {
            session_id: new_session_id.clone(),
            office_id: agent.office_id.clone(),
            agent_name: if agent.role_description == "Office Manager" { "Junior Developer".to_string() } else { "Consultant".to_string() },
            role_description: if agent.role_description == "Office Manager" { "Junior Developer".to_string() } else { "Consultant".to_string() },
            employment_type: if agent.role_description == "Office Manager" { "permanent".to_string() } else { "consultancy".to_string() },
            gender: "none".to_string(),
            experience_level: "junior".to_string(),
            personality_traits: "Eager to help, highly analytical, and strictly follows instructions without small talk.".to_string(),
            current_tasks: 1, // They take the overflow task
            specializations: "Overflow handler".to_string(),
            visiting_session_id: None,
        };

        state.add_office_agent(new_agent.clone()).map_err(|e| e.to_string())?;
        
        // Return the new agent so the UI knows a hire happened!
        Ok(Some(new_agent))
    } else {
        // Just increment and return None (no new hire)
        state.update_agent_tasks(&session_id, new_task_count).map_err(|e| e.to_string())?;
        Ok(None)
    }
}

#[tauri::command]
fn update_visiting_session_id(
    state: tauri::State<'_, db::DbState>,
    session_id: String,
    visiting_session_id: Option<String>
) -> Result<(), String> {
    state.update_visiting_session_id(&session_id, visiting_session_id).map_err(|e| e.to_string())
}

#[tauri::command]
fn add_recent_project(
    state: tauri::State<db::DbState>,
    project: db::RecentProject,
) -> Result<(), String> {
    state.add_recent_project(&project).map_err(|e| e.to_string())
}

#[tauri::command]
fn get_recent_projects(
    state: tauri::State<db::DbState>,
) -> Result<Vec<db::RecentProject>, String> {
    state.get_recent_projects().map_err(|e| e.to_string())
}

#[tauri::command]
fn get_setting(state: tauri::State<db::DbState>, key: String) -> Result<Option<String>, String> {
    state.get_setting(&key).map_err(|e| e.to_string())
}

#[tauri::command]
fn set_setting(
    state: tauri::State<db::DbState>,
    key: String,
    current_value: String,
    default_value: String,
) -> Result<(), String> {
    state
        .set_setting(&key, &current_value, &default_value)
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn get_wallet_balance(state: tauri::State<db::DbState>, office_id: String) -> Result<f64, String> {
    let key = format!("wallet_balance_{}", office_id);
    let val_str = state.get_setting(&key).map_err(|e| e.to_string())?.unwrap_or_else(|| "0.0".to_string());
    val_str.parse::<f64>().map_err(|e| e.to_string())
}

#[tauri::command]
fn add_wallet_balance(state: tauri::State<db::DbState>, office_id: String, amount: f64) -> Result<f64, String> {
    let key = format!("wallet_balance_{}", office_id);
    let val_str = state.get_setting(&key).map_err(|e| e.to_string())?.unwrap_or_else(|| "0.0".to_string());
    let mut current_bal = val_str.parse::<f64>().unwrap_or(0.0);
    current_bal += amount;
    state.set_setting(&key, &current_bal.to_string(), "0.0").map_err(|e| e.to_string())?;
    Ok(current_bal)
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
                if !name.starts_with('.')
                    && name != "node_modules"
                    && name != "target"
                    && name != "bin"
                {
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
async fn write_file(path: String, content: String) -> Result<(), String> {
    let mut target = path;
    if target.starts_with("~/") {
        if let Some(home) = dirs::home_dir() {
            target = target.replacen("~/", &format!("{}/", home.display()), 1);
        }
    }
    // Ensure the parent directory exists
    if let Some(parent) = std::path::Path::new(&target).parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    std::fs::write(target, content).map_err(|e| e.to_string())
}

#[tauri::command]
async fn read_target_file(
    target: Option<ConnectionTarget>,
    path: String,
) -> Result<String, String> {
    if let Some(t) = target {
        if t.kind == "ssh" {
            let host = t.host.as_ref().ok_or("SSH host required")?;
            let mut args = vec![
                "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
                "-o".to_string(), "BatchMode=yes".to_string(),
            ];
            if let Some(port) = t.port {
                args.push("-p".to_string());
                args.push(port.to_string());
            }
            if let Some(key) = t.key_path.as_ref() {
                if !key.is_empty() {
                    args.push("-i".to_string());
                    args.push(key.clone());
                }
            }
            if let Some(user) = t.user.as_ref() {
                if !user.is_empty() {
                    args.push(format!("{}@{}", user, host));
                } else {
                    args.push(host.clone());
                }
            } else {
                args.push(host.clone());
            }
            args.push(format!("cat {}", path));

            let output = std::process::Command::new("ssh")
                .args(&args)
                .output()
                .map_err(|e| format!("Failed to spawn ssh: {}", e))?;

            if output.status.success() {
                return Ok(String::from_utf8_lossy(&output.stdout).into_owned());
            } else {
                return Err(String::from_utf8_lossy(&output.stderr).into_owned());
            }
        }
    }
    // Fallback local
    read_file(path).await
}

#[tauri::command]
async fn write_target_file(
    target: Option<ConnectionTarget>,
    path: String,
    content: String,
) -> Result<(), String> {
    if let Some(t) = target {
        if t.kind == "ssh" {
            let host = t.host.as_ref().ok_or("SSH host required")?;
            let mut args = vec![
                "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
                "-o".to_string(), "BatchMode=yes".to_string(),
            ];
            if let Some(port) = t.port {
                args.push("-p".to_string());
                args.push(port.to_string());
            }
            if let Some(key) = t.key_path.as_ref() {
                if !key.is_empty() {
                    args.push("-i".to_string());
                    args.push(key.clone());
                }
            }
            if let Some(user) = t.user.as_ref() {
                if !user.is_empty() {
                    args.push(format!("{}@{}", user, host));
                } else {
                    args.push(host.clone());
                }
            } else {
                args.push(host.clone());
            }
            let script = format!("mkdir -p $(dirname {}) && cat > {}", path, path);
            args.push(script);

            let mut child = std::process::Command::new("ssh")
                .args(&args)
                .stdin(std::process::Stdio::piped())
                .stderr(std::process::Stdio::piped())
                .spawn()
                .map_err(|e| format!("Failed to spawn ssh: {}", e))?;

            if let Some(mut stdin) = child.stdin.take() {
                stdin
                    .write_all(content.as_bytes())
                    .map_err(|e| e.to_string())?;
            }

            let output = child.wait_with_output().map_err(|e| e.to_string())?;
            if output.status.success() {
                return Ok(());
            } else {
                return Err(String::from_utf8_lossy(&output.stderr).into_owned());
            }
        }
    }
    write_file(path, content).await
}

#[tauri::command]
async fn list_target_files(
    target: Option<ConnectionTarget>,
    dir: Option<String>,
) -> Result<Vec<String>, String> {
    if let Some(t) = target {
        if t.kind == "ssh" {
            let host = t.host.as_ref().ok_or("SSH host required")?;
            let mut args = vec![
                "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
                "-o".to_string(), "BatchMode=yes".to_string(),
            ];
            if let Some(port) = t.port {
                args.push("-p".to_string());
                args.push(port.to_string());
            }
            if let Some(key) = t.key_path.as_ref() {
                if !key.is_empty() {
                    args.push("-i".to_string());
                    args.push(key.clone());
                }
            }
            if let Some(user) = t.user.as_ref() {
                if !user.is_empty() {
                    args.push(format!("{}@{}", user, host));
                } else {
                    args.push(host.clone());
                }
            } else {
                args.push(host.clone());
            }

            let d = dir.clone().unwrap_or_else(|| ".".to_string());
            args.push(format!("ls -1pA {}", d));

            let output = std::process::Command::new("ssh")
                .args(&args)
                .output()
                .map_err(|e| format!("Failed to spawn ssh: {}", e))?;

            if output.status.success() {
                let stdout = String::from_utf8_lossy(&output.stdout);
                let mut files = Vec::new();
                for line in stdout.lines() {
                    let name = line.trim();
                    if name.is_empty()
                        || name == ".git/"
                        || name == "node_modules/"
                        || name == "target/"
                        || name == "bin/"
                    {
                        continue;
                    }
                    if name.starts_with('.') {
                        continue;
                    }
                    files.push(name.to_string());
                }

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
                return Ok(files);
            } else {
                return Err(String::from_utf8_lossy(&output.stderr).into_owned());
            }
        }
    }
    list_files(dir).await
}

#[tauri::command]
async fn list_sessions(
    app: tauri::AppHandle,
    state: tauri::State<'_, db::DbState>,
    target: Option<ConnectionTarget>,
) -> Result<Vec<u8>, String> {
    let mut sessions = Vec::new();

    let is_ssh = target.as_ref().map(|t| t.kind == "ssh").unwrap_or(false);

    if is_ssh {
        let t = target.as_ref().unwrap();
        let host = t.host.as_ref().ok_or("SSH host required")?;

        let mut args = vec![
            "-T".to_string(), "-q".to_string(),
            "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
            "-o".to_string(), "BatchMode=yes".to_string(),
        ];
        if let Some(port) = t.port {
            args.push("-p".to_string());
            args.push(port.to_string());
        }
        if let Some(key) = t.key_path.as_ref() {
            if !key.is_empty() {
                args.push("-i".to_string());
                args.push(key.clone());
            }
        }
        if let Some(user) = t.user.as_ref() {
            if !user.is_empty() {
                args.push(format!("{}@{}", user, host));
            } else {
                args.push(host.clone());
            }
        } else {
            args.push(host.clone());
        }

        // Fetch files using a small script that base64-encodes them with delimiters
        let script = "cd ~/.divmora/localharness/conversations 2>/dev/null && for f in *.pb; do [ -f \"$f\" ] || continue; echo \"::FILE_START::$f\"; base64 \"$f\"; echo \"::FILE_END::\"; done";
        args.push(script.to_string());

        use tauri_plugin_shell::ShellExt;
        let output = app
            .shell()
            .command("ssh")
            .args(args)
            .output()
            .await
            .map_err(|e| format!("Failed to spawn ssh: {}", e))?;

        if output.status.success() {
            let stdout = String::from_utf8_lossy(&output.stdout);

            let mut current_file = String::new();
            let mut current_b64 = String::new();
            let mut in_file = false;

            for line in stdout.lines() {
                let trimmed = line.trim();
                if trimmed.starts_with("::FILE_START::") {
                    current_file = trimmed.trim_start_matches("::FILE_START::").to_string();
                    current_b64.clear();
                    in_file = true;
                } else if trimmed == "::FILE_END::" {
                    in_file = false;

                    if let Ok(decoded) = base64::decode(&current_b64.replace("\n", "")) {
                        let id = std::path::Path::new(&current_file)
                            .file_stem()
                            .and_then(|s| s.to_str())
                            .unwrap_or("")
                            .to_string();
                        if id.is_empty() {
                            continue;
                        }

                        let mut name = format!("Session {}", &id[..std::cmp::min(8, id.len())]);
                        let mut status = localharness::v1::SessionStatus::Ready;
                        let mut updated_at = 0;
                        let mut workspace = String::new();
                        let mut client_source = String::new();
                        let mut budget_allocated = 0.0;
                        let mut budget_spent = 0.0;

                        if let Ok(conv_state) = ConversationState::decode(decoded.as_slice()) {
                            if let Some(config) = &conv_state.config {
                                client_source = config.client_source.clone();
                                if !config.workspaces.is_empty() {
                                    workspace = config.workspaces[0].directory.clone();
                                }
                            }
                            budget_allocated = conv_state.budget_allocated;
                            budget_spent = conv_state.budget_spent;

                            // Find last message timestamp
                            if let Some(last) = conv_state.messages.last() {
                                if let Ok(dt) =
                                    chrono::DateTime::parse_from_rfc3339(&last.timestamp)
                                {
                                    updated_at = dt.timestamp();
                                }
                            }

                            if let Some(first_msg) = conv_state
                                .messages
                                .iter()
                                .find(|m| m.role == "user" && !m.content.is_empty())
                            {
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
                            if let Some(last_msg) = conv_state.messages.last() {
                                if last_msg.role == "model" && !last_msg.tool_calls.is_empty() {
                                    let has_question = last_msg
                                        .tool_calls
                                        .iter()
                                        .any(|t| t.name == "ask_question");
                                    if has_question {
                                        status = localharness::v1::SessionStatus::Blocked;
                                    } else {
                                        status = localharness::v1::SessionStatus::Running;
                                    }
                                }
                            }
                        }

                        sessions.push(SessionInfo {
                            id: id.clone(),
                            name,
                            updated_at: updated_at as i64,
                            status: status.into(),
                            workspace,
                            client_source,
                            budget_allocated,
                            budget_spent,
                        });
                    }
                } else if in_file {
                    current_b64.push_str(trimmed);
                }
            }
        }
    } else {
        // We expect conversations in ~/.divmora/localharness/conversations/
        let home = dirs::home_dir().ok_or("Could not find home dir")?;
        let conv_dir = home.join(".divmora/localharness/conversations");

        if let Ok(entries) = std::fs::read_dir(conv_dir) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.extension().and_then(|s| s.to_str()) == Some("pb") {
                    let id = path
                        .file_stem()
                        .and_then(|s| s.to_str())
                        .unwrap_or("")
                        .to_string();

                    let metadata = entry.metadata().ok();
                    let mut updated_at = metadata
                        .and_then(|m| m.modified().ok())
                        .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                        .map(|d| d.as_secs() as i64)
                        .unwrap_or(0);

                    let mut name = format!("Session {}", &id[..std::cmp::min(8, id.len())]);
                    let mut status = localharness::v1::SessionStatus::Ready;
                    let mut workspace = String::new();
                    let mut client_source = String::new();
                    let mut budget_allocated = 0.0;
                    let mut budget_spent = 0.0;

                    if let Ok(buf) = std::fs::read(&path) {
                        if let Ok(conv_state) = ConversationState::decode(buf.as_slice()) {
                            if let Some(config) = &conv_state.config {
                                client_source = config.client_source.clone();
                                if !config.workspaces.is_empty() {
                                    workspace = config.workspaces[0].directory.clone();
                                }
                            }
                            budget_allocated = conv_state.budget_allocated;
                            budget_spent = conv_state.budget_spent;

                            // Find last message timestamp to override file mtime if present
                            if let Some(last) = conv_state.messages.last() {
                                if let Ok(dt) =
                                    chrono::DateTime::parse_from_rfc3339(&last.timestamp)
                                {
                                    updated_at = dt.timestamp() as i64;
                                }
                            }

                            if let Some(first_msg) = conv_state
                                .messages
                                .iter()
                                .find(|m| m.role == "user" && !m.content.is_empty())
                            {
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

                            // Overwrite status using DB & /status endpoint
                            if let Ok(Some(active)) = state.get_active_session(&id) {
                                let is_alive = std::process::Command::new("kill")
                                    .arg("-0")
                                    .arg(active.pid.to_string())
                                    .status()
                                    .map(|s| s.success())
                                    .unwrap_or(false);

                                if is_alive {
                                    // Default to running if alive
                                    status = localharness::v1::SessionStatus::Running;
                                    
                                    // Try to fetch true status from API
                                    let api_url = format!("http://127.0.0.1:{}/status", active.port);
                                    let client = reqwest::Client::new();
                                    if let Ok(res) = client.get(&api_url).header("x-localharness-api-key", &active.api_key).send().await {
                                        if let Ok(json) = res.json::<serde_json::Value>().await {
                                            if let Some(s) = json["status"].as_str() {
                                                match s {
                                                    "BLOCKED" => status = localharness::v1::SessionStatus::Blocked,
                                                    "RUNNING" => status = localharness::v1::SessionStatus::Running,
                                                    "IDLE" => status = localharness::v1::SessionStatus::Ready,
                                                    _ => {}
                                                }
                                            }
                                        }
                                    }
                                } else {
                                    // Process is dead, clean up DB
                                    let _ = state.delete_active_session(&id);
                                }
                            }
                        }
                    }

                    sessions.push(SessionInfo {
                        id: id.clone(),
                        name,
                        updated_at,
                        status: status.into(),
                        workspace,
                        client_source,
                        budget_allocated,
                        budget_spent,
                    });
                }
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
async fn delete_session(
    app: tauri::AppHandle,
    session_id: String,
    target: Option<ConnectionTarget>,
) -> Result<(), String> {
    let is_ssh = target.as_ref().map(|t| t.kind == "ssh").unwrap_or(false);

    if is_ssh {
        let t = target.as_ref().unwrap();
        let host = t.host.as_ref().ok_or("SSH host required")?;

        let mut args = vec![
            "-T".to_string(), "-q".to_string(),
            "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
            "-o".to_string(), "BatchMode=yes".to_string(),
        ];
        if let Some(port) = t.port {
            args.push("-p".to_string());
            args.push(port.to_string());
        }
        if let Some(key) = t.key_path.as_ref() {
            if !key.is_empty() {
                args.push("-i".to_string());
                args.push(key.clone());
            }
        }
        if let Some(user) = t.user.as_ref() {
            if !user.is_empty() {
                args.push(format!("{}@{}", user, host));
            } else {
                args.push(host.clone());
            }
        } else {
            args.push(host.clone());
        }

        let script = format!("rm -f ~/.divmora/localharness/conversations/{}.pb && rm -rf ~/.divmora/localharness/brain/{}", session_id, session_id);
        args.push(script);

        use tauri_plugin_shell::ShellExt;
        let output = app
            .shell()
            .command("ssh")
            .args(args)
            .output()
            .await
            .map_err(|e| format!("Failed to spawn ssh: {}", e))?;

        if !output.status.success() {
            return Err(format!("SSH rm failed: {}", String::from_utf8_lossy(&output.stderr)));
        }
    } else {
        let home = dirs::home_dir().ok_or("Could not find home dir")?;
        let conv_file = home.join(format!(".divmora/localharness/conversations/{}.pb", session_id));
        let brain_dir = home.join(format!(".divmora/localharness/brain/{}", session_id));

        let _ = std::fs::remove_file(conv_file);
        let _ = std::fs::remove_dir_all(brain_dir);
    }

    Ok(())
}

#[derive(serde::Deserialize, Clone)]
pub struct ConnectionTarget {
    pub kind: String, // "local" or "ssh"
    pub host: Option<String>,
    pub user: Option<String>,
    pub port: Option<u16>,
    pub key_path: Option<String>,
}

use std::io::Read;

#[tauri::command]
async fn start_harness(
    app: tauri::AppHandle,
    state: tauri::State<'_, db::DbState>,
    session_id: Option<String>,
    target: Option<ConnectionTarget>,
) -> Result<HarnessConnection, String> {
    let target = target.unwrap_or(ConnectionTarget {
        kind: "local".to_string(),
        host: None,
        user: None,
        port: None,
        key_path: None,
    });

    let provided_session_id = session_id.unwrap_or_default();

    if target.kind == "local" {
        // 1. Check if session is already running
        if !provided_session_id.is_empty() {
            if let Ok(Some(active)) = state.get_active_session(&provided_session_id) {
                let is_alive = std::process::Command::new("kill")
                    .arg("-0")
                    .arg(active.pid.to_string())
                    .status()
                    .map(|s| s.success())
                    .unwrap_or(false);

                if is_alive {
                    eprintln!("Session {} is already running at PID {}", provided_session_id, active.pid);
                    let needs_ws = {
                        let senders = app.state::<WsSenders>();
                        let map = senders.0.lock().unwrap();
                        !map.contains_key(&provided_session_id)
                    };
                    if needs_ws {
                        connect_and_proxy_websocket(app.clone(), provided_session_id.clone(), active.port as u16, active.api_key.clone()).await?;
                    }
                    return Ok(HarnessConnection {
                        port: active.port as i32,
                        api_key: active.api_key,
                        session_id: provided_session_id,
                    });
                } else {
                    eprintln!("Session {} PID {} is dead. Respawning.", provided_session_id, active.pid);
                    let _ = state.delete_active_session(&provided_session_id);
                }
            }
        }

        let binary_path = resolver::resolve_localharness().await?;

        // 2. Prepare InputConfig
        let input_cfg = InputConfig {
            workspace: String::new(),
            debug: true,
            session_id: provided_session_id.clone(),
        };
        let mut buf = Vec::new();
        input_cfg.encode(&mut buf).map_err(|e| e.to_string())?;
        let mut payload = Vec::new();
        payload.write_all(&(buf.len() as u32).to_le_bytes()).unwrap();
        payload.write_all(&buf).unwrap();

        // 3. Spawn detached process
        let mut child = std::process::Command::new(binary_path);
        child.stdin(std::process::Stdio::piped())
             .stdout(std::process::Stdio::piped())
             .stderr(std::process::Stdio::piped());
        #[cfg(unix)]
        {
            use std::os::unix::process::CommandExt;
            child.process_group(0);
        }

        let mut child_proc = child.spawn().map_err(|e| format!("Failed to spawn localharness: {}", e))?;
        let pid = child_proc.id();

        // 4. Send InputConfig via stdin
        let mut stdin = child_proc.stdin.take().unwrap();
        stdin.write_all(&payload).map_err(|e| format!("Failed to write to stdin: {}", e))?;
        // DO NOT drop(stdin). If stdin closes, the sidecar detects EOF and shuts down!
        // We leak the File Descriptor so it stays open until the Tauri app exits.
        std::mem::forget(stdin);

        // 5. Read OutputConfig from stdout
        let mut stdout = child_proc.stdout.take().unwrap();
        let mut len_bytes = [0u8; 4];
        stdout.read_exact(&mut len_bytes).map_err(|e| format!("Failed to read length from stdout: {}", e))?;
        let length = u32::from_le_bytes(len_bytes) as usize;
        let mut out_payload = vec![0u8; length];
        stdout.read_exact(&mut out_payload).map_err(|e| format!("Failed to read payload from stdout: {}", e))?;
        let output_cfg = OutputConfig::decode(&out_payload[..]).map_err(|e| format!("Failed to decode OutputConfig: {}", e))?;

        // 6. Spawn stderr background reader
        let stderr = child_proc.stderr.take().unwrap();
        let app_clone = app.clone();
        std::thread::spawn(move || {
            use std::io::{BufRead, BufReader};
            let reader = BufReader::new(stderr);
            for line in reader.lines() {
                if let Ok(l) = line {
                    eprintln!("SIDECAR STDERR: {}", l);
                    use tauri::Emitter;
                    let _ = app_clone.emit("sidecar-log", l);
                }
            }
        });

        // 7. Persist to DB
        let active = db::ActiveSession {
            session_id: output_cfg.session_id.clone(),
            pid,
            port: output_cfg.port as u16,
            api_key: output_cfg.api_key.clone(),
            started_at: std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_secs() as i64,
        };
        let _ = state.set_active_session(&active);

        // 8. Connect to the Sidecar WebSocket
        connect_and_proxy_websocket(app.clone(), output_cfg.session_id.clone(), output_cfg.port as u16, output_cfg.api_key.clone()).await?;

        return Ok(HarnessConnection {
            port: output_cfg.port,
            api_key: output_cfg.api_key,
            session_id: output_cfg.session_id,
        });
    }

    let (mut rx, mut child) = if target.kind == "ssh" {
        let host = target.host.as_ref().ok_or("SSH host required")?;

        let mut args = vec![
            "-T".to_string(), "-q".to_string(),
            "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
            "-o".to_string(), "BatchMode=yes".to_string(),
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

        // Read version from manifest
        let manifest_str = include_str!("../../../.release-please-manifest.json");
        let manifest: serde_json::Value = serde_json::from_str(manifest_str)
            .map_err(|e| format!("Failed to parse manifest: {}", e))?;
        let version = manifest["."].as_str().ok_or("No version in manifest")?;
        let tag_name = format!("v{}", version);

        let deploy_script = format!(
            "{{ \
             OS=$(uname -s | tr '[:upper:]' '[:lower:]') && \
             ARCH=$(uname -m) && \
             if [ \"$OS\" = \"darwin\" ]; then \
                 if [ \"$ARCH\" = \"x86_64\" ]; then PLAT=\"darwin-amd64\"; \
                 else PLAT=\"darwin-arm64\"; fi; \
             elif [ \"$OS\" = \"linux\" ]; then \
                 if [ \"$ARCH\" = \"x86_64\" ]; then PLAT=\"linux-amd64\"; \
                 else PLAT=\"linux-arm64\"; fi; \
             else echo \"Unsupported OS: $OS\"; exit 1; fi && \
             mkdir -p ~/.divmora/localharness/bin/{tag_name} && \
             if [ ! -f ~/.divmora/localharness/bin/{tag_name}/localharness ]; then \
               curl -sL https://github.com/divmora/localharness/releases/download/{tag_name}/localharness-{version}-$PLAT.tar.gz | tar xz -C ~/.divmora/localharness/bin/{tag_name}; \
             fi; \
             }} >&2 && \
             exec ~/.divmora/localharness/bin/{tag_name}/localharness",
            tag_name = tag_name,
            version = version
        );
        args.push(deploy_script);

        app.shell()
            .command("ssh")
            .args(args)
            .spawn()
            .map_err(|e| format!("Failed to spawn ssh: {}", e))?
    } else {
        return Err("Local execution is handled above, but ssh block fell through".to_string());
    };

    // 2. Prepare InputConfig
    let input_cfg = InputConfig {
        workspace: String::new(),
        debug: true,
        session_id: provided_session_id.clone(),
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
                eprintln!("STDOUT: {:?}", String::from_utf8_lossy(&data));
                stdout_buf.extend_from_slice(&data);

                // Try to parse if we have at least 4 bytes
                if stdout_buf.len() >= 4 {
                    let mut len_bytes = [0u8; 4];
                    len_bytes.copy_from_slice(&stdout_buf[0..4]);
                    let length = u32::from_le_bytes(len_bytes) as usize;
                    eprintln!("Parsed length: {}", length);

                    if stdout_buf.len() >= 4 + length {
                        let payload_bytes = &stdout_buf[4..4 + length];
                        let output_cfg = OutputConfig::decode(payload_bytes)
                            .map_err(|e| format!("Failed to decode OutputConfig: {}", e))?;

                        let mut local_port = output_cfg.port;
                        let mut tunnel_child_opt = None;
                        if target.kind == "ssh" {
                            let (port, tunnel_child) =
                                setup_ssh_tunnel(&app, &target, output_cfg.port).await?;
                            local_port = port;
                            tunnel_child_opt = Some(tunnel_child);
                        }

                        // Store child to prevent it from being dropped and killed
                        use tauri::Manager;
                        if let Some(state) = app.try_state::<AppState>() {
                            let mut children = state.children.lock().unwrap();
                            children.push(child);
                            if let Some(tc) = tunnel_child_opt {
                                children.push(tc);
                            }
                        }

                        // Keep processing stderr logs in background so sidecar's pipes aren't closed
                        let app_clone = app.clone();
                        tauri::async_runtime::spawn(async move {
                            while let Some(event) = rx.recv().await {
                                if let CommandEvent::Stderr(line) = event {
                                    let log_str = String::from_utf8_lossy(&line).to_string();
                                    eprintln!("SIDECAR STDERR: {}", log_str);
                                    let _ = app_clone.emit("sidecar-log", log_str);
                                }
                            }
                        });

                        connect_and_proxy_websocket(app.clone(), output_cfg.session_id.clone(), local_port as u16, output_cfg.api_key.clone()).await?;

                        return Ok(HarnessConnection {
                            port: local_port,
                            api_key: output_cfg.api_key,
                            session_id: output_cfg.session_id,
                        });
                    }
                }
            }
            CommandEvent::Stderr(line) => {
                let s = String::from_utf8_lossy(&line);
                eprintln!("SIDECAR STDERR: {}", s);
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

async fn setup_ssh_tunnel(
    app: &tauri::AppHandle,
    target: &ConnectionTarget,
    remote_port: i32,
) -> Result<(i32, CommandChild), String> {
    let listener =
        std::net::TcpListener::bind("127.0.0.1:0").map_err(|e| format!("Bind failed: {}", e))?;
    let local_port = listener.local_addr().unwrap().port() as i32;
    drop(listener);

    let host = target.host.as_ref().unwrap();

    let mut args = vec![
        "-N".to_string(),
        "-L".to_string(),
        format!("{}:127.0.0.1:{}", local_port, remote_port),
        "-o".to_string(), "StrictHostKeyChecking=accept-new".to_string(),
        "-o".to_string(), "BatchMode=yes".to_string(),
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

    let (mut rx, child) = app
        .shell()
        .command("ssh")
        .args(args)
        .spawn()
        .map_err(|e| format!("Failed to spawn ssh tunnel: {}", e))?;

    tauri::async_runtime::spawn(async move {
        while let Some(event) = rx.recv().await {
            if let CommandEvent::Stderr(line) = event {
                eprintln!("SSH TUNNEL STDERR: {}", String::from_utf8_lossy(&line));
            }
        }
    });

    tokio::time::sleep(std::time::Duration::from_millis(500)).await;

    Ok((local_port, child))
}

use tauri::{
    menu::{Menu, MenuItem, Submenu},
    Emitter,
};
use tauri_plugin_shell::process::CommandChild;

struct AppState {
    children: Mutex<Vec<CommandChild>>,
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let db = db::init_db().expect("Failed to initialize database");
    tauri::Builder::default()
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                let _ = window.hide();
                api.prevent_close();
            }
        })
        .plugin(tauri_plugin_os::init())
        .plugin(tauri_plugin_notification::init())
        .manage(db)
        .manage(AppState {
            children: Mutex::new(Vec::new()),
        })
        .setup(|app| {
            let handle = app.handle();
            
            if let Some(window) = handle.get_webview_window("main") {
                #[cfg(not(target_os = "macos"))]
                {
                    let _ = window.set_decorations(false);
                }
            }

            let mut menu = Menu::default(handle)?;

            let prefs_menu = Submenu::new(handle, "Preferences", true)?;
            let theme_i = MenuItem::new(handle, "Color Theme [⌘K ⌘T]", true, None::<&str>)?;
            prefs_menu.append(&theme_i)?;

            menu.append(&prefs_menu)?;
            app.set_menu(menu)?;

            let theme_id = theme_i.id().clone();
            app.on_menu_event(move |app_handle, event| {
                if event.id() == &theme_id {
                    let _ = app_handle.emit("open-theme-palette", ());
                }
            });

            let quit_i = MenuItem::new(handle, "Quit", true, None::<&str>)?;
            let show_i = MenuItem::new(handle, "Show App", true, None::<&str>)?;
            let tray_menu = Menu::with_items(handle, &[&show_i, &quit_i])?;

            let show_id = show_i.id().clone();
            let quit_id = quit_i.id().clone();

            TrayIconBuilder::new()
                .menu(&tray_menu)
                .icon(app.default_window_icon().unwrap().clone())
                .on_menu_event(move |app_handle, event| {
                    if event.id() == &show_id {
                        if let Some(window) = app_handle.get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        }
                    } else if event.id() == &quit_id {
                        app_handle.exit(0);
                    }
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        if let Some(window) = tray.app_handle().get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        }
                    }
                })
                .build(app)?;

            Ok(())
        })
        .plugin(tauri_plugin_websocket::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_playwright::init())
        .manage(WsSenders(Mutex::new(HashMap::new())))
        .manage(pty::PtyState { 
            master: std::sync::Mutex::new(None),
            writer: std::sync::Mutex::new(None)
        })
        .invoke_handler(tauri::generate_handler![
            start_harness,
            send_harness_message,
            list_sessions,
            list_files,
            read_file,
            write_file,
            read_target_file,
            write_target_file,
            list_target_files,
            create_office,
            get_offices,
            get_office_manager,
            get_all_office_managers,
            set_office_manager,
            get_office_agents,
            add_office_agent,
            update_agent_tasks,
            update_visiting_session_id,
            create_space,
            pty::spawn_pty,
            pty::write_pty,
            pty::resize_pty,
            get_spaces,
            move_session_to_space,
            get_session_spaces,
            assign_task,
            add_recent_project,
            get_recent_projects,
            get_wallet_balance,
            add_wallet_balance,
            delete_session,
            get_archived_sessions,
            archive_session,
            unarchive_session,
            get_installation_id,
            get_setting,
            set_setting,
            llm_config::get_llm_config,
            llm_config::save_llm_endpoint,
            llm_config::delete_llm_endpoint,
            llm_config::set_default_llm_endpoint,
            mcp_config::get_mcp_config,
            mcp_config::save_mcp_config,
            settings::get_app_settings,
            settings::save_app_settings,
            assets::list_global_skills,
            assets::list_global_knowledge
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

#[tauri::command]
async fn get_archived_sessions(state: tauri::State<'_, db::DbState>) -> Result<Vec<String>, String> {
    state.get_archived_sessions().map_err(|e| e.to_string())
}

#[tauri::command]
async fn archive_session(state: tauri::State<'_, db::DbState>, session_id: String) -> Result<(), String> {
    state.archive_session(&session_id).map_err(|e| e.to_string())
}

#[tauri::command]
async fn unarchive_session(state: tauri::State<'_, db::DbState>, session_id: String) -> Result<(), String> {
    state.unarchive_session(&session_id).map_err(|e| e.to_string())
}

#[tauri::command]
async fn send_harness_message(
    session_id: String,
    message: Vec<u8>,
    senders: tauri::State<'_, WsSenders>,
) -> Result<(), String> {
    let sender = {
        let map = senders.0.lock().unwrap();
        map.get(&session_id).cloned()
    };

    if let Some(sender) = sender {
        sender.send(message).await.map_err(|e| format!("Failed to send message: {}", e))?;
        Ok(())
    } else {
        Err(format!("No active WebSocket connection for session {}", session_id))
    }
}

async fn connect_and_proxy_websocket(app: tauri::AppHandle, session_id: String, port: u16, api_key: String) -> Result<(), String> {
    let ws_url = format!("ws://127.0.0.1:{}/ws", port);
    // Ensure ws_url is a valid request with all WebSocket headers
    let mut request = match tokio_tungstenite::tungstenite::client::IntoClientRequest::into_client_request(ws_url.clone()) {
        Ok(req) => req,
        Err(e) => {
            return Err(format!("Invalid WebSocket URL for {}: {}", session_id, e));
        }
    };

    request.headers_mut().insert("x-localharness-api-key", api_key.parse().unwrap());
    
    let mut retries = 0;
    let ws_stream = loop {
        match connect_async(request.clone()).await {
            Ok((stream, _)) => break stream,
            Err(e) => {
                if retries >= 10 {
                    return Err(format!("Failed to connect WebSocket for {}: {}", session_id, e));
                }
                retries += 1;
                tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            }
        }
    };

    let (mut write, mut read) = ws_stream.split();
    let (tx, mut rx) = mpsc::channel::<Vec<u8>>(100);

    // Store the sender in Tauri state
    {
        let senders = app.state::<WsSenders>();
        let mut map = senders.0.lock().unwrap();
        map.insert(session_id.clone(), tx);
    }

    // Write loop: reads from MPSC channel and writes to WebSocket
    let session_id_clone = session_id.clone();
    tauri::async_runtime::spawn(async move {
        use futures_util::SinkExt;
        let mut interval = tokio::time::interval(std::time::Duration::from_secs(15));
        loop {
            tokio::select! {
                data_opt = rx.recv() => {
                    if let Some(data) = data_opt {
                        if let Err(e) = write.send(tokio_tungstenite::tungstenite::protocol::Message::Binary(data.into())).await {
                            eprintln!("Write loop error for {}: {}", session_id_clone, e);
                            break;
                        }
                    } else {
                        break;
                    }
                }
                _ = interval.tick() => {
                    if let Err(e) = write.flush().await {
                        eprintln!("Write loop flush error for {}: {}", session_id_clone, e);
                        break;
                    }
                }
            }
        }
        let _ = write.close().await;
    });

    // Read loop: reads from WebSocket and emits Tauri events
    use tauri::Emitter;
    let event_name = format!("harness_event_{}", session_id);
    let session_id_read = session_id.clone();
    let app_read = app.clone();
    tauri::async_runtime::spawn(async move {
        while let Some(msg) = read.next().await {
            match msg {
                Ok(WsMessage::Binary(data)) => {
                    let _ = app_read.emit(&event_name, data.to_vec());
                }
                Ok(WsMessage::Close(_)) | Err(_) => {
                    break; // Connection closed
                }
                _ => {} // Ignore Ping/Pong/Text
            }
        }

        eprintln!("WebSocket proxy disconnected for {}", session_id_read);
        
        // Cleanup sender
        {
            let senders = app_read.state::<WsSenders>();
            let mut map = senders.0.lock().unwrap();
            map.remove(&session_id_read);
        }
    });

    Ok(())
}


