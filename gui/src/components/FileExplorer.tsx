import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Search, File, Folder, X } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';

interface FileExplorerProps {
  isOpen: boolean;
  onClose: () => void;
  onFileSelect: (path: string, content: string) => void;
}

export function FileExplorer({ isOpen, onClose, onFileSelect }: FileExplorerProps) {
  const [currentPath, setCurrentPath] = useState<string>('.');
  const [files, setFiles] = useState<string[]>([]);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    
    async function fetchFiles() {
      setLoading(true);
      try {
        const result = await invoke<string[]>('list_files', { dir: currentPath });
        setFiles(result);
      } catch (err) {
        console.error("Failed to list files:", err);
      } finally {
        setLoading(false);
      }
    }
    
    fetchFiles();
  }, [currentPath, isOpen]);

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
        const content = await invoke<string>('read_file', { path });
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
            className="w-full max-w-2xl bg-[#121212] border border-[#262626] rounded-xl shadow-2xl overflow-hidden flex flex-col"
          >
            {/* Search Header */}
            <div className="flex items-center gap-3 px-4 py-3 border-b border-[#262626] bg-[#000000]">
              <Search size={18} className="text-[#9CA3AF]" />
              <input 
                autoFocus
                type="text" 
                placeholder="Search files by name..."
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="flex-1 bg-transparent text-[#F9FAFB] outline-none text-sm placeholder:text-[#6c7086]"
              />
              <button onClick={onClose} className="p-1 hover:bg-[#262626] rounded text-[#9CA3AF]">
                <X size={16} />
              </button>
            </div>

            {/* Path Breadcrumbs */}
            <div className="px-4 py-2 bg-[#0A0A0A] border-b border-[#262626] text-xs font-mono text-[#9CA3AF] flex items-center gap-2">
              <span className="text-[#3B82F6]">Workspace</span>
              {currentPath !== '.' && (
                <>
                  <span className="text-[#6c7086]">/</span>
                  <span>{currentPath}</span>
                </>
              )}
            </div>

            {/* File List */}
            <div className="max-h-[50vh] overflow-y-auto">
              {loading ? (
                <div className="p-8 text-center text-[#6c7086] text-sm animate-pulse">Loading...</div>
              ) : (
                <div className="py-2">
                  {currentPath !== '.' && !search && (
                    <button 
                      onClick={handleBack}
                      className="w-full flex items-center gap-3 px-4 py-2 hover:bg-[#262626]/50 text-[#F9FAFB] text-sm transition-colors text-left"
                    >
                      <Folder size={16} className="text-[#3B82F6]" />
                      <span>..</span>
                    </button>
                  )}
                  
                  {filteredFiles.length === 0 ? (
                    <div className="p-8 text-center text-[#6c7086] text-sm">No files found.</div>
                  ) : (
                    filteredFiles.map((file, i) => {
                      const isDir = file.endsWith('/');
                      const name = isDir ? file.slice(0, -1) : file;
                      
                      return (
                        <button 
                          key={i}
                          onClick={() => handleSelect(file)}
                          className="w-full flex items-center gap-3 px-4 py-2 hover:bg-[#262626]/50 text-[#F9FAFB] text-sm transition-colors text-left group"
                        >
                          {isDir ? (
                            <Folder size={16} className="text-[#3B82F6]" />
                          ) : (
                            <File size={16} className="text-[#9CA3AF] group-hover:text-[#F9FAFB]" />
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
