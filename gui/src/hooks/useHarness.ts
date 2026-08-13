import { useState, useEffect, useCallback } from 'react';
import { invoke } from '@tauri-apps/api/core';
import WebSocket from '@tauri-apps/plugin-websocket';
import { create, toBinary, fromBinary } from '@bufbuild/protobuf';
import { homeDir } from '@tauri-apps/api/path';
import { 
    ClientMessageSchema,
    ServerMessageSchema,
    UserMessageSchema,
    StepUpdate,
    InitRequestSchema,
    HarnessConfigSchema,
    QuestionResponseSchema,
    PermissionResponseSchema
} from '../gen/localharness/v1/localharness_pb';

export interface ConnectionTarget {
    kind: "local" | "ssh";
    host?: string;
    user?: string;
    port?: number;
    key_path?: string;
}

interface HarnessConnection {
    port: number;
    api_key: string;
}

export function useHarness(activeSessionId: string | null, connectionTarget: ConnectionTarget | null = null, workspacePath?: string | null) {
    const [connected, setConnected] = useState(false);
    const [steps, setSteps] = useState<StepUpdate[]>([]);
    const [socket, setSocket] = useState<WebSocket | null>(null);

    useEffect(() => {
        let ws: WebSocket | null = null;
        
        async function initHarness() {
            // Reset steps when switching sessions
            setSteps([]);
            
            try {
                console.log("Requesting sidecar from Rust with target:", connectionTarget);
                let conn: HarnessConnection;
                try {
                    conn = await invoke<HarnessConnection>('start_harness', { target: connectionTarget });
                    console.log("Got sidecar port:", conn.port);
                } catch (err: any) {
                    if (err.toString().includes('__TAURI_INTERNALS__')) {
                        console.warn("Tauri not detected, falling back to standalone localhost:4000 (web dev mode)");
                        conn = { port: 4000, api_key: "dev-key" };
                    } else {
                        throw err;
                    }
                }
                
                ws = await WebSocket.connect(`ws://localhost:${conn.port}/`, {
                    headers: {
                        'x-localharness-api-key': conn.api_key
                    }
                });
                
                // Send InitRequest
                const initReq = create(InitRequestSchema, {
                    config: create(HarnessConfigSchema, {
                        conversationId: activeSessionId || "",
                        workspaces: [
                            {
                                directory: workspacePath || await homeDir(),
                                name: "Project",
                                corpusName: ""
                            }
                        ],
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
                        },
                        clientSource: "GUI"
                    })
                });
                
                const initClientMsg = create(ClientMessageSchema, {
                    payload: {
                        case: "init",
                        value: initReq
                    }
                });
                
                await ws.send({
                    type: 'Binary',
                    data: Array.from(toBinary(ClientMessageSchema, initClientMsg))
                });
                
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
        await socket.send({
            type: 'Binary',
            data: Array.from(bytes)
        });
        
    }, [socket]);

    const submitQuestionResponse = useCallback(async (requestId: string, answers: any[], skipped: boolean) => {
        if (!socket) return;
        
        const qResponse = create(QuestionResponseSchema, {
            requestId: requestId,
            answers: answers,
            skipped: skipped
        });
        
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "questionResponse",
                value: qResponse
            }
        });
        
        const bytes = toBinary(ClientMessageSchema, clientMsg);
        await socket.send({
            type: 'Binary',
            data: Array.from(bytes)
        });
    }, [socket]);

    const submitPermissionResponse = useCallback(async (requestId: string, approved: boolean, denialReason: string = "") => {
        if (!socket) return;
        
        const pResponse = create(PermissionResponseSchema, {
            requestId: requestId,
            approved: approved,
            denialReason: denialReason
        });
        
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "permissionResponse",
                value: pResponse
            }
        });
        
        const bytes = toBinary(ClientMessageSchema, clientMsg);
        await socket.send({
            type: 'Binary',
            data: Array.from(bytes)
        });
    }, [socket]);

    return { connected, steps, sendPrompt, submitQuestionResponse, submitPermissionResponse };
}
