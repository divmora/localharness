import { MessageSquare, Folder, Settings } from 'lucide-react';

export function Sidebar() {
  return (
    <div className="w-12 h-full bg-bg-tertiary border-r border-border-primary flex flex-col items-center py-4 gap-6 text-text-tertiary">
      <button className="hover:text-text-primary transition-colors"><MessageSquare size={20} /></button>
      <button className="hover:text-text-primary transition-colors"><Folder size={20} /></button>
      <div className="flex-1" />
      <button className="hover:text-text-primary transition-colors"><Settings size={20} /></button>
    </div>
  );
}
