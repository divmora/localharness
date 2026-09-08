#[tauri::command]
pub async fn list_global_skills() -> Result<Vec<String>, String> {
    let home = dirs::home_dir().ok_or("Could not find home dir")?;
    let dir = home.join(".divmora/localharness/skills");
    
    let mut files = Vec::new();
    if let Ok(entries) = std::fs::read_dir(&dir) {
        for entry in entries.flatten() {
            let path = entry.path();
            let name = path.file_name().unwrap_or_default().to_string_lossy().into_owned();
            if path.is_dir() {
                files.push(format!("{}/", name));
            } else {
                files.push(name);
            }
        }
    }
    files.sort();
    Ok(files)
}

#[tauri::command]
pub async fn list_global_knowledge(proj_dir: Option<String>) -> Result<Vec<String>, String> {
    let home = dirs::home_dir().ok_or("Could not find home dir")?;
    let mut dir = home.join(".divmora/localharness/knowledge");
    if let Some(p) = proj_dir {
        dir = dir.join(p);
    }
    
    let mut files = Vec::new();
    if let Ok(entries) = std::fs::read_dir(&dir) {
        for entry in entries.flatten() {
            let path = entry.path();
            let name = path.file_name().unwrap_or_default().to_string_lossy().into_owned();
            if path.is_dir() {
                files.push(format!("{}/", name));
            } else {
                files.push(name);
            }
        }
    }
    files.sort();
    Ok(files)
}
