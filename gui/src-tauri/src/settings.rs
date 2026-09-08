use serde_json::Value;

pub fn get_app_settings_path() -> Result<std::path::PathBuf, String> {
    let home = dirs::home_dir().ok_or("Could not find home dir")?;
    Ok(home.join(".divmora/config/settings.json"))
}

#[tauri::command]
pub async fn get_app_settings() -> Result<Value, String> {
    let path = get_app_settings_path()?;
    if let Ok(content) = std::fs::read_to_string(&path) {
        if let Ok(config) = serde_json::from_str(&content) {
            return Ok(config);
        }
    }
    
    // Return empty object if missing or invalid
    Ok(serde_json::json!({}))
}

#[tauri::command]
pub async fn save_app_settings(config: Value) -> Result<(), String> {
    let content = serde_json::to_string_pretty(&config).map_err(|e| e.to_string())?;
    let path = get_app_settings_path()?;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }
    std::fs::write(&path, content).map_err(|e| e.to_string())
}
