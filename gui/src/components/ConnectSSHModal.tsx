import { useState } from 'react';
import { X, Terminal } from 'lucide-react';
import { ConnectionTarget } from '../hooks/useHarness';

interface ConnectSSHModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConnect: (target: ConnectionTarget) => void;
}

export function ConnectSSHModal({ isOpen, onClose, onConnect }: ConnectSSHModalProps) {
  const [host, setHost] = useState('');
  const [user, setUser] = useState('');
  const [port, setPort] = useState('22');
  const [keyPath, setKeyPath] = useState('');

  if (!isOpen) return null;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onConnect({
      kind: "ssh",
      host,
      user,
      port: port ? parseInt(port, 10) : 22,
      key_path: keyPath || undefined
    });
    onClose();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-[450px] rounded-lg border border-[#222222] bg-[#0A0A0A] p-6 text-white shadow-2xl">
        <div className="mb-6 flex items-center justify-between">
          <div className="flex items-center gap-2 text-lg font-semibold">
            <Terminal size={20} className="text-blue-400" />
            Connect via SSH
          </div>
          <button onClick={onClose} className="text-neutral-400 hover:text-white transition-colors">
            <X size={20} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div>
            <label className="mb-1 block text-sm text-neutral-400">Host (IP or domain)</label>
            <input 
              type="text" 
              required
              placeholder="e.g. 192.168.1.100"
              className="w-full rounded border border-[#222] bg-[#111] p-2 text-sm text-white focus:border-blue-500 focus:outline-none"
              value={host}
              onChange={(e) => setHost(e.target.value)}
            />
          </div>

          <div className="flex gap-4">
            <div className="flex-1">
              <label className="mb-1 block text-sm text-neutral-400">Username</label>
              <input 
                type="text" 
                required
                placeholder="e.g. root"
                className="w-full rounded border border-[#222] bg-[#111] p-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                value={user}
                onChange={(e) => setUser(e.target.value)}
              />
            </div>
            <div className="w-24">
              <label className="mb-1 block text-sm text-neutral-400">Port</label>
              <input 
                type="number" 
                placeholder="22"
                className="w-full rounded border border-[#222] bg-[#111] p-2 text-sm text-white focus:border-blue-500 focus:outline-none"
                value={port}
                onChange={(e) => setPort(e.target.value)}
              />
            </div>
          </div>

          <div>
            <label className="mb-1 block text-sm text-neutral-400">SSH Key Path (Optional)</label>
            <input 
              type="text" 
              placeholder="~/.ssh/id_rsa"
              className="w-full rounded border border-[#222] bg-[#111] p-2 text-sm text-white focus:border-blue-500 focus:outline-none"
              value={keyPath}
              onChange={(e) => setKeyPath(e.target.value)}
            />
            <p className="mt-1 text-xs text-neutral-500">If blank, standard SSH config agents/keys are used.</p>
          </div>

          <div className="mt-4 flex justify-end gap-3">
            <button 
              type="button" 
              onClick={onClose}
              className="rounded px-4 py-2 text-sm text-neutral-300 hover:bg-[#222] transition-colors"
            >
              Cancel
            </button>
            <button 
              type="submit" 
              className="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 transition-colors"
            >
              Connect
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
