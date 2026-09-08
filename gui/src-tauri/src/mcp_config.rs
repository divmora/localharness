use serde_json::Value;

pub fn get_mcp_config_path() -> Result<std::path::PathBuf, String> {
    let home = dirs::home_dir().ok_or("Could not find home dir")?;
    Ok(home.join(".divmora/config/mcp_config.json"))
}

#[tauri::command]
pub async fn get_mcp_config() -> Result<Value, String> {
    let path = get_mcp_config_path()?;
    if let Ok(content) = std::fs::read_to_string(&path) {
        if let Ok(config) = serde_json::from_str(&content) {
            return Ok(config);
        }
    }
    
    // Return empty mcpServers object if missing or invalid
    Ok(serde_json::json!({ "mcpServers": {} }))
}

#[tauri::command]
pub async fn save_mcp_config(config: Value) -> Result<(), String> {
    let content = serde_json::to_string_pretty(&config).map_err(|e| e.to_string())?;
    let path = get_mcp_config_path()?;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }
    std::fs::write(&path, content).map_err(|e| e.to_string())
}
