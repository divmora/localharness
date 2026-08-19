import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Office } from '../App';
import { COUNTRIES } from '../components/CreateOfficeModal';
import { useToast } from '../components/Toast';
import { PromptModal } from '../components/PromptModal';
import { Save, Trash2, ArrowLeft } from 'lucide-react';

interface OfficeSettingsPageProps {
  officeId: string;
  onClose: () => void;
  onOfficeUpdated: () => void;
  onOfficeDeleted: () => void;
}

export function OfficeSettingsPage({ officeId, onClose, onOfficeUpdated, onOfficeDeleted }: OfficeSettingsPageProps) {
  const { showToast } = useToast();
  const [office, setOffice] = useState<Office | null>(null);
  
  const [name, setName] = useState('');
  const [country, setCountry] = useState('USA');
  const [isSaving, setIsSaving] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  useEffect(() => {
    async function loadOffice() {
      try {
        const officesList = await invoke<Office[]>('get_offices');
        const current = officesList.find(o => o.id === officeId);
        if (current) {
          setOffice(current);
          setName(current.name);
          // Assuming `country` might be added to Office struct soon on the frontend
        }
      } catch (e) {
        console.error("Failed to load office for settings:", e);
      }
    }
    loadOffice();
  }, [officeId]);

  const handleSave = async () => {
    if (!name.trim()) {
      showToast({ title: 'Error', message: 'Office name cannot be empty', type: 'error' });
      return;
    }
    
    setIsSaving(true);
    try {
      await invoke('update_office', { id: officeId, name: name.trim(), country });
      showToast({ title: 'Success', message: 'Office updated successfully', type: 'success' });
      onOfficeUpdated();
    } catch (e) {
      console.error("Failed to update office:", e);
      showToast({ title: 'Error', message: 'Failed to update office', type: 'error' });
    } finally {
      setIsSaving(false);
    }
  };

  const handleDelete = async () => {
    try {
      await invoke("delete_office", { id: officeId });
      showToast({ title: 'Success', message: 'Office deleted successfully', type: 'success' });
      onOfficeDeleted();
    } catch (e) {
      console.error("Failed to delete office:", e);
      showToast({ title: 'Error', message: 'Failed to delete office', type: 'error' });
    }
  };

  if (!office) return <div className="p-8 text-text-secondary flex-1">Loading settings...</div>;

  return (
    <div className="flex-1 flex flex-col bg-bg-primary overflow-y-auto">
      {/* Header */}
      <div className="flex items-center gap-4 p-6 border-b border-border-primary">
        <button 
          onClick={onClose}
          className="p-2 hover:bg-bg-secondary rounded-full transition-colors text-text-secondary hover:text-text-primary"
        >
          <ArrowLeft size={20} />
        </button>
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Office Settings</h1>
          <p className="text-sm text-text-secondary">Manage {office.name}</p>
        </div>
      </div>

      <div className="p-6 max-w-3xl mx-auto w-full space-y-8">
        
        {/* General Settings */}
        <section className="bg-bg-secondary border border-border-primary rounded-xl p-6 shadow-sm">
          <h2 className="text-lg font-semibold text-text-primary mb-6">General Information</h2>
          
          <div className="space-y-5">
            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5 uppercase tracking-wider">Office Name</label>
              <input
                type="text"
                className="w-full px-3 py-2 bg-bg-primary border border-border-primary rounded-md text-text-primary focus:outline-none focus:border-blue-500 transition-colors"
                value={name}
                onChange={e => setName(e.target.value)}
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5 uppercase tracking-wider">Country</label>
              <select
                className="w-full px-3 py-2 bg-bg-primary border border-border-primary rounded-md text-text-primary focus:outline-none focus:border-blue-500 transition-colors"
                value={country}
                onChange={e => setCountry(e.target.value)}
              >
                {COUNTRIES.map((c: string) => (
                  <option key={c} value={c}>{c}</option>
                ))}
              </select>
            </div>

            <div>
              <label className="block text-xs font-medium text-text-secondary mb-1.5 uppercase tracking-wider">Workspace Directory</label>
              <input
                type="text"
                readOnly
                className="w-full px-3 py-2 bg-bg-tertiary border border-border-primary rounded-md text-text-secondary font-mono text-xs opacity-80 cursor-not-allowed"
                value={office.workspace_path || 'Auto-generated (Isolated)'}
              />
              <p className="text-[10px] text-text-tertiary mt-1.5">
                The workspace path cannot be changed after the office is created to prevent disruption of active agent tasks.
              </p>
            </div>
            
            <div className="pt-4 border-t border-border-primary">
              <button
                onClick={handleSave}
                disabled={isSaving}
                className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors disabled:opacity-50"
              >
                <Save size={16} />
                {isSaving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          </div>
        </section>

        {/* Danger Zone */}
        <section className="bg-bg-secondary border border-red-500/30 rounded-xl shadow-sm overflow-hidden">
          <div className="bg-red-500/10 px-6 py-4 border-b border-red-500/20">
            <h2 className="text-lg font-semibold text-red-500 flex items-center gap-2">
              <Trash2 size={20} />
              Danger Zone
            </h2>
          </div>
          <div className="p-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
              <div>
                <h3 className="font-medium text-text-primary mb-1">Delete Office</h3>
                <p className="text-xs text-text-secondary max-w-md">
                  Permanently wipe this office, all its spaces, and all manager and agent chat histories. If the workspace directory was auto-generated, it will also be deleted from your hard drive. This action cannot be undone.
                </p>
              </div>
              <button
                onClick={() => setShowDeleteConfirm(true)}
                className="flex items-center justify-center gap-2 px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded transition-colors whitespace-nowrap"
              >
                Delete Office
              </button>
            </div>
          </div>
        </section>
      </div>

      {showDeleteConfirm && (
        <PromptModal
          title="Delete Office"
          message="Are you sure you want to completely delete this office? This will permanently wipe the office, all its spaces, and all manager and agent chat histories. If the workspace directory was auto-generated, it will be deleted as well. This cannot be undone."
          onConfirm={handleDelete}
          onCancel={() => setShowDeleteConfirm(false)}
          confirmText="Delete Office"
        />
      )}
    </div>
  );
}
