use crate::db;
use crate::ConnectionTarget;

#[tauri::command]
pub async fn delete_session(
    app: tauri::AppHandle,
    state: tauri::State<'_, db::DbState>,
    session_id: String,
    target: Option<ConnectionTarget>,
) -> Result<(), String> {
    delete_session_internal(app, state, session_id, target).await
}

pub async fn delete_session_internal(
    app: tauri::AppHandle,
    state: tauri::State<'_, db::DbState>,
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
            .map_err(|e| e.to_string())?;

        if !output.status.success() {
            eprintln!("Failed to delete remote session: {:?}", String::from_utf8_lossy(&output.stderr));
        }
    } else {
        if let Ok(Some(active)) = state.get_active_session(&session_id) {
            let _ = std::process::Command::new("kill")
                .arg("-9")
                .arg(active.pid.to_string())
                .status();
        }
        let _ = state.delete_active_session(&session_id);

        let home = dirs::home_dir().ok_or("Could not find home dir")?;
        let pb_path = home.join(format!(".divmora/localharness/conversations/{}.pb", session_id));
        let brain_path = home.join(format!(".divmora/localharness/brain/{}", session_id));

        let _ = std::fs::remove_file(pb_path);
        let _ = std::fs::remove_dir_all(brain_path);
    }

    Ok(())
}
