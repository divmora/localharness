import { useState, useEffect, createContext, useContext, ReactNode } from 'react';
import { X, CheckCircle, AlertCircle, Info, AlertTriangle } from 'lucide-react';

export type ToastType = 'success' | 'error' | 'info' | 'warning';

interface Toast {
  id: string;
  type: ToastType;
  title: string;
  message?: string;
  duration?: number;
}

interface ToastContextType {
  showToast: (toast: Omit<Toast, 'id'>) => void;
  removeToast: (id: string) => void;
}

const ToastContext = createContext<ToastContextType | undefined>(undefined);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const showToast = (toast: Omit<Toast, 'id'>) => {
    const id = crypto.randomUUID();
    const newToast = { ...toast, id };
    setToasts(prev => [...prev, newToast]);

    if (toast.duration !== 0) {
      setTimeout(() => {
        removeToast(id);
      }, toast.duration || 5000);
    }
  };

  const removeToast = (id: string) => {
    setToasts(prev => prev.filter(toast => toast.id !== id));
  };

  return (
    <ToastContext.Provider value={{ showToast, removeToast }}>
      {children}
      <ToastContainer toasts={toasts} removeToast={removeToast} />
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
}

function ToastContainer({ toasts, removeToast }: { toasts: Toast[]; removeToast: (id: string) => void }) {
  return (
    <div className="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 pointer-events-none">
      {toasts.map(toast => (
        <ToastItem key={toast.id} toast={toast} onRemove={removeToast} />
      ))}
    </div>
  );
}

function ToastItem({ toast, onRemove }: { toast: Toast; onRemove: (id: string) => void }) {
  const [isVisible, setIsVisible] = useState(false);

  useEffect(() => {
    setIsVisible(true);
    return () => setIsVisible(false);
  }, []);

  const icons = {
    success: <CheckCircle size={20} className="text-success" style={{ color: 'var(--success)' }} />,
    error: <AlertCircle size={20} className="text-error" style={{ color: 'var(--error)' }} />,
    info: <Info size={20} className="text-info" style={{ color: 'var(--info)' }} />,
    warning: <AlertTriangle size={20} className="text-warning" style={{ color: 'var(--warning)' }} />,
  };

  const borderColors = {
    success: 'var(--success)',
    error: 'var(--error)',
    info: 'var(--info)',
    warning: 'var(--warning)',
  };

  return (
    <div
      className={`pointer-events-auto bg-bg-secondary border border-border-primary rounded-lg shadow-lg p-4 min-w-[300px] max-w-md transition-all duration-300 ease-out ${
        isVisible ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-2'
      }`}
      style={{ borderLeftWidth: '4px', borderLeftColor: borderColors[toast.type] }}
    >
      <div className="flex items-start gap-3">
        <div className="flex-shrink-0 mt-0.5">
          {icons[toast.type]}
        </div>
        <div className="flex-1 min-w-0">
          <h4 className="text-sm font-semibold text-text-primary">{toast.title}</h4>
          {toast.message && (
            <p className="text-xs text-text-secondary mt-1">{toast.message}</p>
          )}
        </div>
        <button
          onClick={() => onRemove(toast.id)}
          className="flex-shrink-0 text-text-tertiary hover:text-text-primary transition-colors"
        >
          <X size={16} />
        </button>
      </div>
    </div>
  );
}