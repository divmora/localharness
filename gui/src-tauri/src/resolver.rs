use flate2::read::GzDecoder;
use serde_json::Value;
use std::fs;
use std::io::Cursor;
use std::path::PathBuf;
use tar::Archive;

pub async fn resolve_localharness() -> Result<PathBuf, String> {
    // 1. Check LOCALHARNESS_BIN env var
    if let Ok(env_path) = std::env::var("LOCALHARNESS_BIN") {
        let p = PathBuf::from(env_path);
        if p.exists() {
            return Ok(p);
        }
    }

    // 2. Check local dev paths (relative to tauri app)
    let dev_paths = vec![
        PathBuf::from("../../bin/localharness"),
        PathBuf::from("./bin/localharness"),
    ];
    for p in dev_paths {
        if p.exists() {
            return Ok(p.canonicalize().unwrap_or(p));
        }
    }

    // 3. Auto-download from GitHub using version from manifest
    let manifest_str = include_str!("../../../.release-please-manifest.json");
    let manifest: Value = serde_json::from_str(manifest_str).map_err(|e| e.to_string())?;
    let version = manifest["."].as_str().ok_or("No version in manifest")?;
    let tag_name = format!("v{}", version);

    let client = reqwest::Client::builder()
        .user_agent("localharness-tauri")
        .build()
        .map_err(|e| e.to_string())?;

    let release_url = format!(
        "https://api.github.com/repos/divmora/localharness/releases/tags/{}",
        tag_name
    );
    let resp: Value = client
        .get(&release_url)
        .send()
        .await
        .map_err(|e| e.to_string())?
        .json()
        .await
        .map_err(|e| e.to_string())?;

    let home_dir = dirs::home_dir().ok_or("Could not find home directory")?;
    let cache_dir = home_dir.join(".divmora/localharness/bin").join(tag_name);
    let binary_path = cache_dir.join("localharness");

    if binary_path.exists() {
        return Ok(binary_path);
    }

    // Determine platform suffix
    let os = std::env::consts::OS;
    let arch = std::env::consts::ARCH;

    let plat_suffix = match (os, arch) {
        ("linux", "x86_64") => "linux-amd64",
        ("linux", "aarch64") => "linux-arm64",
        ("macos", "x86_64") => "darwin-amd64",
        ("macos", "aarch64") => "darwin-arm64",
        ("windows", "x86_64") => "windows-amd64",
        _ => return Err(format!("Unsupported platform: {}/{}", os, arch)),
    };

    let expected_asset = format!("localharness-{}-{}.tar.gz", version, plat_suffix);

    let assets = resp["assets"].as_array().ok_or("No assets in release")?;
    let mut download_url = None;

    for asset in assets {
        if let Some(name) = asset["name"].as_str() {
            if name == expected_asset {
                download_url = asset["browser_download_url"].as_str().map(String::from);
                break;
            }
        }
    }

    let download_url = download_url.ok_or(format!(
        "Could not find asset {} in latest release",
        expected_asset
    ))?;

    // Download and extract
    log::info!("Downloading localharness {} from {}", version, download_url);
    fs::create_dir_all(&cache_dir).map_err(|e| e.to_string())?;

    let tarball_bytes = client
        .get(&download_url)
        .send()
        .await
        .map_err(|e| e.to_string())?
        .bytes()
        .await
        .map_err(|e| e.to_string())?;

    let cursor = Cursor::new(tarball_bytes);
    let tar = GzDecoder::new(cursor);
    let mut archive = Archive::new(tar);

    let mut found = false;
    for file in archive.entries().map_err(|e| e.to_string())? {
        let mut file = file.map_err(|e| e.to_string())?;
        if let Ok(path) = file.path() {
            if path.file_name().and_then(|n| n.to_str()) == Some("localharness") {
                file.unpack(&binary_path).map_err(|e| e.to_string())?;
                found = true;
                break;
            }
        }
    }

    if !found {
        return Err("Binary not found in archive".to_string());
    }

    // chmod +x
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&binary_path)
            .map_err(|e| e.to_string())?
            .permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&binary_path, perms).map_err(|e| e.to_string())?;
    }

    Ok(binary_path)
}
