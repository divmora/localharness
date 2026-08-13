import { useState, useEffect, useCallback } from 'react';
import { invoke } from '@tauri-apps/api/core';
import WebSocket from '@tauri-apps/plugin-websocket';
import { create, toBinary, fromBinary } from '@bufbuild/protobuf';
import { 
    ClientMessageSchema,
    ServerMessageSchema,
    UserMessageSchema,
    StepUpdate,
    InitRequestSchema,
    HarnessConfigSchema
} from '../gen/localharness/v1/localharness_pb';

interface HarnessConnection {
    port: number;
    api_key: string;
}

export function useHarness(activeSessionId: string | null) {
    const [connected, setConnected] = useState(false);
    const [steps, setSteps] = useState<StepUpdate[]>([]);
    const [socket, setSocket] = useState<WebSocket | null>(null);

    useEffect(() => {
        let ws: WebSocket | null = null;
        
        async function initHarness() {
            // Reset steps when switching sessions
            setSteps([]);
            
            try {
                console.log("Requesting sidecar from Rust...");
                const conn = await invoke<HarnessConnection>('start_harness');
                console.log("Got sidecar port:", conn.port);
                
                ws = await WebSocket.connect(`ws://localhost:${conn.port}/`, {
                    headers: {
                        'x-localharness-api-key': conn.api_key
                    }
                });
                
                // Send InitRequest
                const initReq = create(InitRequestSchema, {
                    config: create(HarnessConfigSchema, {
                        conversationId: activeSessionId || "",
                        builtinTools: {
                            viewFile: true,
                            createFile: true,
                            editFile: true,
                            listDir: true,
                            searchDir: true,
                            findFile: true,
                            runCommand: true,
                            finish: true,
                            schedule: true,
                        }
                    })
                });
                
                const initClientMsg = create(ClientMessageSchema, {
                    payload: {
                        case: "init",
                        value: initReq
                    }
                });
                
                await ws.send(Array.from(toBinary(ClientMessageSchema, initClientMsg)));
                
                console.log(`WebSocket connected for session: ${activeSessionId || 'new'}`);
                setConnected(true);
                
                ws.addListener((msg) => {
                    if (msg.type === 'Binary') {
                        const buffer = new Uint8Array(msg.data);
                        const serverMsg = fromBinary(ServerMessageSchema, buffer);
                        
                        if (serverMsg.payload.case === "stepUpdate") {
                            const step = serverMsg.payload.value;
                            setSteps(prev => [...prev, step]);
                        }
                    }
                });
                
                setSocket(ws);
                
            } catch (err) {
                console.error("Failed to start harness:", err);
            }
        }
        
        initHarness();
        
        return () => {
            if (ws) ws.disconnect();
            setConnected(false);
        };
    }, [activeSessionId]);

    const sendPrompt = useCallback(async (text: string) => {
        if (!socket) return;
        
        const userMsg = create(UserMessageSchema, {
            content: text,
        });
        
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "userMessage",
                value: userMsg
            }
        });
        
        const bytes = toBinary(ClientMessageSchema, clientMsg);
        await socket.send(Array.from(bytes)); // tauri websocket expects number[]
        
    }, [socket]);

    return { connected, steps, sendPrompt };
}
