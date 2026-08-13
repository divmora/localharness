import { useState, useEffect, useMemo } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Settings, Plug, Book, Lightbulb, CheckCircle2, Cpu, ArrowLeft } from 'lucide-react';
import { ConnectionTarget } from '../hooks/useHarness';

interface CustomizationsPageProps {
  onClose: () => void;
  connectionTarget?: ConnectionTarget | null;
}

type TabId = 'llm' | 'knowledge' | 'skills' | 'mcp' | 'settings';

export function CustomizationsPage({ onClose, connectionTarget }: CustomizationsPageProps) {
  const [activeTab, setActiveTab] = useState<TabId>('llm');
  const [loading, setLoading] = useState(false);

  // Data states
  const [mcpConfig, setMcpConfig] = useState<any>(null);
  const [settings, setSettings] = useState<any>(null);
  const [skills, setSkills] = useState<string[]>([]);
  const [knowledge, setKnowledge] = useState<string[]>([]);

  // LLM Config states
  // LLM Config State
  const [llmConfig, setLlmConfig] = useState<any>(null);
  const [activeEndpoint, setActiveEndpoint] = useState('divmora');
  const [formBaseUrl, setFormBaseUrl] = useState('');
  const [formApiKey, setFormApiKey] = useState('');
  const [formDefaultModel, setFormDefaultModel] = useState('');
  const [llmSaving, setLlmSaving] = useState(false);
  const [llmError, setLlmError] = useState('');

  const endpointNames = useMemo(() => {
    if (!llmConfig?.endpoints) return ['divmora'];
    return Object.keys(llmConfig.endpoints);
  }, [llmConfig]);

  // Handle selecting an endpoint from the dropdown
  const handleSelectEndpoint = (name: string, configContext?: any) => {
    const configToUse = configContext || llmConfig;
    setActiveEndpoint(name);
    if (configToUse?.endpoints?.[name]) {
      setFormBaseUrl(configToUse.endpoints[name].baseUrl || '');
      setFormApiKey(configToUse.endpoints[name].apiKey || '');
      setFormDefaultModel(configToUse.endpoints[name].defaultModel || '');
    } else {
      setFormBaseUrl('');
      setFormApiKey('');
      setFormDefaultModel('');
    }
  };

  useEffect(() => {
    async function fetchData() {
      setLoading(true);
      try {
        // Fetch LLM Config
        try {
          const llmRaw = await invoke<string>('read_target_file', {
            target: connectionTarget,
            path: '~/.divmora/config/litellm.json'
          });
          const parsed = JSON.parse(llmRaw);
          setLlmConfig(parsed);
          setActiveEndpoint(''); // Start in list view
        } catch (e) {
          // If file doesn't exist, set empty default
          const defaultCfg = { endpoints: { 'divmora': { baseUrl: '', apiKey: '', defaultModel: '' } } };
          setLlmConfig(defaultCfg);
          setActiveEndpoint(''); // Start in list view
        }

        // Fetch MCP Config
        try {
          const mcpRaw = await invoke<string>('read_target_file', {
            target: connectionTarget,
            path: '~/.divmora/config/mcp_config.json'
          });
          setMcpConfig(JSON.parse(mcpRaw));
        } catch (e) {
          setMcpConfig({ mcpServers: {} });
        }

        // Fetch Settings
        try {
          const settingsRaw = await invoke<string>('read_target_file', {
            target: connectionTarget,
            path: '~/.divmora/config/settings.json'
          });
          setSettings(JSON.parse(settingsRaw));
        } catch (e) {
          setSettings({});
        }

        // Fetch Skills
        try {
          const globalSkills = await invoke<string[]>('list_target_files', {
            target: connectionTarget,
            dir: '~/.divmora/localharness/skills'
          });
          setSkills(globalSkills.filter(s => s.endsWith('/')));
        } catch (e) {
          setSkills([]);
        }

        // Fetch Knowledge Items
        try {
          const kiDirs = await invoke<string[]>('list_target_files', {
            target: connectionTarget,
            dir: '~/.divmora/localharness/knowledge'
          });
          let allKis: string[] = [];
          for (const projDir of kiDirs) {
            if (projDir.endsWith('/')) {
              try {
                const items = await invoke<string[]>('list_target_files', {
                  target: connectionTarget,
                  dir: `~/.divmora/localharness/knowledge/${projDir.slice(0, -1)}`
                });
                allKis = [...allKis, ...items.filter(i => i.endsWith('/'))];
              } catch (e) { }
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
  }, [connectionTarget]);

  // Handle escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleSaveLlmConfig = async () => {
    setLlmSaving(true);
    setLlmError('');
    try {
      const newConfig = {
        ...llmConfig,
        endpoints: {
          ...(llmConfig?.endpoints || {}),
          [activeEndpoint]: {
            baseUrl: formBaseUrl,
            apiKey: formApiKey,
            defaultModel: formDefaultModel
          }
        }
      };

      // Keep existing defaultEndpoint unless it wasn't set, then set to active
      if (!newConfig.defaultEndpoint) {
        newConfig.defaultEndpoint = activeEndpoint;
      }

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

  const handleSetDefaultEndpoint = async () => {
    setLlmSaving(true);
    try {
      const newConfig = {
        ...llmConfig,
        defaultEndpoint: activeEndpoint
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

  const handleDeleteEndpoint = async () => {
    if (!confirm(`Are you sure you want to delete the endpoint "${activeEndpoint}"?`)) return;

    setLlmSaving(true);
    try {
      const newEndpoints = { ...(llmConfig?.endpoints || {}) };
      delete newEndpoints[activeEndpoint];

      const newConfig = {
        ...llmConfig,
        endpoints: newEndpoints
      };

      // If we deleted the default, clear it
      if (newConfig.defaultEndpoint === activeEndpoint) {
        newConfig.defaultEndpoint = Object.keys(newEndpoints)[0] || '';
      }

      await invoke('write_target_file', {
        target: connectionTarget,
        path: '~/.divmora/config/litellm.json',
        content: JSON.stringify(newConfig, null, 2)
      });

      setLlmConfig(newConfig);
      handleSelectEndpoint(newConfig.defaultEndpoint || 'divmora', newConfig);
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
    <div className="flex-1 h-full bg-bg-tertiary flex flex-col min-w-0">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-border-primary bg-bg-primary shrink-0">
        <div className="flex items-center gap-3">
          <button
            onClick={onClose}
            className="p-1 hover:bg-border-primary text-text-secondary hover:text-text-primary rounded-md transition-colors flex items-center gap-2 text-sm mr-2"
          >
            <ArrowLeft size={16} />
            Back
          </button>
          <Settings size={20} className="text-text-secondary" />
          <h2 className="text-sm font-semibold text-text-primary">
            Customizations Manager
            {connectionTarget?.kind === 'ssh' && (
              <span className="ml-2 text-xs px-2 py-0.5 rounded bg-blue-900 text-blue-300 font-normal">
                Remote: {connectionTarget.host}
              </span>
            )}
          </h2>
        </div>
      </div>

      <div className="flex flex-1 overflow-hidden min-h-0">
        {/* Sidebar */}
        <div className="w-56 bg-bg-primary border-r border-border-primary flex flex-col p-3 gap-1 shrink-0">
          <div className="text-xs font-semibold text-text-tertiary px-3 py-2 mb-1 uppercase tracking-wider">
            Configuration
          </div>
          {tabs.map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id as TabId)}
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-colors text-left ${activeTab === tab.id ? 'bg-border-primary text-[#3B82F6] font-medium shadow-sm' : 'text-text-secondary hover:bg-border-primary/50 hover:text-text-primary'}`}
            >
              <tab.icon size={16} />
              {tab.label}
            </button>
          ))}
        </div>

        {/* Main Content */}
        <div className="flex-1 overflow-y-auto p-6 bg-bg-tertiary">
          {loading && activeTab !== 'llm' ? (
            <div className="flex h-full items-center justify-center text-[#6c7086] text-sm animate-pulse">Loading configurations...</div>
          ) : (
            <div className="max-w-2xl mx-auto flex flex-col gap-6">

              {activeTab === 'llm' && (
                <div className="flex flex-col gap-6">
                  <div>
                    <h3 className="text-lg font-semibold text-text-primary mb-1">LLM Configuration</h3>
                    <p className="text-xs text-text-tertiary">
                      Configure the LiteLLM proxy settings {connectionTarget?.kind === 'ssh' ? `for remote host ${connectionTarget.host}` : 'for your local machine'}.
                    </p>
                  </div>

                  {loading ? (
                    <div className="text-center text-[#6c7086] text-sm py-8 animate-pulse">Reading config...</div>
                  ) : (
                    <>
                      {activeEndpoint !== '' ? (
                        <div className="bg-bg-secondary border border-border-primary rounded-lg p-5 flex flex-col gap-5">
                          <div className="flex items-center gap-3 border-b border-border-primary pb-3 mb-2">
                            <button onClick={() => setActiveEndpoint('')} className="text-text-secondary hover:text-text-primary transition-colors">
                              <ArrowLeft size={16} />
                            </button>
                            <h4 className="text-sm font-semibold text-text-primary">
                              {endpointNames.includes(activeEndpoint) ? `Edit Endpoint: ${activeEndpoint}` : `New Endpoint: ${activeEndpoint}`}
                            </h4>
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-xs font-semibold text-text-secondary">Base URL</label>
                            <input
                              type="text"
                              placeholder="https://litellm.divmora.cloud"
                              className="w-full bg-bg-primary border border-border-primary text-sm text-text-primary rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors"
                              value={formBaseUrl}
                              onChange={e => setFormBaseUrl(e.target.value)}
                            />
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-xs font-semibold text-text-secondary">API Key</label>
                            <input
                              type="password"
                              placeholder="litellm-key"
                              className="w-full bg-bg-primary border border-border-primary text-sm text-text-primary rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors font-mono"
                              value={formApiKey}
                              onChange={e => setFormApiKey(e.target.value)}
                            />
                          </div>

                          <div className="flex flex-col gap-1.5">
                            <label className="text-xs font-semibold text-text-secondary">Default Model</label>
                            <input
                              type="text"
                              placeholder="workers-ai/@cf/zai-org/glm-5.2"
                              className="w-full bg-bg-primary border border-border-primary text-sm text-text-primary rounded p-2.5 outline-none focus:border-[#3B82F6] transition-colors font-mono"
                              value={formDefaultModel}
                              onChange={e => setFormDefaultModel(e.target.value)}
                            />
                          </div>

                          {llmError && (
                            <div className="text-xs text-red-400 mt-2">{llmError}</div>
                          )}

                          <div className="flex justify-end pt-2">
                            <button
                              onClick={async () => {
                                await handleSaveLlmConfig();
                                setActiveEndpoint('');
                              }}
                              disabled={llmSaving}
                              className="bg-[#3B82F6] hover:bg-[#60A5FA] text-[#000000] text-sm font-semibold rounded-md px-4 py-2 transition-colors disabled:opacity-50"
                            >
                              {llmSaving ? 'Saving...' : 'Save Configuration'}
                            </button>
                          </div>
                        </div>
                      ) : (
                        <div className="flex flex-col gap-4">
                          <div className="flex justify-end">
                            <button
                              onClick={() => {
                                const name = prompt("Enter a name for the new endpoint:");
                                if (name && name.trim()) {
                                  setFormBaseUrl('');
                                  setFormApiKey('');
                                  setFormDefaultModel('');
                                  setActiveEndpoint(name.trim());
                                }
                              }}
                              className="text-xs bg-border-primary hover:bg-border-highlight text-text-primary px-3 py-1.5 rounded-md flex items-center gap-1.5 transition-colors"
                            >
                              <Plug size={14} /> Add Endpoint
                            </button>
                          </div>

                          <div className="grid grid-cols-1 gap-3">
                            {endpointNames.map((name: string) => {
                              const ep = llmConfig?.endpoints?.[name];
                              const isDefault = llmConfig?.defaultEndpoint === name;

                              return (
                                <div key={name} className={`bg-bg-secondary border ${isDefault ? 'border-[#3B82F6]' : 'border-border-primary'} rounded-lg p-4 flex flex-col gap-3 relative`}>
                                  <div className="flex justify-between items-start">
                                    <div className="flex items-center gap-2">
                                      <h4 className="text-sm font-semibold text-text-primary">{name}</h4>
                                      {isDefault && <span className="bg-[#3B82F6]/20 text-[#3B82F6] text-[10px] uppercase font-bold px-1.5 py-0.5 rounded">Default</span>}
                                    </div>
                                    <div className="flex items-center gap-2">
                                      {!isDefault && (
                                        <button
                                          onClick={async () => {
                                            const oldActive = activeEndpoint;
                                            setActiveEndpoint(name);
                                            await handleSetDefaultEndpoint();
                                            setActiveEndpoint(oldActive);
                                          }}
                                          className="text-[11px] text-text-secondary hover:text-text-primary bg-bg-tertiary hover:bg-border-primary px-2 py-1 rounded transition-colors"
                                        >
                                          Make Default
                                        </button>
                                      )}
                                      <button
                                        onClick={() => {
                                          setFormBaseUrl(ep?.baseUrl || '');
                                          setFormApiKey(ep?.apiKey || '');
                                          setFormDefaultModel(ep?.defaultModel || '');
                                          setActiveEndpoint(name);
                                        }}
                                        className="text-[11px] text-text-secondary hover:text-[#3B82F6] bg-bg-tertiary hover:bg-border-primary px-2 py-1 rounded transition-colors"
                                      >
                                        Edit
                                      </button>
                                      {endpointNames.length > 1 && (
                                        <button
                                          onClick={async () => {
                                            if (!confirm(`Delete endpoint "${name}"?`)) return;
                                            const oldActive = activeEndpoint;
                                            setActiveEndpoint(name);
                                            await handleDeleteEndpoint();
                                            setActiveEndpoint(oldActive);
                                          }}
                                          className="text-[11px] text-text-secondary hover:text-red-400 bg-bg-tertiary hover:bg-border-primary px-2 py-1 rounded transition-colors"
                                        >
                                          Delete
                                        </button>
                                      )}
                                    </div>
                                  </div>

                                  <div className="flex flex-col gap-1 mt-1">
                                    <div className="text-xs flex items-center gap-2">
                                      <span className="text-text-tertiary w-16">URL:</span>
                                      <span className="text-[#D1D5DB] font-mono truncate">{ep?.baseUrl || 'Not set'}</span>
                                    </div>
                                    <div className="text-xs flex items-center gap-2">
                                      <span className="text-text-tertiary w-16">Model:</span>
                                      <span className="text-[#D1D5DB] font-mono truncate">{ep?.defaultModel || 'Not set'}</span>
                                    </div>
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                        </div>
                      )}
                    </>
                  )}

                  <div className="text-xs text-text-tertiary flex items-center gap-2 mt-2 bg-bg-primary p-3 rounded-md border border-border-primary">
                    <span className="text-[#EF4444]">Advanced:</span>
                    Config is stored in <code>~/.divmora/config/litellm.json</code> on the target machine.
                  </div>
                </div>
              )}

              {activeTab === 'knowledge' && (
                <div className="flex flex-col gap-4">
                  <div>
                    <h3 className="text-lg font-semibold text-text-primary mb-1">Knowledge Items</h3>
                    <p className="text-xs text-text-tertiary">Persistent memory artifacts saved by the agent to remember project context.</p>
                  </div>
                  {knowledge.length === 0 ? (
                    <div className="p-8 border border-dashed border-border-primary rounded-lg text-center text-[#6c7086] text-sm">No knowledge items found.</div>
                  ) : (
                    <div className="grid grid-cols-1 gap-3">
                      {knowledge.map((ki, i) => (
                        <div key={i} className="p-4 bg-bg-secondary border border-border-primary rounded-lg flex items-center justify-between">
                          <div className="flex items-center gap-3">
                            <Book size={18} className="text-[#3B82F6]" />
                            <span className="text-sm font-medium text-text-primary">{ki.replace('/', '')}</span>
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
                    <h3 className="text-lg font-semibold text-text-primary mb-1">Active Skills</h3>
                    <p className="text-xs text-text-tertiary">Global and workspace-level skills available to the agent.</p>
                  </div>
                  {skills.length === 0 ? (
                    <div className="p-8 border border-dashed border-border-primary rounded-lg text-center text-[#6c7086] text-sm">No active skills found.</div>
                  ) : (
                    <div className="grid grid-cols-1 gap-3">
                      {skills.map((skill, i) => (
                        <div key={i} className="p-4 bg-bg-secondary border border-border-primary rounded-lg flex flex-col gap-1">
                          <div className="flex items-center gap-3">
                            <Lightbulb size={18} className="text-[#F59E0B]" />
                            <span className="text-sm font-medium text-text-primary">{skill.replace('/', '')}</span>
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
                      <h3 className="text-lg font-semibold text-text-primary mb-1">Model Context Protocol Servers</h3>
                      <p className="text-xs text-text-tertiary">External tool servers connected to the agent.</p>
                    </div>
                    <button className="px-3 py-1.5 bg-[#3B82F6] hover:bg-[#60A5FA] text-[#000000] text-xs font-semibold rounded-md transition-colors">
                      Add Server
                    </button>
                  </div>

                  {!mcpConfig?.mcpServers || Object.keys(mcpConfig.mcpServers).length === 0 ? (
                    <div className="p-8 border border-dashed border-border-primary rounded-lg text-center text-[#6c7086] text-sm">No MCP servers configured.</div>
                  ) : (
                    <div className="flex flex-col gap-4">
                      {Object.entries(mcpConfig.mcpServers).map(([name, config]: [string, any]) => (
                        <div key={name} className="p-4 bg-bg-secondary border border-border-primary rounded-lg flex flex-col gap-3">
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              <Plug size={16} className="text-[#10B981]" />
                              <span className="font-semibold text-sm text-text-primary">{name}</span>
                            </div>
                            <span className="px-2 py-0.5 rounded text-[10px] uppercase font-bold bg-[#10B981]/10 text-[#10B981] flex items-center gap-1">
                              <CheckCircle2 size={10} /> Active
                            </span>
                          </div>
                          <div className="text-xs font-mono text-text-tertiary bg-bg-primary p-2 rounded border border-border-primary overflow-x-auto">
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
                    <h3 className="text-lg font-semibold text-text-primary mb-1">Global Settings</h3>
                    <p className="text-xs text-text-tertiary">Configuration for the LocalHarness engine.</p>
                  </div>

                  <div className="bg-bg-secondary border border-border-primary rounded-lg p-5 flex flex-col gap-4">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-text-secondary">Telemetry</label>
                      <div className="flex items-center justify-between text-sm text-text-primary">
                        <span>Allow anonymous usage statistics</span>
                        <input type="checkbox" checked={settings?.telemetry !== false} readOnly className="accent-[#3B82F6]" />
                      </div>
                    </div>
                    <div className="w-full h-px bg-border-primary" />
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-text-secondary">Log Level</label>
                      <select className="bg-bg-primary border border-border-primary text-sm text-text-primary rounded p-2 outline-none focus:border-[#3B82F6]">
                        <option value="info">Info</option>
                        <option value="debug">Debug</option>
                        <option value="warn">Warn</option>
                        <option value="error">Error</option>
                      </select>
                    </div>
                  </div>

                  <div className="text-xs text-text-tertiary flex items-center gap-2 mt-4 bg-bg-primary p-3 rounded-md border border-border-primary">
                    <span className="text-[#EF4444]">Advanced:</span>
                    Config files are stored in <code>~/.divmora/config/</code>.
                  </div>
                </div>
              )}

            </div>
          )}
        </div>
      </div>
    </div>
  );
}
