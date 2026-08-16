import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Search, File, Folder, X } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import { ConnectionTarget } from '../hooks/useHarness';

interface FileExplorerProps {
  isOpen: boolean;
  onClose: () => void;
  onFileSelect: (path: string, content: string) => void;
  connectionTarget?: ConnectionTarget | null;
}

export function FileExplorer({ isOpen, onClose, onFileSelect, connectionTarget }: FileExplorerProps) {
  const [currentPath, setCurrentPath] = useState<string>('.');
  const [files, setFiles] = useState<string[]>([]);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    
    async function fetchFiles() {
      setLoading(true);
      try {
        const result = await invoke<string[]>('list_target_files', { 
          target: connectionTarget, 
          dir: currentPath 
        });
        setFiles(result);
      } catch (err) {
        console.error("Failed to list files:", err);
      } finally {
        setLoading(false);
      }
    }
    
    fetchFiles();
  }, [currentPath, isOpen, connectionTarget]);

  // Handle escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  const handleSelect = async (item: string) => {
    if (item.endsWith('/')) {
      // It's a directory
      const newPath = currentPath === '.' ? item.slice(0, -1) : `${currentPath}/${item.slice(0, -1)}`;
      setCurrentPath(newPath);
    } else {
      // It's a file
      try {
        const path = currentPath === '.' ? item : `${currentPath}/${item}`;
        const content = await invoke<string>('read_target_file', { 
          target: connectionTarget, 
          path 
        });
        onFileSelect(path, content);
      } catch (err) {
        console.error("Failed to read file:", err);
      }
    }
  };

  const handleBack = () => {
    if (currentPath === '.') return;
    const parts = currentPath.split('/');
    parts.pop();
    setCurrentPath(parts.length > 0 ? parts.join('/') : '.');
  };

  const filteredFiles = files.filter(f => f.toLowerCase().includes(search.toLowerCase()));

  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh] bg-black/50 backdrop-blur-sm">
          <motion.div 
            initial={{ opacity: 0, scale: 0.95, y: -20 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: -20 }}
            transition={{ duration: 0.15 }}
            className="w-full max-w-2xl bg-bg-tertiary border border-border-primary rounded-xl shadow-2xl overflow-hidden flex flex-col"
          >
            {/* Search Header */}
            <div className="flex items-center gap-3 px-4 py-3 border-b border-border-primary bg-bg-primary">
              <Search size={18} className="text-text-tertiary" />
              <input
                autoFocus
                type="text"
                placeholder="Search files by name..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="flex-1 bg-transparent text-text-primary outline-none text-sm placeholder:text-text-tertiary"
              />
              <button onClick={onClose} className="p-1 hover:bg-bg-tertiary rounded text-text-tertiary">
                <X size={16} />
              </button>
            </div>

            {/* Path Breadcrumbs */}
            <div className="px-4 py-2 bg-bg-secondary border-b border-border-primary text-xs font-mono text-text-tertiary flex items-center gap-2">
              <span className="text-accent-primary" style={{ color: 'var(--accent-primary)' }}>Workspace</span>
              {currentPath !== '.' && (
                <>
                  <span className="text-text-tertiary">/</span>
                  <span>{currentPath}</span>
                </>
              )}
            </div>

            {/* File List */}
            <div className="max-h-[50vh] overflow-y-auto">
              {loading ? (
                <div className="p-8 text-center text-text-tertiary text-sm animate-pulse">Loading...</div>
              ) : (
                <div className="py-2">
                  {currentPath !== '.' && !search && (
                    <button
                      onClick={handleBack}
                      className="w-full flex items-center gap-3 px-4 py-2 hover:bg-bg-tertiary/50 text-text-primary text-sm transition-colors text-left"
                    >
                      <Folder size={16} className="text-accent-primary" style={{ color: 'var(--accent-primary)' }} />
                      <span>..</span>
                    </button>
                  )}

                  {filteredFiles.length === 0 ? (
                    <div className="p-8 text-center text-text-tertiary text-sm">No files found.</div>
                  ) : (
                    filteredFiles.map((file, i) => {
                      const isDir = file.endsWith('/');
                      const name = isDir ? file.slice(0, -1) : file;
                      
                      return (
                        <button
                          key={i}
                          onClick={() => handleSelect(file)}
                          className="w-full flex items-center gap-3 px-4 py-2 hover:bg-bg-tertiary/50 text-text-primary text-sm transition-colors text-left group"
                        >
                          {isDir ? (
                            <Folder size={16} className="text-accent-primary" style={{ color: 'var(--accent-primary)' }} />
                          ) : (
                            <File size={16} className="text-text-tertiary group-hover:text-text-primary" />
                          )}
                          <span>{name}</span>
                        </button>
                      );
                    })
                  )}
                </div>
              )}
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
