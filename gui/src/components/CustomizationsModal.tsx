import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Settings, Plug, Book, Lightbulb, X, CheckCircle2 } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';

interface CustomizationsModalProps {
  isOpen: boolean;
  onClose: () => void;
}

type TabId = 'knowledge' | 'skills' | 'mcp' | 'settings';

export function CustomizationsModal({ isOpen, onClose }: CustomizationsModalProps) {
  const [activeTab, setActiveTab] = useState<TabId>('knowledge');
  const [loading, setLoading] = useState(false);

  // Data states
  const [mcpConfig, setMcpConfig] = useState<any>(null);
  const [settings, setSettings] = useState<any>(null);
  const [skills, setSkills] = useState<string[]>([]);
  const [knowledge, setKnowledge] = useState<string[]>([]);

  useEffect(() => {
    if (!isOpen) return;
    
    async function fetchData() {
      setLoading(true);
      try {
        // Fetch MCP Config
        try {
          const mcpRaw = await invoke<string>('read_file', { path: '~/.divmora/config/mcp_config.json' });
          setMcpConfig(JSON.parse(mcpRaw));
        } catch (e) {
          setMcpConfig({ mcpServers: {} }); // default empty
        }

        // Fetch Settings
        try {
          const settingsRaw = await invoke<string>('read_file', { path: '~/.divmora/config/settings.json' });
          setSettings(JSON.parse(settingsRaw));
        } catch (e) {
          setSettings({});
        }

        // Fetch Skills
        try {
          const globalSkills = await invoke<string[]>('list_files', { dir: '~/.divmora/localharness/skills' });
          setSkills(globalSkills.filter(s => s.endsWith('/'))); // Only directories
        } catch (e) {
          setSkills([]);
        }

        // Fetch Knowledge Items (across all projects for now)
        try {
          const kiDirs = await invoke<string[]>('list_files', { dir: '~/.divmora/localharness/knowledge' });
          let allKis: string[] = [];
          for (const projDir of kiDirs) {
            if (projDir.endsWith('/')) {
              try {
                const items = await invoke<string[]>('list_files', { dir: `~/.divmora/localharness/knowledge/${projDir}` });
                allKis = [...allKis, ...items.filter(i => i.endsWith('/'))];
              } catch (e) {}
            }
          }
          setKnowledge(allKis);
        } catch (e) {
          setKnowledge([]);
        }

      } catch (err) {
        console.error("Failed to load customizations:", err);
      } finally {
        setLoading(false);
      }
    }
    
    fetchData();
  }, [isOpen]);

  // Handle escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  const tabs = [
    { id: 'knowledge', label: 'Knowledge Base', icon: Book },
    { id: 'skills', label: 'Skills & Rules', icon: Lightbulb },
    { id: 'mcp', label: 'MCP Servers', icon: Plug },
    { id: 'settings', label: 'Settings', icon: Settings },
  ] as const;

  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-8 bg-black/60 backdrop-blur-sm">
          <motion.div 
            initial={{ opacity: 0, scale: 0.95, y: 10 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 10 }}
            transition={{ duration: 0.15 }}
            className="w-full max-w-4xl h-[70vh] bg-[#1e1e2e] border border-[#313244] rounded-xl shadow-2xl overflow-hidden flex flex-col"
          >
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-[#313244] bg-[#11111b] shrink-0">
              <div className="flex items-center gap-3">
                <Settings size={20} className="text-[#a6adc8]" />
                <h2 className="text-sm font-semibold text-[#cdd6f4]">Customizations Manager</h2>
              </div>
              <button onClick={onClose} className="p-1 hover:bg-[#313244] rounded text-[#a6adc8]">
                <X size={18} />
              </button>
            </div>

            {/* Content Area */}
            <div className="flex flex-1 overflow-hidden">
              {/* Sidebar Tabs */}
              <div className="w-48 bg-[#181825] border-r border-[#313244] flex flex-col p-2 gap-1 shrink-0">
                {tabs.map(tab => (
                  <button
                    key={tab.id}
                    onClick={() => setActiveTab(tab.id as TabId)}
                    className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-colors text-left ${activeTab === tab.id ? 'bg-[#313244] text-[#89b4fa] font-medium shadow-sm' : 'text-[#a6adc8] hover:bg-[#313244]/50 hover:text-[#cdd6f4]'}`}
                  >
                    <tab.icon size={16} />
                    {tab.label}
                  </button>
                ))}
              </div>

              {/* Main Content */}
              <div className="flex-1 overflow-y-auto p-6 bg-[#1e1e2e]">
                {loading ? (
                  <div className="flex h-full items-center justify-center text-[#6c7086] text-sm animate-pulse">Loading configurations...</div>
                ) : (
                  <div className="max-w-2xl mx-auto flex flex-col gap-6">
                    
                    {activeTab === 'knowledge' && (
                      <div className="flex flex-col gap-4">
                        <div>
                          <h3 className="text-lg font-semibold text-[#cdd6f4] mb-1">Knowledge Items</h3>
                          <p className="text-xs text-[#7f849c]">Persistent memory artifacts saved by the agent to remember project context.</p>
                        </div>
                        {knowledge.length === 0 ? (
                          <div className="p-8 border border-dashed border-[#313244] rounded-lg text-center text-[#6c7086] text-sm">No knowledge items found.</div>
                        ) : (
                          <div className="grid grid-cols-1 gap-3">
                            {knowledge.map((ki, i) => (
                              <div key={i} className="p-4 bg-[#181825] border border-[#313244] rounded-lg flex items-center justify-between">
                                <div className="flex items-center gap-3">
                                  <Book size={18} className="text-[#89b4fa]" />
                                  <span className="text-sm font-medium text-[#cdd6f4]">{ki.replace('/', '')}</span>
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}

                    {activeTab === 'skills' && (
                      <div className="flex flex-col gap-4">
                        <div>
                          <h3 className="text-lg font-semibold text-[#cdd6f4] mb-1">Active Skills</h3>
                          <p className="text-xs text-[#7f849c]">Global and workspace-level skills available to the agent.</p>
                        </div>
                        {skills.length === 0 ? (
                          <div className="p-8 border border-dashed border-[#313244] rounded-lg text-center text-[#6c7086] text-sm">No active skills found.</div>
                        ) : (
                          <div className="grid grid-cols-1 gap-3">
                            {skills.map((skill, i) => (
                              <div key={i} className="p-4 bg-[#181825] border border-[#313244] rounded-lg flex flex-col gap-1">
                                <div className="flex items-center gap-3">
                                  <Lightbulb size={18} className="text-[#f9e2af]" />
                                  <span className="text-sm font-medium text-[#cdd6f4]">{skill.replace('/', '')}</span>
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}

                    {activeTab === 'mcp' && (
                      <div className="flex flex-col gap-4">
                        <div className="flex items-center justify-between">
                          <div>
                            <h3 className="text-lg font-semibold text-[#cdd6f4] mb-1">Model Context Protocol Servers</h3>
                            <p className="text-xs text-[#7f849c]">External tool servers connected to the agent.</p>
                          </div>
                          <button className="px-3 py-1.5 bg-[#89b4fa] hover:bg-[#b4befe] text-[#11111b] text-xs font-semibold rounded-md transition-colors">
                            Add Server
                          </button>
                        </div>
                        
                        {!mcpConfig?.mcpServers || Object.keys(mcpConfig.mcpServers).length === 0 ? (
                          <div className="p-8 border border-dashed border-[#313244] rounded-lg text-center text-[#6c7086] text-sm">No MCP servers configured.</div>
                        ) : (
                          <div className="flex flex-col gap-4">
                            {Object.entries(mcpConfig.mcpServers).map(([name, config]: [string, any]) => (
                              <div key={name} className="p-4 bg-[#181825] border border-[#313244] rounded-lg flex flex-col gap-3">
                                <div className="flex items-center justify-between">
                                  <div className="flex items-center gap-2">
                                    <Plug size={16} className="text-[#a6e3a1]" />
                                    <span className="font-semibold text-sm text-[#cdd6f4]">{name}</span>
                                  </div>
                                  <span className="px-2 py-0.5 rounded text-[10px] uppercase font-bold bg-[#a6e3a1]/10 text-[#a6e3a1] flex items-center gap-1">
                                    <CheckCircle2 size={10} /> Active
                                  </span>
                                </div>
                                <div className="text-xs font-mono text-[#7f849c] bg-[#11111b] p-2 rounded border border-[#313244] overflow-x-auto">
                                  {config.command} {(config.args || []).join(' ')}
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}

                    {activeTab === 'settings' && (
                      <div className="flex flex-col gap-6">
                        <div>
                          <h3 className="text-lg font-semibold text-[#cdd6f4] mb-1">Global Settings</h3>
                          <p className="text-xs text-[#7f849c]">Configuration for the LocalHarness engine.</p>
                        </div>
                        
                        <div className="bg-[#181825] border border-[#313244] rounded-lg p-5 flex flex-col gap-4">
                          <div className="flex flex-col gap-1">
                            <label className="text-xs font-semibold text-[#a6adc8]">Telemetry</label>
                            <div className="flex items-center justify-between text-sm text-[#cdd6f4]">
                              <span>Allow anonymous usage statistics</span>
                              <input type="checkbox" checked={settings?.telemetry !== false} readOnly className="accent-[#89b4fa]" />
                            </div>
                          </div>
                          <div className="w-full h-px bg-[#313244]" />
                          <div className="flex flex-col gap-1">
                            <label className="text-xs font-semibold text-[#a6adc8]">Log Level</label>
                            <select className="bg-[#11111b] border border-[#313244] text-sm text-[#cdd6f4] rounded p-2 outline-none focus:border-[#89b4fa]">
                              <option value="info">Info</option>
                              <option value="debug">Debug</option>
                              <option value="warn">Warn</option>
                              <option value="error">Error</option>
                            </select>
                          </div>
                        </div>

                        <div className="text-xs text-[#7f849c] flex items-center gap-2 mt-4 bg-[#11111b] p-3 rounded-md border border-[#313244]">
                           <span className="text-[#f38ba8]">Advanced:</span>
                           Config files are stored in <code>~/.divmora/config/</code>.
                        </div>
                      </div>
                    )}

                  </div>
                )}
              </div>
            </div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
