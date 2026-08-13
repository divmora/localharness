import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
import { Sidebar } from './components/Sidebar';
import { ChatPanel } from './components/ChatPanel';
import { EditorPanel } from './components/EditorPanel';
import { TerminalPanel } from './components/TerminalPanel';
import './App.css';

function App() {
  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[#11111b] text-white">
      <Sidebar />
      <PanelGroup direction="horizontal" className="flex-1">
        <Panel defaultSize={25} minSize={20}>
          <ChatPanel />
        </Panel>
        
        <PanelResizeHandle className="w-1 bg-[#313244] hover:bg-blue-500 transition-colors" />
        
        <Panel defaultSize={75} minSize={30}>
          <PanelGroup direction="vertical">
            <Panel defaultSize={70}>
              <EditorPanel />
            </Panel>
            
            <PanelResizeHandle className="h-1 bg-[#313244] hover:bg-blue-500 transition-colors" />
            
            <Panel defaultSize={30} minSize={10}>
              <TerminalPanel />
            </Panel>
          </PanelGroup>
        </Panel>
      </PanelGroup>
    </div>
  );
}

export default App;
