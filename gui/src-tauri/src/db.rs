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

    conn.execute(
        "CREATE TABLE IF NOT EXISTS offices (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            country TEXT,
            workspace_path TEXT,
            created_at INTEGER NOT NULL
        )",
        [],
    )?;

    // Migration for existing offices
    let _ = conn.execute("ALTER TABLE offices ADD COLUMN country TEXT", []);
    let _ = conn.execute("ALTER TABLE offices ADD COLUMN workspace_path TEXT", []);

    // Ensure a Default Office exists
    let now = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_secs() as i64;
    conn.execute(
        "INSERT OR IGNORE INTO offices (id, name, country, created_at) VALUES ('default', 'Default Office', 'USA', ?1)",
        params![now],
    )?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS spaces (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            installation_id TEXT NOT NULL,
            office_id TEXT NOT NULL DEFAULT 'default' REFERENCES offices(id),
            created_at INTEGER NOT NULL
        )",
        [],
    )?;

    // Migration for existing spaces
    let _ = conn.execute("ALTER TABLE spaces ADD COLUMN office_id TEXT NOT NULL DEFAULT 'default' REFERENCES offices(id)", []);

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
        "CREATE TABLE IF NOT EXISTS office_managers (
            office_id TEXT PRIMARY KEY,
            manager_session_id TEXT NOT NULL
        )",
        [],
    )?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS archived_sessions (
            session_id TEXT PRIMARY KEY,
            archived_at INTEGER NOT NULL
        )",
        [],
    )?;

    conn.execute(
        "CREATE TABLE IF NOT EXISTS office_agents (
            session_id TEXT PRIMARY KEY,
            office_id TEXT NOT NULL,
            agent_name TEXT NOT NULL,
            role_description TEXT NOT NULL,
            employment_type TEXT NOT NULL,
            gender TEXT NOT NULL,
            experience_level TEXT NOT NULL,
            personality_traits TEXT NOT NULL DEFAULT '',
            current_tasks INTEGER NOT NULL DEFAULT 0,
            specializations TEXT NOT NULL DEFAULT '[]',
            visiting_session_id TEXT,
            FOREIGN KEY (office_id) REFERENCES offices(id) ON DELETE CASCADE
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

    conn.execute(
        "CREATE TABLE IF NOT EXISTS active_sessions (
            session_id TEXT PRIMARY KEY,
            pid INTEGER NOT NULL,
            port INTEGER NOT NULL,
            api_key TEXT NOT NULL,
            started_at INTEGER NOT NULL
        )",
        [],
    )?;

    Ok(DbState {
        conn: std::sync::Mutex::new(conn),
    })
}

#[derive(serde::Serialize)]
pub struct Office {
    pub id: String,
    pub name: String,
    pub country: Option<String>,
    pub workspace_path: Option<String>,
}

#[derive(serde::Serialize)]
pub struct Space {
    pub id: String,
    pub name: String,
    pub installation_id: String,
    pub office_id: String,
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

#[derive(serde::Serialize, serde::Deserialize, Clone)]
pub struct ActiveSession {
    pub session_id: String,
    pub pid: u32,
    pub port: u16,
    pub api_key: String,
    pub started_at: i64,
}

#[derive(serde::Serialize, serde::Deserialize, Clone)]
pub struct OfficeAgent {
    pub session_id: String,
    pub office_id: String,
    pub agent_name: String,
    pub role_description: String,
    pub employment_type: String,
    pub gender: String,
    pub experience_level: String,
    pub personality_traits: String,
    pub current_tasks: i32,
    pub specializations: String,
    pub visiting_session_id: Option<String>,
}

impl DbState {
    pub fn create_office(&self, id: &str, name: &str, country: &str, workspace_path: Option<&str>) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
        conn.execute(
            "INSERT INTO offices (id, name, country, workspace_path, created_at) VALUES (?1, ?2, ?3, ?4, ?5)",
            params![id, name, country, workspace_path, now],
        )?;
        Ok(())
    }

    pub fn update_office(&self, id: &str, name: &str, country: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE offices SET name = ?1, country = ?2 WHERE id = ?3",
            params![name, country, id],
        )?;
        Ok(())
    }

    pub fn get_office_manager(&self, office_id: &str) -> Result<Option<String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT manager_session_id FROM office_managers WHERE office_id = ?1")?;
        let mut rows = stmt.query([office_id])?;
        if let Some(row) = rows.next()? {
            Ok(Some(row.get(0)?))
        } else {
            Ok(None)
        }
    }

    pub fn get_all_office_managers(&self) -> Result<std::collections::HashMap<String, String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT office_id, manager_session_id FROM office_managers")?;
        let mut rows = stmt.query([])?;
        let mut map = std::collections::HashMap::new();
        while let Some(row) = rows.next()? {
            map.insert(row.get(0)?, row.get(1)?);
        }
        Ok(map)
    }

    pub fn set_office_manager(&self, office_id: &str, manager_session_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO office_managers (office_id, manager_session_id) VALUES (?1, ?2) ON CONFLICT(office_id) DO UPDATE SET manager_session_id = ?2",
            params![office_id, manager_session_id],
        )?;
        Ok(())
    }

    pub fn get_office_agents(&self, office_id: &str) -> Result<Vec<OfficeAgent>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT session_id, office_id, agent_name, role_description, employment_type, gender, experience_level, personality_traits, current_tasks, specializations, visiting_session_id FROM office_agents WHERE office_id = ?1")?;
        let rows = stmt.query_map([office_id], |row| {
            Ok(OfficeAgent {
                session_id: row.get(0)?,
                office_id: row.get(1)?,
                agent_name: row.get(2)?,
                role_description: row.get(3)?,
                employment_type: row.get(4)?,
                gender: row.get(5)?,
                experience_level: row.get(6)?,
                personality_traits: row.get(7)?,
                current_tasks: row.get(8)?,
                specializations: row.get(9)?,
                visiting_session_id: row.get(10)?,
            })
        })?;
        let mut agents = Vec::new();
        for row in rows {
            agents.push(row?);
        }
        Ok(agents)
    }

    pub fn get_agent_by_session_id(&self, session_id: &str) -> Result<Option<OfficeAgent>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT session_id, office_id, agent_name, role_description, employment_type, gender, experience_level, personality_traits, current_tasks, specializations, visiting_session_id FROM office_agents WHERE session_id = ?1")?;
        let mut rows = stmt.query([session_id])?;
        if let Some(row) = rows.next()? {
            Ok(Some(OfficeAgent {
                session_id: row.get(0)?,
                office_id: row.get(1)?,
                agent_name: row.get(2)?,
                role_description: row.get(3)?,
                employment_type: row.get(4)?,
                gender: row.get(5)?,
                experience_level: row.get(6)?,
                personality_traits: row.get(7)?,
                current_tasks: row.get(8)?,
                specializations: row.get(9)?,
                visiting_session_id: row.get(10)?,
            }))
        } else {
            Ok(None)
        }
    }

    pub fn add_office_agent(&self, agent: OfficeAgent) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO office_agents (session_id, office_id, agent_name, role_description, employment_type, gender, experience_level, personality_traits, current_tasks, specializations, visiting_session_id) 
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)
             ON CONFLICT(session_id) DO UPDATE SET 
                agent_name=excluded.agent_name, 
                role_description=excluded.role_description, 
                employment_type=excluded.employment_type,
                gender=excluded.gender,
                experience_level=excluded.experience_level,
                personality_traits=excluded.personality_traits",
            params![
                agent.session_id,
                agent.office_id,
                agent.agent_name,
                agent.role_description,
                agent.employment_type,
                agent.gender,
                agent.experience_level,
                agent.personality_traits,
                agent.current_tasks,
                agent.specializations,
                agent.visiting_session_id
            ],
        )?;
        Ok(())
    }

    pub fn update_agent_tasks(&self, session_id: &str, current_tasks: i32) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE office_agents SET current_tasks = ?1 WHERE session_id = ?2",
            params![current_tasks, session_id],
        )?;
        Ok(())
    }

    pub fn update_visiting_session_id(&self, session_id: &str, visiting_session_id: Option<String>) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE office_agents SET visiting_session_id = ?1 WHERE session_id = ?2",
            params![visiting_session_id, session_id],
        )?;
        Ok(())
    }

    pub fn get_offices(&self) -> Result<Vec<Office>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT id, name, country, workspace_path FROM offices ORDER BY created_at ASC")?;

        let mut offices = Vec::new();
        let mut rows = stmt.query([])?;
        while let Some(row) = rows.next()? {
            offices.push(Office {
                id: row.get(0)?,
                name: row.get(1)?,
                country: row.get(2)?,
                workspace_path: row.get(3)?,
            });
        }

        Ok(offices)
    }

    pub fn delete_office(&self, id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        
        // Delete all office_agents associated with this office
        conn.execute("DELETE FROM office_agents WHERE office_id = ?1", params![id])?;
        // Delete all space mappings for spaces in this office
        conn.execute("DELETE FROM session_spaces WHERE space_id IN (SELECT id FROM spaces WHERE office_id = ?1)", params![id])?;
        // Delete all spaces associated with this office
        conn.execute("DELETE FROM spaces WHERE office_id = ?1", params![id])?;
        // Delete the office manager
        conn.execute("DELETE FROM office_managers WHERE office_id = ?1", params![id])?;
        // Delete the office itself
        conn.execute("DELETE FROM offices WHERE id = ?1", params![id])?;
        
        Ok(())
    }

    pub fn get_office_sessions(&self, office_id: &str) -> Result<Vec<String>> {
        let conn = self.conn.lock().unwrap();
        let mut session_ids = std::collections::HashSet::new();

        // Get manager session
        let mut stmt = conn.prepare("SELECT manager_session_id FROM office_managers WHERE office_id = ?1")?;
        let mut rows = stmt.query([office_id])?;
        if let Some(row) = rows.next()? {
            session_ids.insert(row.get::<_, String>(0)?);
        }

        // Get agent sessions
        let mut stmt = conn.prepare("SELECT session_id FROM office_agents WHERE office_id = ?1")?;
        let mut rows = stmt.query([office_id])?;
        while let Some(row) = rows.next()? {
            session_ids.insert(row.get::<_, String>(0)?);
        }

        // Get space sessions
        let mut stmt = conn.prepare("SELECT session_id FROM session_spaces WHERE space_id IN (SELECT id FROM spaces WHERE office_id = ?1)")?;
        let mut rows = stmt.query([office_id])?;
        while let Some(row) = rows.next()? {
            session_ids.insert(row.get::<_, String>(0)?);
        }

        Ok(session_ids.into_iter().collect())
    }

    pub fn get_office(&self, id: &str) -> Result<Option<Office>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT id, name, country, workspace_path FROM offices WHERE id = ?1")?;
        let mut rows = stmt.query([id])?;
        if let Some(row) = rows.next()? {
            Ok(Some(Office {
                id: row.get(0)?,
                name: row.get(1)?,
                country: row.get(2)?,
                workspace_path: row.get(3)?,
            }))
        } else {
            Ok(None)
        }
    }

    pub fn create_space(&self, id: &str, name: &str, installation_id: &str, office_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
        conn.execute(
            "INSERT INTO spaces (id, name, installation_id, office_id, created_at) VALUES (?1, ?2, ?3, ?4, ?5)",
            params![id, name, installation_id, office_id, now],
        )?;
        Ok(())
    }

    pub fn get_spaces(&self, installation_id: &str, office_id: &str) -> Result<Vec<Space>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT id, name, installation_id, office_id FROM spaces WHERE installation_id = ?1 AND office_id = ?2 ORDER BY created_at ASC")?;

        let mut spaces = Vec::new();
        let mut rows = stmt.query(params![installation_id, office_id])?;
        while let Some(row) = rows.next()? {
            spaces.push(Space {
                id: row.get(0)?,
                name: row.get(1)?,
                installation_id: row.get(2)?,
                office_id: row.get(3)?,
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

    pub fn set_active_session(&self, session: &ActiveSession) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO active_sessions (session_id, pid, port, api_key, started_at) 
             VALUES (?1, ?2, ?3, ?4, ?5)
             ON CONFLICT(session_id) DO UPDATE SET 
             pid = excluded.pid, port = excluded.port, api_key = excluded.api_key, started_at = excluded.started_at",
            params![
                session.session_id,
                session.pid,
                session.port,
                session.api_key,
                session.started_at
            ],
        )?;
        Ok(())
    }

    pub fn get_active_session(&self, session_id: &str) -> Result<Option<ActiveSession>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT session_id, pid, port, api_key, started_at FROM active_sessions WHERE session_id = ?1")?;
        
        let mut rows = stmt.query(params![session_id])?;
        if let Some(row) = rows.next()? {
            Ok(Some(ActiveSession {
                session_id: row.get(0)?,
                pid: row.get(1)?,
                port: row.get(2)?,
                api_key: row.get(3)?,
                started_at: row.get(4)?,
            }))
        } else {
            Ok(None)
        }
    }

    pub fn delete_active_session(&self, session_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM active_sessions WHERE session_id = ?1", params![session_id])?;
        Ok(())
    }

    pub fn get_archived_sessions(&self) -> Result<Vec<String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT session_id FROM archived_sessions")?;
        let rows = stmt.query_map([], |row| row.get(0))?;
        
        let mut result = Vec::new();
        for r in rows {
            result.push(r?);
        }
        Ok(result)
    }

    pub fn archive_session(&self, session_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        let now = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_secs() as i64;
        conn.execute(
            "INSERT OR IGNORE INTO archived_sessions (session_id, archived_at) VALUES (?1, ?2)",
            params![session_id, now]
        )?;
        Ok(())
    }

    pub fn unarchive_session(&self, session_id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM archived_sessions WHERE session_id = ?1", params![session_id])?;
        Ok(())
    }
}
