import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Settings, Plug, Book, Lightbulb, X, CheckCircle2, Cpu } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import { ConnectionTarget } from '../hooks/useHarness';

interface CustomizationsModalProps {
  isOpen: boolean;
  onClose: () => void;
  connectionTarget?: ConnectionTarget | null;
}

type TabId = 'llm' | 'knowledge' | 'skills' | 'mcp' | 'settings';

export function CustomizationsModal({ isOpen, onClose, connectionTarget }: CustomizationsModalProps) {
  const [activeTab, setActiveTab] = useState<TabId>('llm');
  const [loading, setLoading] = useState(false);

  // Data states
  const [mcpConfig, setMcpConfig] = useState<any>(null);
  const [settings, setSettings] = useState<any>(null);
  const [skills, setSkills] = useState<string[]>([]);
  const [knowledge, setKnowledge] = useState<string[]>([]);
  
  // LLM Config states
  const [llmConfig, setLlmConfig] = useState<any>(null);
  const [llmLoading, setLlmLoading] = useState(false);
  const [llmSaving, setLlmSaving] = useState(false);
  const [llmError, setLlmError] = useState('');

  // Form states
  const [formEndpoint, setFormEndpoint] = useState('divmora');
  const [formBaseUrl, setFormBaseUrl] = useState('');
  const [formApiKey, setFormApiKey] = useState('');
  const [formDefaultModel, setFormDefaultModel] = useState('');

  useEffect(() => {
    if (!isOpen) return;
    
    async function fetchData() {
      setLoading(true);
      try {
        // Fetch LLM Config using target-aware read
        setLlmLoading(true);
        try {
          const llmRaw = await invoke<string>('read_target_file', { 
            target: connectionTarget, 
            path: '~/.divmora/config/litellm.json' 
          });
          const config = JSON.parse(llmRaw);
          setLlmConfig(config);
          
          if (config && config.endpoints && config.defaultEndpoint) {
            setFormEndpoint(config.defaultEndpoint);
            const ep = config.endpoints[config.defaultEndpoint];
            if (ep) {
              setFormBaseUrl(ep.baseUrl || '');
              setFormApiKey(ep.apiKey || '');
              setFormDefaultModel(ep.defaultModel || '');
            }
          }
        } catch (e) {
          setLlmConfig({
            defaultEndpoint: 'divmora',
            endpoints: {
              'divmora': {
                baseUrl: '',
                apiKey: '',
                defaultModel: ''
              }
            }
          });
          setFormEndpoint('divmora');
        } finally {
          setLlmLoading(false);
        }

        // Fetch MCP Config
        try {
          const mcpRaw = await invoke<string>('read_file', { path: '~/.divmora/config/mcp_config.json' });
          setMcpConfig(JSON.parse(mcpRaw));
        } catch (e) {
          setMcpConfig({ mcpServers: {} });
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
          setSkills(globalSkills.filter(s => s.endsWith('/')));
        } catch (e) {
          setSkills([]);
        }

        // Fetch Knowledge Items
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
  }, [isOpen, connectionTarget]);

  // Handle escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  const handleSaveLlmConfig = async () => {
    setLlmSaving(true);
    setLlmError('');
    try {
      const newConfig = {
        ...llmConfig,
        defaultEndpoint: formEndpoint,
        endpoints: {
          ...(llmConfig?.endpoints || {}),
          [formEndpoint]: {
            baseUrl: formBaseUrl,
            apiKey: formApiKey,
            defaultModel: formDefaultModel
          }
        }
      };

      await invoke('write_target_file', {
        target: connectionTarget,
        path: '~/.divmora/config/litellm.json',
        content: JSON.stringify(newConfig, null, 2)
      });
      
      setLlmConfig(newConfig);
    } catch (e: any) {
      setLlmError(e.toString());
    } finally {
      setLlmSaving(false);
    }
  };

  const tabs = [
    { id: 'llm', label: 'LLM Configuration', icon: Cpu },
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
            className="w-full max-w-4xl h-[70vh] bg-[#121212] border border-[#262626] rounded-xl shadow-2xl overflow-hidden flex flex-col"
          >
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-[#262626] bg-[#000000] shrink-0">
              <div className="flex items-center gap-3">
                <Settings size={20} className="text-[#9CA3AF]" />
                <h2 className="text-sm font-semibold text-[#F9FAFB]">
                  Customizations Manager
                  {connectionTarget?.kind === 'ssh' && (
                    <span className="ml-2 text-xs px-2 py-0.5 rounded bg-blue-900 text-blue-300 font-normal">
                      Remote: {connectionTarget.host}
                    </span>
                  )}
                </h2>
              </div>
              <button onClick={onClose} className="p-1 hover:bg-[#262626] rounded text-[#9CA3AF]">
                <X size={18} />
              </button>
            </div>

            {/* Content Area */}
            <div className="flex flex-1 overflow-hidden">
              {/* Sidebar Tabs */}
              <div className="w-56 bg-[#0A0A0A] border-r border-[#262626] flex flex-col p-2 gap-1 shrink-0">
                {tabs.map(tab => (
                  <button
                    key={tab.id}
                    onClick={() => setActiveTab(tab.id as TabId)}
                    className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-colors text-left ${activeTab === tab.id ? 'bg-[#262626] text-[#3B82F6] font-medium shadow-sm' : 'text-[#9CA3AF] hover:bg-[#262626]/50 hover:text-[#F9FAFB]'}`}
                  >
                    <tab.icon size={16} />
                    {tab.label}
                  </button>
                ))}
              </div>

              {/* Main Content */}
              <div className="flex-1 overflow-y-auto p-6 bg-[#121212]">
                {loading && activeTab !== 'llm' ? (
                  <div className="flex h-full items-center justify-center text-[#6c7086] text-sm animate-pulse">Loading configurations...</div>
                ) : (
                  <div className="max-w-2xl mx-auto flex flex-col gap-6">
                    
                    {activeTab === 'llm' && (
                      <div className="flex flex-col gap-6">
                        <div>
                          <h3 className="text-lg font-semibold text-[#F9FAFB] mb-1">LLM Configuration</h3>
                          <p className="text-xs text-[#6B7280]">
                            Configure the LiteLLM proxy settings {connectionTarget?.kind === 'ssh' ? `for remote host ${connectionTarget.host}` : 'for your local machine'}.
                          </p>
                        </div>
                        
                        {llmLoading ? (
                          <div className="text-center text-[#6c7086] text-sm py-8 animate-pulse">Reading config...</div>
                        ) : (
                          <div className="bg-[#0A0A0A] border border-[#262626] rounded-lg p-5 flex flex-col gap-5">
                            
                            <div className="flex flex-col gap-1.5">
                              <label className="text-xs font-semibold text-[#9CA3AF]">Endpoint Name</label>
                              <input
                                type="text"
                                className="w-full bg-[#000000] border border-[#262626] text-sm text-[#F9FAFB] rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors"
                                value={formEndpoint}
                                onChange={e => setFormEndpoint(e.target.value)}
                              />
                            </div>
                            
                            <div className="flex flex-col gap-1.5">
                              <label className="text-xs font-semibold text-[#9CA3AF]">Base URL</label>
                              <input
                                type="text"
                                placeholder="https://litellm.pixelvide.cloud"
                                className="w-full bg-[#000000] border border-[#262626] text-sm text-[#F9FAFB] rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors"
                                value={formBaseUrl}
                                onChange={e => setFormBaseUrl(e.target.value)}
                              />
                            </div>
                            
                            <div className="flex flex-col gap-1.5">
                              <label className="text-xs font-semibold text-[#9CA3AF]">API Key</label>
                              <input
                                type="password"
                                placeholder="dc001-litellm-key"
                                className="w-full bg-[#000000] border border-[#262626] text-sm text-[#F9FAFB] rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors font-mono"
                                value={formApiKey}
                                onChange={e => setFormApiKey(e.target.value)}
                              />
                            </div>
                            
                            <div className="flex flex-col gap-1.5">
                              <label className="text-xs font-semibold text-[#9CA3AF]">Default Model</label>
                              <input
                                type="text"
                                placeholder="workers-ai/@cf/zai-org/glm-5.2"
                                className="w-full bg-[#000000] border border-[#262626] text-sm text-[#F9FAFB] rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors font-mono"
                                value={formDefaultModel}
                                onChange={e => setFormDefaultModel(e.target.value)}
                              />
                            </div>
                            
                            {llmError && (
                              <div className="text-xs text-red-400 mt-2">{llmError}</div>
                            )}
                            
                            <div className="flex justify-end pt-2">
                              <button
                                onClick={handleSaveLlmConfig}
                                disabled={llmSaving}
                                className="bg-[#3B82F6] hover:bg-[#60A5FA] text-[#000000] text-sm font-semibold rounded-md px-4 py-2 transition-colors disabled:opacity-50"
                              >
                                {llmSaving ? 'Saving...' : 'Save Configuration'}
                              </button>
                            </div>
                          </div>
                        )}
                        
                        <div className="text-xs text-[#6B7280] flex items-center gap-2 mt-2 bg-[#000000] p-3 rounded-md border border-[#262626]">
                           <span className="text-[#EF4444]">Advanced:</span>
                           Config is stored in <code>~/.divmora/config/litellm.json</code> on the target machine.
                        </div>
                      </div>
                    )}

                    {activeTab === 'knowledge' && (
                      <div className="flex flex-col gap-4">
                        <div>
                          <h3 className="text-lg font-semibold text-[#F9FAFB] mb-1">Knowledge Items</h3>
                          <p className="text-xs text-[#6B7280]">Persistent memory artifacts saved by the agent to remember project context.</p>
                        </div>
                        {knowledge.length === 0 ? (
                          <div className="p-8 border border-dashed border-[#262626] rounded-lg text-center text-[#6c7086] text-sm">No knowledge items found.</div>
                        ) : (
                          <div className="grid grid-cols-1 gap-3">
                            {knowledge.map((ki, i) => (
                              <div key={i} className="p-4 bg-[#0A0A0A] border border-[#262626] rounded-lg flex items-center justify-between">
                                <div className="flex items-center gap-3">
                                  <Book size={18} className="text-[#3B82F6]" />
                                  <span className="text-sm font-medium text-[#F9FAFB]">{ki.replace('/', '')}</span>
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
                          <h3 className="text-lg font-semibold text-[#F9FAFB] mb-1">Active Skills</h3>
                          <p className="text-xs text-[#6B7280]">Global and workspace-level skills available to the agent.</p>
                        </div>
                        {skills.length === 0 ? (
                          <div className="p-8 border border-dashed border-[#262626] rounded-lg text-center text-[#6c7086] text-sm">No active skills found.</div>
                        ) : (
                          <div className="grid grid-cols-1 gap-3">
                            {skills.map((skill, i) => (
                              <div key={i} className="p-4 bg-[#0A0A0A] border border-[#262626] rounded-lg flex flex-col gap-1">
                                <div className="flex items-center gap-3">
                                  <Lightbulb size={18} className="text-[#F59E0B]" />
                                  <span className="text-sm font-medium text-[#F9FAFB]">{skill.replace('/', '')}</span>
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
                            <h3 className="text-lg font-semibold text-[#F9FAFB] mb-1">Model Context Protocol Servers</h3>
                            <p className="text-xs text-[#6B7280]">External tool servers connected to the agent.</p>
                          </div>
                          <button className="px-3 py-1.5 bg-[#3B82F6] hover:bg-[#60A5FA] text-[#000000] text-xs font-semibold rounded-md transition-colors">
                            Add Server
                          </button>
                        </div>
                        
                        {!mcpConfig?.mcpServers || Object.keys(mcpConfig.mcpServers).length === 0 ? (
                          <div className="p-8 border border-dashed border-[#262626] rounded-lg text-center text-[#6c7086] text-sm">No MCP servers configured.</div>
                        ) : (
                          <div className="flex flex-col gap-4">
                            {Object.entries(mcpConfig.mcpServers).map(([name, config]: [string, any]) => (
                              <div key={name} className="p-4 bg-[#0A0A0A] border border-[#262626] rounded-lg flex flex-col gap-3">
                                <div className="flex items-center justify-between">
                                  <div className="flex items-center gap-2">
                                    <Plug size={16} className="text-[#10B981]" />
                                    <span className="font-semibold text-sm text-[#F9FAFB]">{name}</span>
                                  </div>
                                  <span className="px-2 py-0.5 rounded text-[10px] uppercase font-bold bg-[#10B981]/10 text-[#10B981] flex items-center gap-1">
                                    <CheckCircle2 size={10} /> Active
                                  </span>
                                </div>
                                <div className="text-xs font-mono text-[#6B7280] bg-[#000000] p-2 rounded border border-[#262626] overflow-x-auto">
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
                          <h3 className="text-lg font-semibold text-[#F9FAFB] mb-1">Global Settings</h3>
                          <p className="text-xs text-[#6B7280]">Configuration for the LocalHarness engine.</p>
                        </div>
                        
                        <div className="bg-[#0A0A0A] border border-[#262626] rounded-lg p-5 flex flex-col gap-4">
                          <div className="flex flex-col gap-1">
                            <label className="text-xs font-semibold text-[#9CA3AF]">Telemetry</label>
                            <div className="flex items-center justify-between text-sm text-[#F9FAFB]">
                              <span>Allow anonymous usage statistics</span>
                              <input type="checkbox" checked={settings?.telemetry !== false} readOnly className="accent-[#3B82F6]" />
                            </div>
                          </div>
                          <div className="w-full h-px bg-[#262626]" />
                          <div className="flex flex-col gap-1">
                            <label className="text-xs font-semibold text-[#9CA3AF]">Log Level</label>
                            <select className="bg-[#000000] border border-[#262626] text-sm text-[#F9FAFB] rounded p-2 outline-none focus:border-[#3B82F6]">
                              <option value="info">Info</option>
                              <option value="debug">Debug</option>
                              <option value="warn">Warn</option>
                              <option value="error">Error</option>
                            </select>
                          </div>
                        </div>

                        <div className="text-xs text-[#6B7280] flex items-center gap-2 mt-4 bg-[#000000] p-3 rounded-md border border-[#262626]">
                           <span className="text-[#EF4444]">Advanced:</span>
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
