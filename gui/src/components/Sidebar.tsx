import { MessageSquare, Folder, Settings } from 'lucide-react';

export function Sidebar() {
  return (
    <div className="w-12 h-full bg-[#1e1e2e] border-r border-[#313244] flex flex-col items-center py-4 gap-6 text-[#a6adc8]">
      <button className="hover:text-white transition-colors"><MessageSquare size={20} /></button>
      <button className="hover:text-white transition-colors"><Folder size={20} /></button>
      <div className="flex-1" />
      <button className="hover:text-white transition-colors"><Settings size={20} /></button>
    </div>
  );
}
