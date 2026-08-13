import { MessageSquare, Folder, Settings } from 'lucide-react';

export function Sidebar() {
  return (
    <div className="w-12 h-full bg-[#121212] border-r border-[#262626] flex flex-col items-center py-4 gap-6 text-[#9CA3AF]">
      <button className="hover:text-white transition-colors"><MessageSquare size={20} /></button>
      <button className="hover:text-white transition-colors"><Folder size={20} /></button>
      <div className="flex-1" />
      <button className="hover:text-white transition-colors"><Settings size={20} /></button>
    </div>
  );
}
