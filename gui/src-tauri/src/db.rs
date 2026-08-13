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
        "CREATE TABLE IF NOT EXISTS session_spaces (
            session_id TEXT PRIMARY KEY,
            space_id TEXT NOT NULL,
            FOREIGN KEY (space_id) REFERENCES spaces(id) ON DELETE CASCADE
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

impl DbState {
    pub fn create_space(&self, id: &str, name: &str, installation_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        let now = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_secs() as i64;
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
             WHERE ss.session_id = ?1"
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
}
