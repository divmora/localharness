import { useState, useRef, useEffect } from 'react';

interface PromptModalProps {
  title: string;
  message?: string;
  defaultValue?: string;
  placeholder?: string;
  confirmText?: string;
  cancelText?: string;
  onConfirm: (value: string) => void;
  onCancel: () => void;
}

export function PromptModal({ 
  title, 
  message, 
  defaultValue = '', 
  placeholder = '', 
  confirmText = 'Confirm', 
  cancelText = 'Cancel', 
  onConfirm, 
  onCancel 
}: PromptModalProps) {
  const [value, setValue] = useState(defaultValue);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onConfirm(value);
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center animate-in fade-in duration-200">
      <div className="bg-bg-primary border border-border-primary rounded-lg shadow-xl p-6 w-[400px] max-w-full m-4">
        <h2 className="text-lg font-bold text-text-primary mb-2">{title}</h2>
        {message && <p className="text-sm text-text-secondary mb-4">{message}</p>}
        
        <form onSubmit={handleSubmit}>
          <input
            data-testid="input-prompt-modal"
            ref={inputRef}
            type="text"
            className="w-full bg-bg-secondary border border-border-primary text-text-primary text-sm rounded-md px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500/50 mb-6"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={placeholder}
          />
          
          <div className="flex justify-end gap-2">
            <button
              data-testid="btn-cancel-prompt-modal"
              type="button"
              className="px-4 py-2 text-sm font-medium text-text-secondary bg-bg-secondary hover:bg-bg-tertiary border border-border-primary rounded transition-colors"
              onClick={onCancel}
            >
              {cancelText}
            </button>
            <button
              data-testid="btn-confirm-prompt-modal"
              type="submit"
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded transition-colors"
              disabled={!value.trim()}
            >
              {confirmText}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
