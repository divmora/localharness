use crate::db;
use crate::session::delete_session_internal;

#[tauri::command]
pub fn create_office(
    name: String,
    country: String,
    workspace_path: Option<String>,
    state: tauri::State<'_, db::DbState>,
) -> Result<String, String> {
    let id = uuid::Uuid::new_v4().to_string();
    
    let final_workspace = if let Some(path) = workspace_path {
        path
    } else {
        let home = dirs::home_dir().ok_or("Could not find home dir")?;
        let office_dir = home.join(format!(".divmora/localharness/offices/{}", id));
        std::fs::create_dir_all(&office_dir).map_err(|e| format!("Failed to create office dir: {}", e))?;
        office_dir.to_string_lossy().to_string()
    };

    state.create_office(&id, &name, &country, Some(&final_workspace)).map_err(|e| e.to_string())?;
        
    Ok(id)
}

#[tauri::command]
pub fn update_office(
    id: String,
    name: String,
    country: String,
    state: tauri::State<'_, db::DbState>,
) -> Result<(), String> {
    state.update_office(&id, &name, &country).map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn delete_office(
    app: tauri::AppHandle,
    state: tauri::State<'_, db::DbState>,
    id: String,
) -> Result<(), String> {
    // 1. Get office details before deleting
    let office = state.get_office(&id)
        .map_err(|e| e.to_string())?
        .ok_or("Office not found")?;

    // 2. Find all associated session IDs
    let session_ids = state.get_office_sessions(&id).map_err(|e| e.to_string())?;

    // 3. Delete all sessions locally
    for session_id in session_ids {
        // Run the delete_session command on each
        if let Err(e) = delete_session_internal(app.clone(), state.clone(), session_id, None).await {
            eprintln!("Failed to delete session during office deletion: {}", e);
        }
    }

    // 4. Delete DB records
    state.delete_office(&id).map_err(|e| e.to_string())?;

    // 5. Smart wipe directory if it's the auto-generated one
    if let Some(path) = office.workspace_path {
        if path.contains(".divmora/localharness/offices/") {
            let _ = std::fs::remove_dir_all(&path);
        }
    }

    Ok(())
}

#[tauri::command]
pub fn get_office_manager(state: tauri::State<db::DbState>, office_id: String) -> Result<Option<String>, String> {
    state.get_office_manager(&office_id).map_err(|e| e.to_string())
}

#[tauri::command]
pub fn get_all_office_managers(state: tauri::State<db::DbState>) -> Result<std::collections::HashMap<String, String>, String> {
    state.get_all_office_managers().map_err(|e| e.to_string())
}

#[tauri::command]
pub fn set_office_manager(state: tauri::State<db::DbState>, office_id: String, manager_session_id: String) -> Result<(), String> {
    state.set_office_manager(&office_id, &manager_session_id).map_err(|e| e.to_string())
}

#[tauri::command]
pub fn get_offices(state: tauri::State<db::DbState>) -> Result<Vec<db::Office>, String> {
    state.get_offices().map_err(|e| e.to_string())
}

#[tauri::command]
pub fn spawn_or_focus_office(
    app: tauri::AppHandle,
    id: String,
    name: String,
) -> Result<(), String> {
    use tauri::Manager;
    
    let label = format!("divmora-office-{}", id);
    if let Some(window) = app.get_webview_window(&label) {
        let _ = window.unminimize();
        let _ = window.set_focus();
        return Ok(());
    }

    let url = format!("/#/office/{}", id);
    let title = format!("Divmora - {}", name);

    tauri::WebviewWindowBuilder::new(&app, &label, tauri::WebviewUrl::App(url.into()))
        .title(title)
        .inner_size(1200.0, 800.0)
        .build()
        .map_err(|e| e.to_string())?;

    Ok(())
}
