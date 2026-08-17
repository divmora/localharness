interface ConfirmModalProps {
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  onConfirm: () => void;
  onCancel: () => void;
  destructive?: boolean;
}

export function ConfirmModal({ 
  title, 
  message, 
  confirmText = 'Confirm', 
  cancelText = 'Cancel', 
  onConfirm, 
  onCancel,
  destructive = false
}: ConfirmModalProps) {
  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center animate-in fade-in duration-200">
      <div className="bg-bg-primary border border-border-primary rounded-lg shadow-xl p-6 w-[400px] max-w-full m-4">
        <h2 className="text-lg font-bold text-text-primary mb-2">{title}</h2>
        <p className="text-sm text-text-secondary mb-6">{message}</p>
        
        <div className="flex justify-end gap-2">
          <button
            type="button"
            className="px-4 py-2 text-sm font-medium text-text-secondary bg-bg-secondary hover:bg-bg-tertiary border border-border-primary rounded transition-colors"
            onClick={onCancel}
          >
            {cancelText}
          </button>
          <button
            type="button"
            className={`px-4 py-2 text-sm font-medium text-white rounded transition-colors ${destructive ? 'bg-red-600 hover:bg-red-700' : 'bg-blue-600 hover:bg-blue-700'}`}
            onClick={onConfirm}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  );
}
