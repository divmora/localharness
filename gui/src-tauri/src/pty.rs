use portable_pty::{CommandBuilder, NativePtySystem, PtySize, PtySystem, MasterPty};
use std::sync::Mutex;
use std::io::{Read, Write};
use tauri::{AppHandle, Emitter, State};

pub struct PtyState {
    pub master: Mutex<Option<Box<dyn MasterPty + Send>>>,
    pub writer: Mutex<Option<Box<dyn Write + Send>>>,
}

#[tauri::command]
pub fn spawn_pty(app: AppHandle, state: State<'_, PtyState>, rows: u16, cols: u16) -> Result<(), String> {
    let pty_system = NativePtySystem::default();
    
    let size = PtySize {
        rows,
        cols,
        pixel_width: 0,
        pixel_height: 0,
    };
    
    let pair = pty_system.openpty(size).map_err(|e| e.to_string())?;
    
    #[cfg(target_os = "windows")]
    let cmd = CommandBuilder::new("powershell.exe");
    
    #[cfg(not(target_os = "windows"))]
    let cmd = CommandBuilder::new(std::env::var("SHELL").unwrap_or_else(|_| "/bin/bash".to_string()));
    
    let _child = pair.slave.spawn_command(cmd).map_err(|e| e.to_string())?;
    
    drop(pair.slave);
    
    let master = pair.master;
    let mut reader = master.try_clone_reader().map_err(|e| e.to_string())?;
    let writer = master.take_writer().map_err(|e| e.to_string())?;
    
    *state.master.lock().unwrap() = Some(master);
    *state.writer.lock().unwrap() = Some(writer);
    
    std::thread::spawn(move || {
        let mut buf = [0u8; 1024];
        loop {
            match reader.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    let data = &buf[..n];
                    let _ = app.emit("pty-output", data.to_vec());
                }
                Err(_) => break,
            }
        }
    });
    
    Ok(())
}

#[tauri::command]
pub fn write_pty(state: State<'_, PtyState>, data: String) -> Result<(), String> {
    if let Some(writer) = state.writer.lock().unwrap().as_mut() {
        let _ = writer.write_all(data.as_bytes());
    }
    Ok(())
}

#[tauri::command]
pub fn resize_pty(state: State<'_, PtyState>, rows: u16, cols: u16) -> Result<(), String> {
    if let Some(master) = state.master.lock().unwrap().as_mut() {
        let _ = master.resize(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        });
    }
    Ok(())
}
