use rusqlite::{params, Connection, Result};
use std::path::PathBuf;

pub struct DbState {
    conn: std::sync::Mutex<Connection>,
}

pub fn init_db() -> Result<DbState> {
    let mut db_path = dirs::home_dir().expect("Home directory not found");
    db_path.push(".divmora/localharness");
    std::fs::create_dir_all(&db_path).ok();
    db_path.push("divmora-gui.db");

    let conn = Connection::open(db_path)?;

    conn.execute("DROP TABLE IF EXISTS session_spaces", [])?;
    conn.execute("DROP TABLE IF EXISTS spaces", [])?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS spaces (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            installation_id TEXT NOT NULL,
            created_at INTEGER NOT NULL
        )",
        [],
    )?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS settings (
            key TEXT PRIMARY KEY,
            current_value TEXT,
            default_value TEXT
        )",
        [],
    )?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS session_spaces (
            session_id TEXT PRIMARY KEY,
            space_id TEXT NOT NULL,
            FOREIGN KEY (space_id) REFERENCES spaces(id) ON DELETE CASCADE
        )",
        [],
    )?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS recent_projects (
            id TEXT PRIMARY KEY,
            path TEXT NOT NULL,
            target_kind TEXT NOT NULL,
            target_host TEXT,
            target_user TEXT,
            target_port INTEGER,
            target_key_path TEXT,
            last_opened_at INTEGER NOT NULL
        )",
        [],
    )?;

    Ok(DbState {
        conn: std::sync::Mutex::new(conn),
    })
}

#[derive(serde::Serialize)]
pub struct Space {
    pub id: String,
    pub name: String,
    pub installation_id: String,
}

#[derive(serde::Serialize, serde::Deserialize, Clone)]
pub struct RecentProject {
    pub id: String,
    pub path: String,
    pub target_kind: String,
    pub target_host: Option<String>,
    pub target_user: Option<String>,
    pub target_port: Option<i32>,
    pub target_key_path: Option<String>,
    pub last_opened_at: i64,
}

impl DbState {
    pub fn create_space(&self, id: &str, name: &str, installation_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
        conn.execute(
            "INSERT INTO spaces (id, name, installation_id, created_at) VALUES (?1, ?2, ?3, ?4)",
            params![id, name, installation_id, now],
        )?;
        Ok(())
    }

    pub fn get_spaces(&self, installation_id: &str) -> Result<Vec<Space>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT id, name, installation_id FROM spaces WHERE installation_id = ?1 ORDER BY created_at ASC")?;

        let mut spaces = Vec::new();
        let mut rows = stmt.query(params![installation_id])?;
        while let Some(row) = rows.next()? {
            spaces.push(Space {
                id: row.get(0)?,
                name: row.get(1)?,
                installation_id: row.get(2)?,
            });
        }

        Ok(spaces)
    }

    pub fn remove_session_from_space(&self, session_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "DELETE FROM session_spaces WHERE session_id = ?1",
            params![session_id],
        )?;
        Ok(())
    }

    pub fn get_setting(&self, key: &str) -> Result<Option<String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT current_value FROM settings WHERE key = ?1")?;
        let mut rows = stmt.query(params![key])?;
        if let Some(row) = rows.next()? {
            Ok(Some(row.get(0)?))
        } else {
            Ok(None)
        }
    }

    pub fn set_setting(&self, key: &str, current_value: &str, default_value: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO settings (key, current_value, default_value) VALUES (?1, ?2, ?3)
             ON CONFLICT(key) DO UPDATE SET current_value = excluded.current_value",
            params![key, current_value, default_value],
        )?;
        Ok(())
    }

    pub fn move_session_to_space(&self, session_id: &str, space_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO session_spaces (session_id, space_id) VALUES (?1, ?2) 
             ON CONFLICT(session_id) DO UPDATE SET space_id = excluded.space_id",
            params![session_id, space_id],
        )?;
        Ok(())
    }

    pub fn get_space_for_session(&self, session_id: &str) -> Result<Option<(String, String)>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT s.id, s.name FROM session_spaces ss 
             JOIN spaces s ON ss.space_id = s.id 
             WHERE ss.session_id = ?1",
        )?;

        let mut rows = stmt.query(params![session_id])?;
        if let Some(row) = rows.next()? {
            Ok(Some((row.get(0)?, row.get(1)?)))
        } else {
            Ok(None)
        }
    }

    pub fn get_session_spaces(&self) -> Result<std::collections::HashMap<String, String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT session_id, space_id FROM session_spaces")?;
        let mut rows = stmt.query([])?;

        let mut map = std::collections::HashMap::new();
        while let Some(row) = rows.next()? {
            map.insert(row.get(0)?, row.get(1)?);
        }
        Ok(map)
    }

    pub fn add_recent_project(&self, project: &RecentProject) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
            
        // Use an id format like "kind:user@host:port:path" or just let frontend send a unique string
        conn.execute(
            "INSERT INTO recent_projects (id, path, target_kind, target_host, target_user, target_port, target_key_path, last_opened_at) 
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
             ON CONFLICT(id) DO UPDATE SET last_opened_at = excluded.last_opened_at",
            params![
                project.id,
                project.path,
                project.target_kind,
                project.target_host,
                project.target_user,
                project.target_port,
                project.target_key_path,
                now
            ],
        )?;
        Ok(())
    }

    pub fn get_recent_projects(&self) -> Result<Vec<RecentProject>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT id, path, target_kind, target_host, target_user, target_port, target_key_path, last_opened_at FROM recent_projects ORDER BY last_opened_at DESC LIMIT 50")?;
        
        let mut projects = Vec::new();
        let mut rows = stmt.query([])?;
        
        while let Some(row) = rows.next()? {
            projects.push(RecentProject {
                id: row.get(0)?,
                path: row.get(1)?,
                target_kind: row.get(2)?,
                target_host: row.get(3)?,
                target_user: row.get(4)?,
                target_port: row.get(5)?,
                target_key_path: row.get(6)?,
                last_opened_at: row.get(7)?,
            });
        }
        
        Ok(projects)
    }
}
