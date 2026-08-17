import { useState, useRef, useEffect } from 'react';

interface CreateOfficeModalProps {
  onConfirm: (name: string, country: string) => void;
  onCancel: () => void;
}

const COUNTRIES = [
  'USA',
  'India',
  'China',
  'Japan',
  'UK',
  'Germany',
  'Canada'
];

export function CreateOfficeModal({ onConfirm, onCancel }: CreateOfficeModalProps) {
  const [name, setName] = useState('');
  const [country, setCountry] = useState('USA');
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onConfirm(name, country);
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center animate-in fade-in duration-200">
      <div className="bg-bg-primary border border-border-primary rounded-lg shadow-xl p-6 w-[400px] max-w-full m-4">
        <h2 className="text-lg font-bold text-text-primary mb-4">Create New Office</h2>
        
        <form onSubmit={handleSubmit}>
          <div className="mb-4">
            <label className="block text-xs text-text-secondary mb-1">Office Name</label>
            <input
              ref={inputRef}
              type="text"
              className="w-full px-3 py-2 bg-bg-secondary border border-border-primary rounded text-text-primary focus:outline-none focus:border-border-highlight"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="Enter Office name..."
            />
          </div>
          
          <div className="mb-6">
            <label className="block text-xs text-text-secondary mb-1">Country</label>
            <select
              className="w-full px-3 py-2 bg-bg-secondary border border-border-primary rounded text-text-primary focus:outline-none focus:border-border-highlight"
              value={country}
              onChange={e => setCountry(e.target.value)}
            >
              {COUNTRIES.map(c => (
                <option key={c} value={c}>{c}</option>
              ))}
            </select>
          </div>
          
          <div className="flex justify-end gap-2">
            <button
              type="button"
              className="px-4 py-2 text-sm font-medium text-text-secondary bg-bg-secondary hover:bg-bg-tertiary border border-border-primary rounded transition-colors"
              onClick={onCancel}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!name.trim()}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded transition-colors disabled:opacity-50"
            >
              Create Office
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
