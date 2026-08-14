import { useState, useEffect } from 'react';
import { X, Terminal, Server, FileText, Check, ChevronRight } from 'lucide-react';
import { ConnectionTarget } from '../hooks/useHarness';
import { invoke } from '@tauri-apps/api/core';

interface ConnectSSHModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConnect: (target: ConnectionTarget) => void;
}

export function ConnectSSHModal({ isOpen, onClose, onConnect }: ConnectSSHModalProps) {
  const [mode, setMode] = useState<'select' | 'edit'>('select');
  const [configContent, setConfigContent] = useState('');
  const [hosts, setHosts] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (isOpen) {
      loadConfig();
      setMode('select');
    }
  }, [isOpen]);

  const loadConfig = async () => {
    setIsLoading(true);
    setError('');
    try {
      const content = await invoke<string>('read_file', { path: "~/.ssh/config" });
      setConfigContent(content);
      parseHosts(content);
    } catch (e: any) {
      // If file doesn't exist, it's fine, we start with empty
      setConfigContent('');
      setHosts([]);
    } finally {
      setIsLoading(false);
    }
  };

  const parseHosts = (content: string) => {
    const lines = content.split('\n');
    const parsedHosts: string[] = [];
    for (const line of lines) {
      const match = line.trim().match(/^Host\s+(.+)$/i);
      if (match) {
        const hostNames = match[1].split(/\s+/);
        for (const hostName of hostNames) {
          if (!hostName.includes('*') && !hostName.includes('?')) {
            parsedHosts.push(hostName);
          }
        }
      }
    }
    // Deduplicate
    setHosts(Array.from(new Set(parsedHosts)));
  };

  const handleSave = async () => {
    setIsSaving(true);
    try {
      await invoke('write_file', { path: "~/.ssh/config", content: configContent });
      parseHosts(configContent);
      setMode('select');
    } catch (e: any) {
      setError(e.toString());
    } finally {
      setIsSaving(false);
    }
  };

  const handleConnect = (hostName: string) => {
    onConnect({
      kind: "ssh",
      host: hostName
    });
    onClose();
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-[500px] h-[550px] flex flex-col rounded-xl border border-border-primary bg-bg-secondary text-text-primary shadow-2xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-[#222] p-4 bg-[#111]">
          <div className="flex items-center gap-2 text-lg font-semibold">
            <Terminal size={20} className="text-blue-400" />
            Connect via SSH
          </div>
          <button onClick={onClose} className="text-text-tertiary hover:text-text-primary transition-colors">
            <X size={20} />
          </button>
        </div>

        {/* Tabs */}
        <div className="flex px-4 pt-3 border-b border-[#222] gap-4">
          <button
            onClick={() => setMode('select')}
            className={`pb-3 text-sm font-medium transition-colors border-b-2 ${
              mode === 'select' ? 'border-blue-500 text-text-primary' : 'border-transparent text-text-tertiary hover:text-text-tertiary'
            }`}
          >
            Select Host
          </button>
          <button
            onClick={() => setMode('edit')}
            className={`pb-3 text-sm font-medium transition-colors border-b-2 ${
              mode === 'edit' ? 'border-blue-500 text-text-primary' : 'border-transparent text-text-tertiary hover:text-text-tertiary'
            }`}
          >
            Edit ~/.ssh/config
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-hidden flex flex-col p-4 bg-bg-secondary">
          {isLoading ? (
            <div className="flex-1 flex items-center justify-center text-text-tertiary text-sm">
              Loading...
            </div>
          ) : mode === 'select' ? (
            <div className="flex flex-col h-full">
              {hosts.length === 0 ? (
                <div className="flex-1 flex flex-col items-center justify-center text-center">
                  <Server size={48} className="text-text-secondary mb-4" />
                  <h3 className="text-text-tertiary font-medium mb-1">No SSH hosts configured</h3>
                  <p className="text-text-tertiary text-sm mb-4">Add a host to your SSH config to get started.</p>
                  <button
                    onClick={() => {
                      if (!configContent) {
                        setConfigContent('# Example config:\n# Host myserver\n#   HostName 192.168.1.100\n#   User root\n#   IdentityFile ~/.ssh/id_rsa\n');
                      }
                      setMode('edit');
                    }}
                    className="flex items-center gap-2 text-sm text-blue-400 hover:text-blue-300 transition-colors"
                  >
                    <FileText size={16} />
                    Open Configuration File
                  </button>
                </div>
              ) : (
                <div className="flex-1 overflow-y-auto space-y-2">
                  <div className="text-xs font-semibold tracking-wider text-text-tertiary mb-3 px-2">AVAILABLE HOSTS</div>
                  {hosts.map(host => (
                    <button
                      key={host}
                      onClick={() => handleConnect(host)}
                      className="w-full flex items-center justify-between p-3 rounded-lg border border-[#222] bg-[#111] hover:bg-[#1A1A1A] hover:border-border-primary transition-all group text-left"
                    >
                      <div className="flex items-center gap-3">
                        <div className="w-8 h-8 rounded bg-bg-tertiary flex items-center justify-center text-text-tertiary group-hover:text-blue-400 transition-colors">
                          <Server size={16} />
                        </div>
                        <span className="font-medium text-text-primary group-hover:text-text-primary">{host}</span>
                      </div>
                      <ChevronRight size={18} className="text-text-tertiary group-hover:text-blue-400 transition-colors" />
                    </button>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <div className="flex flex-col h-full gap-3">
              <div className="text-xs text-text-tertiary">
                Editing your native <code className="bg-bg-tertiary px-1 py-0.5 rounded text-text-tertiary">~/.ssh/config</code>. Changes will be saved to your system.
              </div>
              <textarea
                value={configContent}
                onChange={e => setConfigContent(e.target.value)}
                className="flex-1 w-full rounded-lg border border-border-primary bg-bg-primary p-4 text-sm text-text-tertiary font-mono focus:border-blue-500 focus:outline-none resize-none"
                spellCheck={false}
              />
              {error && <div className="text-xs text-red-400">{error}</div>}
              <div className="flex justify-end pt-1">
                <button
                  onClick={handleSave}
                  disabled={isSaving}
                  className="flex items-center gap-2 rounded bg-blue-600 px-4 py-2 text-sm font-medium text-text-primary hover:bg-blue-500 transition-colors disabled:opacity-50"
                >
                  <Check size={16} />
                  {isSaving ? 'Saving...' : 'Save & Refresh'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
