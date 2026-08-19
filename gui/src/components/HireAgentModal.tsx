import { useState } from 'react';
import { Space } from '../App';
import { invoke } from '@tauri-apps/api/core';

interface HireAgentModalProps {
  officeId: string;
  spaces: Space[];
  workspacePath: string | null;
  onClose: () => void;
  onAgentHired: () => void;
}

export const HireAgentModal = ({ officeId, spaces, workspacePath, onClose, onAgentHired }: HireAgentModalProps) => {
  const [name, setName] = useState('');
  const [role, setRole] = useState('Frontend Engineer');
  const [spaceId, setSpaceId] = useState(spaces.length > 0 ? spaces[0].id : '');
  const [gender, setGender] = useState('male');
  const [loading, setLoading] = useState(false);

  const handleHire = async () => {
    if (!name.trim()) return;
    setLoading(true);
    try {
      // 1. Start a new harness session
      const conn: any = await invoke('start_harness', { 
        sessionId: null, 
        target: null, 
        workspacePath: workspacePath || null 
      });
      
      const newSessionId = conn.session_id || conn;

      // 2. Assign to office agent list
      await invoke('add_office_agent', {
        officeId,
        agent: {
          session_id: newSessionId,
          office_id: officeId,
          agent_name: name,
          role_description: role,
          employment_type: 'Full-Time',
          gender: gender,
          experience_level: 'Senior',
          personality_traits: 'Hardworking',
          current_tasks: 0,
          specializations: role,
          visiting_session_id: null
        }
      });

      // 3. Move to selected space
      if (spaceId) {
        await invoke('move_session_to_space', {
          sessionId: newSessionId,
          spaceId
        });
      }

      onAgentHired();
      onClose();
    } catch (err) {
      console.error("Failed to hire agent:", err);
      alert(`Failed to hire agent: ${err}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
      <div className="bg-bg-primary border border-border-primary rounded-xl shadow-2xl w-full max-w-md overflow-hidden flex flex-col animate-in fade-in zoom-in duration-200">
        
        <div className="p-6 border-b border-border-primary">
          <h2 className="text-xl font-bold text-text-primary">Hire New Agent</h2>
          <p className="text-sm text-text-tertiary mt-1">Recruit a new autonomous agent to your office.</p>
        </div>

        <div className="p-6 flex-1 overflow-y-auto space-y-4">
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Agent Name</label>
            <input 
              type="text" 
              value={name} 
              onChange={e => setName(e.target.value)}
              className="w-full bg-bg-secondary border border-border-primary rounded px-3 py-2 text-text-primary focus:outline-none focus:border-blue-500"
              placeholder="e.g. Alice"
              autoFocus
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Role / Specialization</label>
            <select 
              value={role} 
              onChange={e => setRole(e.target.value)}
              className="w-full bg-bg-secondary border border-border-primary rounded px-3 py-2 text-text-primary focus:outline-none focus:border-blue-500"
            >
              <option value="Frontend Engineer">Frontend Engineer</option>
              <option value="Backend Engineer">Backend Engineer</option>
              <option value="Product Designer">Product Designer</option>
              <option value="Product Manager">Product Manager</option>
              <option value="DevOps Engineer">DevOps Engineer</option>
            </select>
          </div>
          
          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Gender (for Avatar)</label>
            <select 
              value={gender} 
              onChange={e => setGender(e.target.value)}
              className="w-full bg-bg-secondary border border-border-primary rounded px-3 py-2 text-text-primary focus:outline-none focus:border-blue-500"
            >
              <option value="male">Male</option>
              <option value="female">Female</option>
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-text-secondary mb-1">Assign to Space</label>
            <select 
              value={spaceId} 
              onChange={e => setSpaceId(e.target.value)}
              className="w-full bg-bg-secondary border border-border-primary rounded px-3 py-2 text-text-primary focus:outline-none focus:border-blue-500"
            >
              <option value="">Unassigned</option>
              {spaces.map(s => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </div>
        </div>

        <div className="p-4 border-t border-border-primary bg-bg-secondary flex justify-end gap-3">
          <button 
            onClick={onClose}
            className="px-4 py-2 text-text-secondary hover:text-text-primary transition-colors font-medium text-sm"
            disabled={loading}
          >
            Cancel
          </button>
          <button 
            onClick={handleHire}
            disabled={!name.trim() || loading}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded font-medium text-sm transition-colors shadow-sm disabled:opacity-50 flex items-center gap-2"
          >
            {loading ? 'Hiring...' : 'Hire Agent'}
          </button>
        </div>

      </div>
    </div>
  );
};
