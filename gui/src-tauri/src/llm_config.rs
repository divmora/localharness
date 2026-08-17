use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct LlmEndpoint {
    #[serde(rename = "baseUrl", default)]
    pub base_url: String,
    #[serde(rename = "apiKey", default)]
    pub api_key: String,
    #[serde(rename = "defaultModel", default)]
    pub default_model: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct LlmConfig {
    #[serde(rename = "defaultEndpoint", default)]
    pub default_endpoint: String,
    #[serde(default)]
    pub endpoints: HashMap<String, LlmEndpoint>,
}

pub fn get_llm_config_path() -> Result<std::path::PathBuf, String> {
    let home = dirs::home_dir().ok_or("Could not find home dir")?;
    Ok(home.join(".divmora/config/litellm.json"))
}

#[tauri::command]
pub async fn get_llm_config() -> Result<LlmConfig, String> {
    let path = get_llm_config_path()?;
    if let Ok(content) = std::fs::read_to_string(&path) {
        if let Ok(config) = serde_json::from_str(&content) {
            return Ok(config);
        }
    }
    
    // Return default if missing or invalid
    let mut default_config = LlmConfig::default();
    default_config.endpoints.insert("divmora".to_string(), LlmEndpoint::default());
    Ok(default_config)
}

#[tauri::command]
pub async fn save_llm_endpoint(name: String, endpoint: LlmEndpoint) -> Result<(), String> {
    let mut config = get_llm_config().await.unwrap_or_default();
    config.endpoints.insert(name.clone(), endpoint);
    if config.default_endpoint.is_empty() {
        config.default_endpoint = name;
    }
    
    let content = serde_json::to_string_pretty(&config).map_err(|e| e.to_string())?;
    let path = get_llm_config_path()?;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }
    std::fs::write(&path, content).map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn delete_llm_endpoint(name: String) -> Result<(), String> {
    let mut config = get_llm_config().await.unwrap_or_default();
    config.endpoints.remove(&name);
    
    // Auto-update default if the default was deleted
    if config.default_endpoint == name {
        config.default_endpoint = config.endpoints.keys().next().cloned().unwrap_or_default();
    }
    
    let content = serde_json::to_string_pretty(&config).map_err(|e| e.to_string())?;
    let path = get_llm_config_path()?;
    std::fs::write(&path, content).map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn set_default_llm_endpoint(name: String) -> Result<(), String> {
    let mut config = get_llm_config().await.unwrap_or_default();
    if config.endpoints.contains_key(&name) {
        config.default_endpoint = name;
        let content = serde_json::to_string_pretty(&config).map_err(|e| e.to_string())?;
        let path = get_llm_config_path()?;
        std::fs::write(&path, content).map_err(|e| e.to_string())?;
    }
    Ok(())
}
